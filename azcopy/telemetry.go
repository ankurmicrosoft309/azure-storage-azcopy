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
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-storage-azcopy/v10/common"
	"github.com/Azure/azure-storage-azcopy/v10/telemetry"
)

// defaultTelemetryConnectionString is the built-in Application Insights
// connection string used to emit anonymous usage telemetry. Telemetry is ON by
// default so that we collect usage from the broad customer base; customers can
// opt out via the --disable-telemetry flag or the AZCOPY_DISABLE_TELEMETRY
// environment variable.
//
// NOTE: replace the InstrumentationKey/IngestionEndpoint below with the real
// XClient App Insights resource values before release.
const defaultTelemetryConnectionString = "InstrumentationKey=00000000-0000-0000-0000-000000000000;IngestionEndpoint=https://centralus-2.in.applicationinsights.azure.com/;LiveEndpoint=https://centralus.livediagnostics.monitor.azure.com/"

// telemetryConnectionString is the Application Insights connection string used
// to emit anonymous usage telemetry. It defaults to the hardcoded value above
// and may be overridden at build time, e.g.:
//
//	-ldflags "-X github.com/Azure/azure-storage-azcopy/v10/azcopy.telemetryConnectionString=<conn>"
//
// or at runtime via AZCOPY_TELEMETRY_CONNECTION_STRING.
var telemetryConnectionString = defaultTelemetryConnectionString

// telemetryDisabledByFlag is set from the --disable-telemetry CLI flag (wired
// through ClientOptions.DisableTelemetry). When true, telemetry is disabled
// regardless of the connection string.
var telemetryDisabledByFlag bool

const (
	// envTelemetryConnectionString overrides the build-time connection string.
	envTelemetryConnectionString = "AZCOPY_TELEMETRY_CONNECTION_STRING"
	// envDisableTelemetry, when set to "true", disables telemetry entirely.
	envDisableTelemetry = "AZCOPY_DISABLE_TELEMETRY"
	// telemetrySendTimeout bounds how long a single send may block.
	telemetrySendTimeout = 5 * time.Second
	// installationIDFileName stores the anonymous, per-install identifier.
	installationIDFileName = "installation_id"
)

// telemetryAgent owns the (optional) telemetry reporter plus the cached,
// process-wide resource attributes. All methods are safe to call when the agent
// is nil or disabled, in which case they are no-ops.
type telemetryAgent struct {
	enabled  bool
	reporter *telemetry.Reporter
	resource telemetry.ResourceAttributes
}

var (
	telemetryOnce sync.Once
	telemetryInst *telemetryAgent
)

// getTelemetryAgent lazily builds the process-wide telemetry agent.
func getTelemetryAgent() *telemetryAgent {
	telemetryOnce.Do(func() {
		telemetryInst = newTelemetryAgent()
	})
	return telemetryInst
}

func newTelemetryAgent() *telemetryAgent {
	a := &telemetryAgent{}
	if telemetryDisabledByFlag || strings.EqualFold(os.Getenv(envDisableTelemetry), "true") {
		return a
	}
	conn := os.Getenv(envTelemetryConnectionString)
	if conn == "" {
		conn = telemetryConnectionString
	}
	if conn == "" {
		return a
	}
	a.reporter = telemetry.NewReporter(telemetry.Config{
		Backend:          telemetry.BackendAppInsights,
		ConnectionString: conn,
	})
	a.resource = buildResourceAttributes()
	a.enabled = true
	return a
}

// ReportCommandInvoked emits a single command.invoked telemetry event for the
// given command. It is intended for commands that do not run a transfer job and
// therefore do not emit job.started/job.finished (everything except copy and
// sync). It is best-effort and a no-op when telemetry is disabled.
func ReportCommandInvoked(command, runID string) {
	getTelemetryAgent().reportCommand(command, runID)
}

// reportStarted emits a job.started event asynchronously (best-effort). It never
// blocks the caller and never surfaces errors to the user.
func (a *telemetryAgent) reportStarted(dims telemetry.JobDimensions, runID string, start time.Time) {
	if a == nil || !a.enabled {
		return
	}
	evt := telemetry.JobStartedEvent{
		Resource:     a.resource,
		Dimensions:   dims,
		RunID:        runID,
		Timestamp:    start,
		StartedCount: 1,
	}
	go a.sendSafely(evt)
}

// reportFinished emits a job.finished event synchronously (bounded by
// telemetrySendTimeout) so the event is delivered before the process exits.
// Failures are logged to the job log only.
func (a *telemetryAgent) reportFinished(evt telemetry.JobFinishedEvent) {
	if a == nil || !a.enabled {
		return
	}
	a.sendSafely(evt)
}

// reportCommand emits a single command.invoked event synchronously (bounded by
// telemetrySendTimeout). Used for commands that do not emit job.started/finished
// (everything except copy and sync). Best-effort; no-op when disabled.
func (a *telemetryAgent) reportCommand(command, runID string) {
	if a == nil || !a.enabled {
		return
	}
	a.sendSafely(telemetry.CommandInvokedEvent{
		Resource:     a.resource,
		Command:      command,
		RunID:        runID,
		Timestamp:    time.Now(),
		InvokedCount: 1,
	})
}

func (a *telemetryAgent) sendSafely(evt telemetry.MetricEvent) {
	defer func() {
		if r := recover(); r != nil {
			common.LogToJobLogWithPrefix(fmt.Sprintf("telemetry: recovered from panic while sending %s: %v", evt.EventName(), r), common.LogError)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), telemetrySendTimeout)
	defer cancel()
	if err := a.reporter.ReportEvent(ctx, evt); err != nil {
		common.LogToJobLogWithPrefix(fmt.Sprintf("telemetry: failed to send %s: %v", evt.EventName(), err), common.LogWarning)
	}
}

// ---------------------------------------------------------------------------
// Resource attributes (host probe)
// ---------------------------------------------------------------------------

func buildResourceAttributes() telemetry.ResourceAttributes {
	hw := probeHostHardware()
	imds := probeIMDS()

	return telemetry.ResourceAttributes{
		ServiceName:        "azcopy",
		ServiceVersion:     common.AzcopyVersion,
		OSType:             runtime.GOOS,
		OSVersion:          hw.osVersion,
		HostArch:           runtime.GOARCH,
		HostNumCPU:         runtime.NumCPU(),
		HostCPUModel:       hw.cpuModel,
		HostMemoryTotalGB:  hw.memoryGB,
		HostNICSpeedMbps:   hw.nicMbps,
		HostVirtualization: virtualization(imds.isAzureVM),
		GeoRegion:          imds.region,
		GeoTimezone:        geoTimezone(),
		GeoCountry:         geoCountry(),
		NetworkRunContext:  networkRunContext(imds.isAzureVM),
		InstallationID:     installationID(),
		InvocationContext:  detectInvocationContext(os.Getenv),
	}
}

// virtualization maps the Azure-VM detection signal to the HostVirtualization
// attribute value.
func virtualization(isAzureVM bool) string {
	if isAzureVM {
		return "azure-vm"
	}
	return "unknown"
}

// networkRunContext maps the Azure-VM detection signal to the NetworkRunContext
// attribute value.
func networkRunContext(isAzureVM bool) string {
	if isAzureVM {
		return "azure-vm"
	}
	return "on-prem"
}

// installationID returns a stable, anonymous per-install identifier. It is a
// random 128-bit value persisted alongside the job plan files. It is NOT derived
// from any machine identity and contains no PII.
func installationID() string {
	if common.AzcopyJobPlanFolder == "" {
		return ""
	}
	p := filepath.Join(common.AzcopyJobPlanFolder, installationIDFileName)
	if b, err := os.ReadFile(p); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id
		}
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	id := hex.EncodeToString(buf)
	_ = os.WriteFile(p, []byte(id), 0600)
	return id
}

// detectInvocationContext infers how AzCopy was invoked. getenv is injected for
// testability.
func detectInvocationContext(getenv func(string) string) string {
	for _, k := range []string{"TF_BUILD", "GITHUB_ACTIONS", "CI", "JENKINS_URL", "GITLAB_CI", "BUILD_BUILDID"} {
		if getenv(k) != "" {
			return "ci"
		}
	}
	return "interactive"
}

// ---------------------------------------------------------------------------
// Job dimensions
// ---------------------------------------------------------------------------

func baseJobDimensions(command string, fromTo common.FromTo, srcCredType, dstCredType common.CredentialType) telemetry.JobDimensions {
	return telemetry.JobDimensions{
		Command:             command,
		FromTo:              fromTo.String(),
		SourceType:          fromTo.From().String(),
		DestType:            fromTo.To().String(),
		TransferDirection:   transferDirection(fromTo),
		TransferTopology:    transferTopology(fromTo),
		SourceProtocol:      protocolForLocation(fromTo.From()),
		SourceMountType:     mountTypeForLocation(fromTo.From()),
		DestProtocol:        protocolForLocation(fromTo.To()),
		SourceAuthMechanism: srcCredType.String(),
		DestAuthMechanism:   dstCredType.String(),
	}
}

// protocolForLocation maps a transfer endpoint location to the wire/access
// protocol used to reach it.
func protocolForLocation(loc common.Location) string {
	switch loc {
	case common.ELocation.Local():
		return "local"
	case common.ELocation.Blob(), common.ELocation.BlobFS(), common.ELocation.File():
		return "https"
	case common.ELocation.FileNFS():
		return "nfs"
	case common.ELocation.S3():
		return "s3"
	case common.ELocation.GCP():
		return "gcs"
	default:
		return ""
	}
}

// mountTypeForLocation classifies the storage backing an endpoint location at a
// coarse level (no path inspection). For local paths it reports "local-disk";
// callers that have the concrete local path should prefer sourceMountType to
// distinguish NAS (SMB/NFS) mounts.
func mountTypeForLocation(loc common.Location) string {
	switch {
	case loc == common.ELocation.Local():
		return "local-disk"
	case loc.IsAzure():
		return "cloud-azure"
	case loc == common.ELocation.S3():
		return "cloud-s3"
	case loc == common.ELocation.GCP():
		return "cloud-gcs"
	default:
		return ""
	}
}

// sourceMountType refines mountTypeForLocation for local sources by inspecting
// the OS mount table to distinguish network-attached storage from local disk:
// "nas-nfs" | "nas-smb" | "local-disk". For remote locations it defers to the
// coarse classification. localPath is ignored for non-local sources.
func sourceMountType(loc common.Location, localPath string) string {
	if loc != common.ELocation.Local() {
		return mountTypeForLocation(loc)
	}
	if mt := localMountType(localPath); mt != "" {
		return mt
	}
	return "local-disk"
}

// transferTopology summarizes the source->destination shape of the transfer.
// The on-prem vs. azure-vm aspect of *where AzCopy runs* is captured separately
// by the NetworkRunContext resource attribute.
func transferTopology(fromTo common.FromTo) string {
	switch {
	case fromTo.IsS2S():
		return "intra-azure"
	case fromTo.IsUpload():
		return "local-to-azure"
	case fromTo.IsDownload():
		return "azure-to-local"
	case fromTo.IsDelete():
		return "azure-delete"
	default:
		return "unknown"
	}
}

func transferDirection(fromTo common.FromTo) string {
	switch {
	case fromTo.IsUpload():
		return "upload"
	case fromTo.IsDownload():
		return "download"
	case fromTo.IsS2S():
		return "s2s"
	case fromTo.IsDelete():
		return "delete"
	default:
		return "unknown"
	}
}

// concurrencyValue returns the user-configured concurrency value, or 0 when it
// is unset or set to "AUTO".
func concurrencyValue() int {
	v := common.GetEnvironmentVariable(common.EEnvironmentVariable.ConcurrencyValue())
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0
	}
	return n
}

func bytesToMB(b int64) int {
	return int(b / (1024 * 1024))
}

// storageAccountName returns a customer-identifying remote resource name: the
// Azure storage account name (the first DNS label of the host) for Azure
// resources, e.g. "myaccount" from "https://myaccount.blob.core.windows.net/c",
// or the bucket name for S3/GCP resources. It returns "" for local resources or
// when the value cannot be parsed. Account/bucket names are globally unique and
// DNS-constrained, so no hashing is applied.
func storageAccountName(r common.ResourceString, loc common.Location) string {
	if loc.IsAzure() {
		host := hostOf(r)
		if host == "" {
			return ""
		}
		if i := strings.IndexByte(host, '.'); i > 0 {
			return host[:i]
		}
		return ""
	}
	switch loc {
	case common.ELocation.S3():
		if u, err := url.Parse(r.Value); err == nil {
			if p, err := common.NewS3URLParts(*u); err == nil {
				return p.BucketName
			}
		}
	case common.ELocation.GCP():
		if u, err := url.Parse(r.Value); err == nil {
			if p, err := common.NewGCPURLParts(*u); err == nil {
				return p.BucketName
			}
		}
	}
	return ""
}

// hostOf returns the lower-cased hostname of a resource URL, or "" when it
// cannot be parsed or has no host.
func hostOf(r common.ResourceString) string {
	u, err := url.Parse(r.Value)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// endpointKind reports whether the destination is reached via an Azure Private
// Endpoint ("private-endpoint") or the public endpoint ("public"). It returns ""
// for non-Azure destinations or when the host cannot be parsed.
func endpointKind(r common.ResourceString, loc common.Location) string {
	if !loc.IsAzure() {
		return ""
	}
	host := hostOf(r)
	if host == "" {
		return ""
	}
	if strings.Contains(host, ".privatelink.") {
		return "private-endpoint"
	}
	return "public"
}

// cloudType infers the Azure cloud environment ("public" | "gov" | "china" |
// "germany") from the source or destination host suffix. It returns "" when
// neither endpoint is an Azure location.
func cloudType(src common.ResourceString, srcLoc common.Location, dst common.ResourceString, dstLoc common.Location) string {
	if srcLoc.IsAzure() {
		if c := cloudTypeFromHost(hostOf(src)); c != "" {
			return c
		}
	}
	if dstLoc.IsAzure() {
		if c := cloudTypeFromHost(hostOf(dst)); c != "" {
			return c
		}
	}
	// At least one endpoint is Azure but the suffix was unrecognized: default to
	// public, which is by far the most common and matches unknown sovereign-less
	// hosts.
	if srcLoc.IsAzure() || dstLoc.IsAzure() {
		return "public"
	}
	return ""
}

// cloudTypeFromHost maps an Azure storage host suffix to a cloud environment.
func cloudTypeFromHost(host string) string {
	switch {
	case host == "":
		return ""
	case strings.HasSuffix(host, ".core.usgovcloudapi.net"):
		return "gov"
	case strings.HasSuffix(host, ".core.chinacloudapi.cn"):
		return "china"
	case strings.HasSuffix(host, ".core.cloudapi.de"):
		return "germany"
	case strings.HasSuffix(host, ".core.windows.net"), strings.HasSuffix(host, ".storage.azure.net"):
		return "public"
	default:
		return ""
	}
}

func copyJobDimensions(o *CookedTransferOptions, srcCredType, dstCredType common.CredentialType, capMbps float64) telemetry.JobDimensions {
	d := baseJobDimensions("copy", o.fromTo, srcCredType, dstCredType)
	d.SourceMountType = sourceMountType(o.fromTo.From(), o.source.Value)
	d.SourceStorageAccount = storageAccountName(o.source, o.fromTo.From())
	d.DestStorageAccount = storageAccountName(o.destination, o.fromTo.To())
	d.DestEndpointKind = endpointKind(o.destination, o.fromTo.To())
	d.CloudType = cloudType(o.source, o.fromTo.From(), o.destination, o.fromTo.To())
	d.BlobType = o.blobType.String()
	d.RequestedAccessTier = requestedAccessTier(o.blockBlobTier, o.pageBlobTier)
	d.OptRecursive = o.recursive
	d.OptOverwrite = o.forceWrite.String()
	d.OptPutMD5 = o.putMd5
	d.OptCapMbps = capMbps > 0
	d.OptPreserveSMBPermissions = o.preservePermissions.IsTruthy()
	d.OptBlockSizeMB = bytesToMB(o.blockSize)
	d.OptConcurrency = concurrencyValue()
	d.OptFlagsSet = copyFlagsSet(o)
	return d
}

func copyFlagsSet(o *CookedTransferOptions) []string {
	var f []string
	appendIf(&f, o.recursive, "recursive")
	appendIf(&f, o.putMd5, "put-md5")
	appendIf(&f, o.preservePermissions.IsTruthy(), "preserve-permissions")
	appendIf(&f, o.preserveInfo, "preserve-info")
	appendIf(&f, o.preservePosixProperties, "preserve-posix-properties")
	appendIf(&f, o.noGuessMimeType, "no-guess-mime-type")
	appendIf(&f, o.autoDecompress, "decompress")
	appendIf(&f, o.asSubdir, "as-subdir")
	appendIf(&f, o.backupMode, "backup")
	appendIf(&f, o.checkLength, "check-length")
	appendIf(&f, o.blockSize > 0, "block-size-mb")
	appendIf(&f, o.blobType != common.EBlobType.Detect(), "blob-type")
	return f
}

func syncJobDimensions(o *cookedSyncOptions, srcCredType, dstCredType common.CredentialType, capMbps float64) telemetry.JobDimensions {
	d := baseJobDimensions("sync", o.fromTo, srcCredType, dstCredType)
	d.SourceMountType = sourceMountType(o.fromTo.From(), o.source.Value)
	d.SourceStorageAccount = storageAccountName(o.source, o.fromTo.From())
	d.DestStorageAccount = storageAccountName(o.destination, o.fromTo.To())
	d.DestEndpointKind = endpointKind(o.destination, o.fromTo.To())
	d.CloudType = cloudType(o.source, o.fromTo.From(), o.destination, o.fromTo.To())
	d.OptRecursive = o.recursive
	d.OptPutMD5 = o.putMd5
	d.OptCapMbps = capMbps > 0
	d.OptPreserveSMBPermissions = o.preservePermissions.IsTruthy()
	d.OptBlockSizeMB = bytesToMB(o.blockSize)
	d.OptConcurrency = concurrencyValue()
	d.OptFlagsSet = syncFlagsSet(o)
	return d
}

func syncFlagsSet(o *cookedSyncOptions) []string {
	var f []string
	appendIf(&f, o.recursive, "recursive")
	appendIf(&f, o.putMd5, "put-md5")
	appendIf(&f, o.preservePermissions.IsTruthy(), "preserve-permissions")
	appendIf(&f, o.preserveInfo, "preserve-info")
	appendIf(&f, o.preservePosixProperties, "preserve-posix-properties")
	appendIf(&f, o.mirrorMode, "mirror-mode")
	appendIf(&f, o.deleteDestination != common.EDeleteDestination.False(), "delete-destination")
	appendIf(&f, o.compareHash != common.ESyncHashType.None(), "compare-hash")
	appendIf(&f, o.blockSize > 0, "block-size-mb")
	return f
}

func appendIf(dst *[]string, cond bool, name string) {
	if cond {
		*dst = append(*dst, name)
	}
}

// requestedAccessTier returns the access tier the user explicitly requested via
// --block-blob-tier / --page-blob-tier, or "None" when neither was set. Only one
// of the two is ever non-None in practice; the block-blob tier wins if both set.
func requestedAccessTier(blockTier common.BlockBlobTier, pageTier common.PageBlobTier) string {
	if blockTier != common.EBlockBlobTier.None() {
		return blockTier.String()
	}
	if pageTier != common.EPageBlobTier.None() {
		return pageTier.String()
	}
	return common.EBlockBlobTier.None().String()
}

// ---------------------------------------------------------------------------
// Finished event
// ---------------------------------------------------------------------------

func buildFinishedEvent(resource telemetry.ResourceAttributes, dims telemetry.JobDimensions, runID string, start, end time.Time, summary common.ListJobSummaryResponse, elapsed time.Duration) telemetry.JobFinishedEvent {
	dur := elapsed.Seconds()
	return telemetry.JobFinishedEvent{
		Resource:           resource,
		Dimensions:         dims,
		RunID:              runID,
		StartTimestamp:     start,
		EndTimestamp:       end,
		FinishedCount:      1,
		JobStatus:          summary.JobStatus.String(),
		BytesTransferred:   int64(summary.TotalBytesTransferred),
		BytesOverWire:      int64(summary.BytesOverWire),
		TransfersCompleted: int64(summary.TransfersCompleted),
		TransfersFailed:    int64(summary.TransfersFailed),
		TransfersSkipped:   int64(summary.TransfersSkipped),
		TransfersTotal:     int64(summary.TotalTransfers),
		DurationSeconds:    dur,
		ThroughputMbps:     throughputMbps(int64(summary.TotalBytesTransferred), dur),
		AvgE2ELatencyMs:    int64(summary.AverageE2EMilliseconds),
		AvgIOPS:            int64(summary.AverageIOPS),
		ServerBusyPct:      float64(summary.ServerBusyPercentage),
		NetworkErrorPct:    float64(summary.NetworkErrorPercentage),
		PercentComplete:    float64(summary.PercentComplete),
		FailureErrorCodes:  aggregateErrorCodes(summary.FailedTransfers),
	}
}

// maxErrorCodeBuckets bounds how many distinct error codes are reported so a job
// with many different failure codes cannot create an unbounded dimension value.
const maxErrorCodeBuckets = 10

// aggregateErrorCodes summarizes the error codes across failed transfers into a
// compact, bounded "code:count" histogram ordered by descending count (then by
// code for stability), e.g. "403:5,500:2". Only the numeric codes are included
// (no paths/messages), so the result contains no PII. Returns "" when empty.
func aggregateErrorCodes(failed []common.TransferDetail) string {
	if len(failed) == 0 {
		return ""
	}
	counts := make(map[int32]int)
	for _, t := range failed {
		counts[t.ErrorCode]++
	}
	type bucket struct {
		code  int32
		count int
	}
	buckets := make([]bucket, 0, len(counts))
	for code, count := range counts {
		buckets = append(buckets, bucket{code, count})
	}
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].count != buckets[j].count {
			return buckets[i].count > buckets[j].count
		}
		return buckets[i].code < buckets[j].code
	})
	if len(buckets) > maxErrorCodeBuckets {
		buckets = buckets[:maxErrorCodeBuckets]
	}
	parts := make([]string, 0, len(buckets))
	for _, b := range buckets {
		parts = append(parts, strconv.Itoa(int(b.code))+":"+strconv.Itoa(b.count))
	}
	return strings.Join(parts, ",")
}

func throughputMbps(bytes int64, durationSeconds float64) float64 {
	if durationSeconds <= 0 {
		return 0
	}
	return float64(bytes) * 8 / 1e6 / durationSeconds
}
