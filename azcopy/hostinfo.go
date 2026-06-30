// Copyright © Microsoft <wastore@microsoft.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package azcopy

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// hostHardwareInfo holds best-effort hardware/OS facts about the machine running
// AzCopy. Each field carries a sentinel ("" or -1) when it cannot be determined
// on the current platform. The per-field probes are implemented in the
// platform-specific hostinfo_*.go files.
type hostHardwareInfo struct {
	osVersion string // e.g. "Ubuntu 22.04.4 LTS" / "Windows 10 Pro 19045"
	cpuModel  string // e.g. "Intel(R) Xeon(R) Platinum 8370C CPU @ 2.80GHz"
	memoryGB  int    // total physical memory rounded to GiB, 0 when unknown
	nicMbps   int    // best NIC link speed in Mbps, -1 when unknown
}

// probeHostHardware gathers best-effort host hardware facts. It never blocks on
// the network and never fails: missing values are returned as sentinels.
func probeHostHardware() hostHardwareInfo {
	return hostHardwareInfo{
		osVersion: osVersion(),
		cpuModel:  cpuModel(),
		memoryGB:  totalMemoryGB(),
		nicMbps:   nicSpeedMbps(),
	}
}

// ---------------------------------------------------------------------------
// Azure Instance Metadata Service (IMDS)
// ---------------------------------------------------------------------------

// imdsTimeout bounds how long the IMDS probe may block job startup. IMDS lives
// on a non-routable link-local address and replies almost instantly when
// present, so a short timeout is enough to detect "not on an Azure VM".
const imdsTimeout = 1 * time.Second

// imdsInfo captures the subset of IMDS data the telemetry agent cares about.
type imdsInfo struct {
	isAzureVM bool   // true when IMDS responded (i.e. running on an Azure VM)
	region    string // compute.location, e.g. "eastus"; "" when unavailable
}

// probeIMDS queries the Azure Instance Metadata Service to determine whether
// AzCopy is running on an Azure VM and, if so, in which region. It is
// best-effort: any error (including not being on an Azure VM) yields a zero
// value with isAzureVM=false. No proxy is used because IMDS is a non-routable
// link-local address visible only to Azure VMs, and the response is not
// security-sensitive.
func probeIMDS() imdsInfo {
	return probeIMDSWithClient(&http.Client{Timeout: imdsTimeout})
}

// probeIMDSWithClient is the testable core of probeIMDS.
func probeIMDSWithClient(client *http.Client) imdsInfo {
	const url = "http://169.254.169.254/metadata/instance/compute/location?api-version=2021-02-01&format=text"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return imdsInfo{}
	}
	req.Header.Add("Metadata", "true")

	resp, err := client.Do(req)
	if err != nil {
		return imdsInfo{}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// A response (even a non-200) still tells us we are on an Azure VM.
		return imdsInfo{isAzureVM: true}
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return imdsInfo{isAzureVM: true}
	}
	return imdsInfo{isAzureVM: true, region: strings.TrimSpace(buf.String())}
}

// ---------------------------------------------------------------------------
// Geo (best-effort, no network)
// ---------------------------------------------------------------------------

// geoTimezone returns the IANA timezone name (e.g. "America/Los_Angeles") on a
// best-effort basis. It consults, in order: the TZ environment variable, the
// /etc/timezone file, and the symlink target of /etc/localtime. It falls back to
// the local zone abbreviation reported by the Go runtime. Returns "" only when
// nothing is determinable.
func geoTimezone() string {
	return geoTimezoneFrom(os.Getenv, os.ReadFile, os.Readlink, localZoneAbbrev)
}

// geoTimezoneFrom is the testable core of geoTimezone with its environment and
// filesystem dependencies injected.
func geoTimezoneFrom(getenv func(string) string, readFile func(string) ([]byte, error), readlink func(string) (string, error), zoneAbbrev func() string) string {
	if tz := strings.TrimSpace(getenv("TZ")); tz != "" {
		return tz
	}
	if b, err := readFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(b)); tz != "" {
			return tz
		}
	}
	if target, err := readlink("/etc/localtime"); err == nil {
		if tz := zoneNameFromZoneinfoPath(target); tz != "" {
			return tz
		}
	}
	return zoneAbbrev()
}

// zoneNameFromZoneinfoPath extracts the IANA zone name from a zoneinfo path such
// as "/usr/share/zoneinfo/America/Los_Angeles" -> "America/Los_Angeles".
func zoneNameFromZoneinfoPath(p string) string {
	p = filepath.ToSlash(p)
	const marker = "zoneinfo/"
	if i := strings.LastIndex(p, marker); i >= 0 {
		return strings.Trim(p[i+len(marker):], "/")
	}
	return ""
}

// localZoneAbbrev returns the local timezone abbreviation (e.g. "PST") as a last
// resort when the IANA name cannot be determined.
func localZoneAbbrev() string {
	name, _ := time.Now().Zone()
	return name
}

// geoCountry returns a best-effort ISO 3166-1 alpha-2 country code derived from
// the process locale (LC_ALL / LC_MESSAGES / LANG), e.g. "US" from
// "en_US.UTF-8". Returns "" when no locale country can be determined.
func geoCountry() string {
	return geoCountryFrom(os.Getenv)
}

// geoCountryFrom is the testable core of geoCountry.
func geoCountryFrom(getenv func(string) string) string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if c := countryFromLocale(getenv(k)); c != "" {
			return c
		}
	}
	return ""
}

// countryFromLocale parses a POSIX locale string like "en_US.UTF-8" or "en_US"
// and returns the upper-cased two-letter country code ("US"), or "" if absent.
func countryFromLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" || strings.EqualFold(locale, "C") || strings.EqualFold(locale, "POSIX") {
		return ""
	}
	// Strip codeset / modifier: en_US.UTF-8@euro -> en_US
	if i := strings.IndexAny(locale, ".@"); i >= 0 {
		locale = locale[:i]
	}
	i := strings.IndexByte(locale, '_')
	if i < 0 || i+3 > len(locale) {
		return ""
	}
	cc := locale[i+1 : i+3]
	for _, r := range cc {
		if r < 'A' || (r > 'Z' && r < 'a') || r > 'z' {
			return ""
		}
	}
	return strings.ToUpper(cc)
}

// ---------------------------------------------------------------------------
// Mount-table parsing helpers (used by the Linux localMountType probe; kept
// here, platform-independent, so they remain unit-testable on any OS).
// ---------------------------------------------------------------------------

// parseMountinfoLine extracts the mount point (field 5) and filesystem type (the
// first field after the " - " separator) from a /proc/self/mountinfo line.
func parseMountinfoLine(line string) (mountPoint, fsType string, ok bool) {
	sep := strings.Index(line, " - ")
	if sep < 0 {
		return "", "", false
	}
	left := strings.Fields(line[:sep])
	right := strings.Fields(line[sep+len(" - "):])
	if len(left) < 5 || len(right) < 1 {
		return "", "", false
	}
	// Field 5 (index 4) is the mount point; octal-style escapes (e.g. \040 for
	// space) are left as-is, which is acceptable for prefix matching of typical
	// mount roots.
	return left[4], right[0], true
}

// pathHasMountPrefix reports whether mountPoint is the mount point covering path
// (either identical, the filesystem root "/", or a path-segment prefix).
func pathHasMountPrefix(path, mountPoint string) bool {
	if mountPoint == "/" || path == mountPoint {
		return true
	}
	return strings.HasPrefix(path, mountPoint+"/")
}

// classifyFSType maps a Linux filesystem type to a telemetry mount category:
// "nas-nfs" | "nas-smb" | "local-disk", or "" for an empty input.
func classifyFSType(fsType string) string {
	switch {
	case fsType == "":
		return ""
	case strings.HasPrefix(fsType, "nfs"): // nfs, nfs4
		return "nas-nfs"
	case fsType == "cifs" || strings.HasPrefix(fsType, "smb"): // cifs, smb3, smbfs
		return "nas-smb"
	default:
		return "local-disk"
	}
}
