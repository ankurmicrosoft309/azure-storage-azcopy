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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-storage-azcopy/v10/common"
	"github.com/Azure/azure-storage-azcopy/v10/telemetry"
	"github.com/stretchr/testify/assert"
)

func TestTransferDirection(t *testing.T) {
	a := assert.New(t)
	a.Equal("upload", transferDirection(common.EFromTo.LocalBlob()))
	a.Equal("download", transferDirection(common.EFromTo.BlobLocal()))
	a.Equal("s2s", transferDirection(common.EFromTo.BlobBlob()))
	a.Equal("delete", transferDirection(common.EFromTo.BlobTrash()))
}

func TestBaseJobDimensions(t *testing.T) {
	a := assert.New(t)
	d := baseJobDimensions("copy", common.EFromTo.LocalBlob(), common.ECredentialType.OAuthToken(), common.ECredentialType.SharedKey())
	a.Equal("copy", d.Command)
	a.Equal(common.EFromTo.LocalBlob().String(), d.FromTo)
	a.Equal(common.ELocation.Local().String(), d.SourceType)
	a.Equal(common.ELocation.Blob().String(), d.DestType)
	a.Equal("upload", d.TransferDirection)
	a.Equal("local-to-azure", d.TransferTopology)
	a.Equal("local", d.SourceProtocol)
	a.Equal("local-disk", d.SourceMountType)
	a.Equal("https", d.DestProtocol)
	a.Equal(common.ECredentialType.OAuthToken().String(), d.SourceAuthMechanism)
	a.Equal(common.ECredentialType.SharedKey().String(), d.DestAuthMechanism)
}

func TestSourceMountType(t *testing.T) {
	a := assert.New(t)
	// Remote sources use the coarse cloud classification (no path inspection).
	a.Equal("cloud-azure", sourceMountType(common.ELocation.Blob(), ""))
	a.Equal("cloud-s3", sourceMountType(common.ELocation.S3(), ""))
	a.Equal("cloud-gcs", sourceMountType(common.ELocation.GCP(), ""))
	// A local path that cannot be classified falls back to local-disk (never empty).
	a.Equal("local-disk", sourceMountType(common.ELocation.Local(), "this-path-does-not-exist-xyz"))
}

func TestClassifyFSType(t *testing.T) {
	a := assert.New(t)
	a.Equal("nas-nfs", classifyFSType("nfs"))
	a.Equal("nas-nfs", classifyFSType("nfs4"))
	a.Equal("nas-smb", classifyFSType("cifs"))
	a.Equal("nas-smb", classifyFSType("smb3"))
	a.Equal("nas-smb", classifyFSType("smbfs"))
	a.Equal("local-disk", classifyFSType("ext4"))
	a.Equal("local-disk", classifyFSType("xfs"))
	a.Equal("", classifyFSType(""))
}

func TestParseMountinfoLine(t *testing.T) {
	a := assert.New(t)
	mp, fs, ok := parseMountinfoLine("36 35 98:0 / /mnt/nas rw,noatime - nfs4 1.2.3.4:/export rw")
	a.True(ok)
	a.Equal("/mnt/nas", mp)
	a.Equal("nfs4", fs)

	mp, fs, ok = parseMountinfoLine("22 30 0:21 / / rw,relatime shared:1 - ext4 /dev/root rw")
	a.True(ok)
	a.Equal("/", mp)
	a.Equal("ext4", fs)

	_, _, ok = parseMountinfoLine("garbage line without separator")
	a.False(ok)
}

func TestPathHasMountPrefix(t *testing.T) {
	a := assert.New(t)
	a.True(pathHasMountPrefix("/mnt/nas/data", "/mnt/nas"))
	a.True(pathHasMountPrefix("/mnt/nas", "/mnt/nas"))
	a.True(pathHasMountPrefix("/anything", "/"))
	a.False(pathHasMountPrefix("/mnt/nasextra", "/mnt/nas")) // not a path-segment prefix
	a.False(pathHasMountPrefix("/home/user", "/mnt/nas"))
}

func TestProtocolForLocation(t *testing.T) {
	a := assert.New(t)
	a.Equal("local", protocolForLocation(common.ELocation.Local()))
	a.Equal("https", protocolForLocation(common.ELocation.Blob()))
	a.Equal("https", protocolForLocation(common.ELocation.BlobFS()))
	a.Equal("https", protocolForLocation(common.ELocation.File()))
	a.Equal("nfs", protocolForLocation(common.ELocation.FileNFS()))
	a.Equal("s3", protocolForLocation(common.ELocation.S3()))
	a.Equal("gcs", protocolForLocation(common.ELocation.GCP()))
}

func TestMountTypeForLocation(t *testing.T) {
	a := assert.New(t)
	a.Equal("local-disk", mountTypeForLocation(common.ELocation.Local()))
	a.Equal("cloud-azure", mountTypeForLocation(common.ELocation.Blob()))
	a.Equal("cloud-azure", mountTypeForLocation(common.ELocation.FileNFS()))
	a.Equal("cloud-s3", mountTypeForLocation(common.ELocation.S3()))
	a.Equal("cloud-gcs", mountTypeForLocation(common.ELocation.GCP()))
}

func TestTransferTopology(t *testing.T) {
	a := assert.New(t)
	a.Equal("local-to-azure", transferTopology(common.EFromTo.LocalBlob()))
	a.Equal("azure-to-local", transferTopology(common.EFromTo.BlobLocal()))
	a.Equal("intra-azure", transferTopology(common.EFromTo.BlobBlob()))
	a.Equal("azure-delete", transferTopology(common.EFromTo.BlobTrash()))
}

func TestEndpointKind(t *testing.T) {
	a := assert.New(t)
	a.Equal("public", endpointKind(
		common.ResourceString{Value: "https://acct.blob.core.windows.net/c"},
		common.ELocation.Blob()))
	a.Equal("private-endpoint", endpointKind(
		common.ResourceString{Value: "https://acct.privatelink.blob.core.windows.net/c"},
		common.ELocation.Blob()))
	// Non-Azure destinations have no endpoint kind.
	a.Equal("", endpointKind(
		common.ResourceString{Value: "/local/path"},
		common.ELocation.Local()))
}

func TestCloudType(t *testing.T) {
	a := assert.New(t)
	pub := common.ResourceString{Value: "https://acct.blob.core.windows.net/c"}
	gov := common.ResourceString{Value: "https://acct.blob.core.usgovcloudapi.net/c"}
	china := common.ResourceString{Value: "https://acct.blob.core.chinacloudapi.cn/c"}
	local := common.ResourceString{Value: "/local/path"}

	a.Equal("public", cloudType(local, common.ELocation.Local(), pub, common.ELocation.Blob()))
	a.Equal("gov", cloudType(gov, common.ELocation.Blob(), local, common.ELocation.Local()))
	a.Equal("china", cloudType(local, common.ELocation.Local(), china, common.ELocation.Blob()))
	// Azure host with unknown suffix defaults to public.
	a.Equal("public", cloudType(local, common.ELocation.Local(),
		common.ResourceString{Value: "https://acct.blob.example.com/c"}, common.ELocation.Blob()))
	// Neither endpoint Azure -> empty.
	a.Equal("", cloudType(local, common.ELocation.Local(), local, common.ELocation.Local()))
}

func TestCountryFromLocale(t *testing.T) {
	a := assert.New(t)
	a.Equal("US", countryFromLocale("en_US.UTF-8"))
	a.Equal("GB", countryFromLocale("en_GB"))
	a.Equal("DE", countryFromLocale("de_DE.UTF-8@euro"))
	a.Equal("", countryFromLocale("C"))
	a.Equal("", countryFromLocale("POSIX"))
	a.Equal("", countryFromLocale(""))
	a.Equal("", countryFromLocale("en"))
}

func TestZoneNameFromZoneinfoPath(t *testing.T) {
	a := assert.New(t)
	a.Equal("America/Los_Angeles", zoneNameFromZoneinfoPath("/usr/share/zoneinfo/America/Los_Angeles"))
	a.Equal("Europe/Berlin", zoneNameFromZoneinfoPath("../usr/share/zoneinfo/Europe/Berlin"))
	a.Equal("", zoneNameFromZoneinfoPath("/etc/localtime"))
}

func TestGeoTimezoneFrom(t *testing.T) {
	a := assert.New(t)
	// TZ env takes precedence.
	tz := geoTimezoneFrom(
		func(k string) string {
			if k == "TZ" {
				return "America/New_York"
			}
			return ""
		},
		func(string) ([]byte, error) { return nil, assertErr() },
		func(string) (string, error) { return "", assertErr() },
		func() string { return "UTC" })
	a.Equal("America/New_York", tz)

	// Falls back through /etc/timezone.
	tz = geoTimezoneFrom(
		func(string) string { return "" },
		func(p string) ([]byte, error) {
			if p == "/etc/timezone" {
				return []byte("Europe/London\n"), nil
			}
			return nil, assertErr()
		},
		func(string) (string, error) { return "", assertErr() },
		func() string { return "UTC" })
	a.Equal("Europe/London", tz)

	// Falls back through /etc/localtime symlink.
	tz = geoTimezoneFrom(
		func(string) string { return "" },
		func(string) ([]byte, error) { return nil, assertErr() },
		func(string) (string, error) { return "/usr/share/zoneinfo/Asia/Kolkata", nil },
		func() string { return "UTC" })
	a.Equal("Asia/Kolkata", tz)

	// Ultimate fallback to zone abbreviation.
	tz = geoTimezoneFrom(
		func(string) string { return "" },
		func(string) ([]byte, error) { return nil, assertErr() },
		func(string) (string, error) { return "", assertErr() },
		func() string { return "PST" })
	a.Equal("PST", tz)
}

func TestVirtualizationAndNetworkContext(t *testing.T) {
	a := assert.New(t)
	a.Equal("azure-vm", virtualization(true))
	a.Equal("unknown", virtualization(false))
	a.Equal("azure-vm", networkRunContext(true))
	a.Equal("on-prem", networkRunContext(false))
}

func assertErr() error { return errTest }

var errTest = errors.New("test error")

func TestCopyJobDimensions(t *testing.T) {
	a := assert.New(t)
	o := &CookedTransferOptions{
		fromTo:              common.EFromTo.LocalBlob(),
		blobType:            common.EBlobType.BlockBlob(),
		recursive:           true,
		forceWrite:          common.EOverwriteOption.True(),
		putMd5:              true,
		preservePermissions: common.NewPreservePermissionsOption(true, true, common.EFromTo.LocalBlob()),
		blockSize:           8 * 1024 * 1024,
		blockBlobTier:       common.EBlockBlobTier.Cool(),
	}
	d := copyJobDimensions(o, common.ECredentialType.Anonymous(), common.ECredentialType.OAuthToken(), 0)
	a.Equal("copy", d.Command)
	a.Equal(common.EBlobType.BlockBlob().String(), d.BlobType)
	a.Equal(common.EBlockBlobTier.Cool().String(), d.RequestedAccessTier)
	a.True(d.OptRecursive)
	a.Equal(common.EOverwriteOption.True().String(), d.OptOverwrite)
	a.True(d.OptPutMD5)
	a.True(d.OptPreserveSMBPermissions)
	a.Equal(8, d.OptBlockSizeMB)
	a.False(d.OptCapMbps)
	a.Equal(common.ECredentialType.Anonymous().String(), d.SourceAuthMechanism)
	a.Equal(common.ECredentialType.OAuthToken().String(), d.DestAuthMechanism)
	a.Contains(d.OptFlagsSet, "recursive")
	a.Contains(d.OptFlagsSet, "put-md5")
	a.Contains(d.OptFlagsSet, "block-size-mb")

	// cap-mbps set should flip OptCapMbps.
	a.True(copyJobDimensions(o, common.ECredentialType.Anonymous(), common.ECredentialType.OAuthToken(), 100).OptCapMbps)
}

func TestSyncJobDimensions(t *testing.T) {
	a := assert.New(t)
	o := &cookedSyncOptions{
		fromTo:            common.EFromTo.LocalBlob(),
		recursive:         true,
		putMd5:            false,
		blockSize:         4 * 1024 * 1024,
		deleteDestination: common.EDeleteDestination.True(),
		mirrorMode:        true,
	}
	d := syncJobDimensions(o, common.ECredentialType.SharedKey(), common.ECredentialType.Anonymous(), 0)
	a.Equal("sync", d.Command)
	a.True(d.OptRecursive)
	a.Equal(4, d.OptBlockSizeMB)
	a.Contains(d.OptFlagsSet, "recursive")
	a.Contains(d.OptFlagsSet, "delete-destination")
	a.Contains(d.OptFlagsSet, "mirror-mode")
	a.NotContains(d.OptFlagsSet, "put-md5")
}

func TestBuildFinishedEvent(t *testing.T) {
	a := assert.New(t)
	start := time.Now()
	end := start.Add(2 * time.Second)
	summary := common.ListJobSummaryResponse{
		JobStatus:              common.EJobStatus.Completed(),
		TotalBytesTransferred:  1000000,
		BytesOverWire:          1100000,
		TransfersCompleted:     10,
		TransfersFailed:        1,
		TransfersSkipped:       2,
		TotalTransfers:         13,
		AverageE2EMilliseconds: 50,
		AverageIOPS:            7,
		ServerBusyPercentage:   1.5,
		NetworkErrorPercentage: 0.5,
		PercentComplete:        100,
		FailedTransfers: []common.TransferDetail{
			{ErrorCode: 403}, {ErrorCode: 500}, {ErrorCode: 403}, {ErrorCode: 403},
		},
	}
	dims := baseJobDimensions("copy", common.EFromTo.LocalBlob(), common.ECredentialType.OAuthToken(), common.ECredentialType.SharedKey())
	evt := buildFinishedEvent(telemetryResourceForTest(), dims, "job-1234", start, end, summary, 2*time.Second)

	a.Equal("job-1234", evt.RunID)
	a.Equal(int64(1), evt.FinishedCount)
	a.Equal(common.EJobStatus.Completed().String(), evt.JobStatus)
	a.Equal("403:3,500:1", evt.FailureErrorCodes)
	a.Equal(100.0, evt.PercentComplete)
	a.Equal(int64(1000000), evt.BytesTransferred)
	a.Equal(int64(1100000), evt.BytesOverWire)
	a.Equal(int64(10), evt.TransfersCompleted)
	a.Equal(int64(1), evt.TransfersFailed)
	a.Equal(int64(2), evt.TransfersSkipped)
	a.Equal(int64(13), evt.TransfersTotal)
	a.Equal(2.0, evt.DurationSeconds)
	a.InDelta(4.0, evt.ThroughputMbps, 1e-9) // 1e6 bytes * 8 / 1e6 / 2s
	a.Equal(int64(50), evt.AvgE2ELatencyMs)
	a.Equal(int64(7), evt.AvgIOPS)
}

func TestRequestedAccessTier(t *testing.T) {
	a := assert.New(t)
	// Neither set -> None.
	a.Equal(common.EBlockBlobTier.None().String(),
		requestedAccessTier(common.EBlockBlobTier.None(), common.EPageBlobTier.None()))
	// Block-blob tier set.
	a.Equal(common.EBlockBlobTier.Archive().String(),
		requestedAccessTier(common.EBlockBlobTier.Archive(), common.EPageBlobTier.None()))
	// Page-blob tier set.
	a.Equal(common.EPageBlobTier.P10().String(),
		requestedAccessTier(common.EBlockBlobTier.None(), common.EPageBlobTier.P10()))
}

func TestAggregateErrorCodes(t *testing.T) {
	a := assert.New(t)
	// No failures -> empty.
	a.Equal("", aggregateErrorCodes(nil))
	a.Equal("", aggregateErrorCodes([]common.TransferDetail{}))
	// Ordered by descending count, then ascending code.
	a.Equal("403:3,500:1", aggregateErrorCodes([]common.TransferDetail{
		{ErrorCode: 500}, {ErrorCode: 403}, {ErrorCode: 403}, {ErrorCode: 403},
	}))
	// Tie on count -> lower code first.
	a.Equal("404:1,409:1", aggregateErrorCodes([]common.TransferDetail{
		{ErrorCode: 409}, {ErrorCode: 404},
	}))
	// Bounded to maxErrorCodeBuckets distinct codes.
	many := make([]common.TransferDetail, 0, maxErrorCodeBuckets+5)
	for i := 0; i < maxErrorCodeBuckets+5; i++ {
		many = append(many, common.TransferDetail{ErrorCode: int32(600 + i)})
	}
	res := aggregateErrorCodes(many)
	a.Equal(maxErrorCodeBuckets, strings.Count(res, ":"))
}

func TestStorageAccountName(t *testing.T) {
	a := assert.New(t)
	// Azure remote: first DNS label is the account name.
	a.Equal("myaccount", storageAccountName(
		common.ResourceString{Value: "https://myaccount.blob.core.windows.net/container/path"},
		common.ELocation.Blob()))
	a.Equal("acct2", storageAccountName(
		common.ResourceString{Value: "https://acct2.dfs.core.windows.net/fs"},
		common.ELocation.BlobFS()))
	// Local locations return empty.
	a.Equal("", storageAccountName(
		common.ResourceString{Value: "/local/path"},
		common.ELocation.Local()))
	// S3/GCP return the bucket name.
	a.Equal("bucket", storageAccountName(
		common.ResourceString{Value: "https://bucket.s3.amazonaws.com/key"},
		common.ELocation.S3()))
	a.Equal("mybucket", storageAccountName(
		common.ResourceString{Value: "https://storage.cloud.google.com/mybucket/object"},
		common.ELocation.GCP()))
	// Hostless values return empty.
	a.Equal("", storageAccountName(
		common.ResourceString{Value: "relative/path"},
		common.ELocation.Blob()))
}

func TestThroughputMbps(t *testing.T) {
	a := assert.New(t)
	a.Equal(0.0, throughputMbps(1000, 0))
	a.Equal(0.0, throughputMbps(1000, -1))
	a.InDelta(8.0, throughputMbps(1_000_000, 1), 1e-9)
}

func TestDetectInvocationContext(t *testing.T) {
	a := assert.New(t)
	a.Equal("interactive", detectInvocationContext(func(string) string { return "" }))
	a.Equal("ci", detectInvocationContext(func(k string) string {
		if k == "GITHUB_ACTIONS" {
			return "true"
		}
		return ""
	}))
}

func TestDisabledAgentIsNoop(t *testing.T) {
	a := assert.New(t)
	var agent *telemetryAgent
	// nil agent must not panic.
	agent.reportStarted(telemetry.JobDimensions{}, "id", time.Now())
	agent.reportFinished(telemetry.JobFinishedEvent{})

	disabled := &telemetryAgent{enabled: false}
	disabled.reportStarted(telemetry.JobDimensions{}, "id", time.Now())
	disabled.reportFinished(telemetry.JobFinishedEvent{})
	a.False(disabled.enabled)
}

func telemetryResourceForTest() telemetry.ResourceAttributes {
	return telemetry.ResourceAttributes{
		ServiceName:    "azcopy",
		ServiceVersion: common.AzcopyVersion,
	}
}
