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

// Package telemetry defines the AzCopy telemetry event model and the reporter
// used to send those events to Azure Monitor (Application Insights).
//
// Two events mirror what AzCopy emits per run:
//
//	Event 1 (azcopy.job.started)  - emitted at job start.
//	Event 2 (azcopy.job.finished) - emitted at job completion, with measurements.
package telemetry

import (
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Resource attributes - machine/system facts, constant for the process.
// Attached to BOTH metric events.
// ---------------------------------------------------------------------------

type ResourceAttributes struct {
	ServiceName        string // "azcopy"
	ServiceVersion     string // "10.32.2"
	OSType             string // runtime.GOOS
	OSVersion          string // uname / RtlGetVersion
	HostArch           string // runtime.GOARCH
	HostNumCPU         int    // runtime.NumCPU()
	HostCPUModel       string // /proc/cpuinfo, WMI, sysctl
	HostMemoryTotalGB  int    // total physical memory
	HostNICSpeedMbps   int    // best-effort; -1 when unavailable
	HostVirtualization string // "azure-vm" | "unknown"
	GeoRegion          string // IMDS compute.location, else timezone bucket
	GeoTimezone        string // IANA timezone
	GeoCountry         string // ISO 3166 country name, e.g. "United States"
	NetworkRunContext  string // "azure-vm" | "on-prem" | "unknown"
	InstallationID     string // anonymous, stable per-install identifier (no PII)
	InvocationContext  string // "interactive" | "ci" | "sdk" | "unknown"
}

func (ra ResourceAttributes) props() map[string]string {
	return map[string]string{
		"ServiceName":        ra.ServiceName,
		"ServiceVersion":     ra.ServiceVersion,
		"OSType":             ra.OSType,
		"OSVersion":          ra.OSVersion,
		"HostArch":           ra.HostArch,
		"HostNumCPU":         strconv.Itoa(ra.HostNumCPU),
		"HostCPUModel":       ra.HostCPUModel,
		"HostMemoryTotalGB":  strconv.Itoa(ra.HostMemoryTotalGB),
		"HostNICSpeedMbps":   strconv.Itoa(ra.HostNICSpeedMbps),
		"HostVirtualization": ra.HostVirtualization,
		"GeoRegion":          ra.GeoRegion,
		"GeoTimezone":        ra.GeoTimezone,
		"GeoCountry":         ra.GeoCountry,
		"NetworkRunContext":  ra.NetworkRunContext,
		"InstallationID":     ra.InstallationID,
		"InvocationContext":  ra.InvocationContext,
	}
}

// ---------------------------------------------------------------------------
// Job dimension attributes - job-specific facts attached to each data point.
// Present on BOTH events.
// ---------------------------------------------------------------------------

type JobDimensions struct {
	Command                   string // "copy" | "sync" | "remove"
	FromTo                    string // "LocalBlob", "BlobLocal", "S3Blob", ...
	SourceType                string // "Local" | "Blob" | "File" | "S3" | ...
	DestType                  string
	TransferDirection         string // "upload" | "download" | "s2s" | "delete"
	TransferTopology          string // "onprem-to-azure" | "intra-azure" | ...
	SourceProtocol            string // "smb" | "nfs" | "local" | "https" | "s3" | "gcs"
	SourceMountType           string // "nas-smb" | "nas-nfs" | "local-disk" | "cloud-azure" | ...
	SourceStorageAccount      string // Azure account name OR S3/GCP bucket name; HIGH cardinality (empty for local)
	DestProtocol              string
	DestStorageAccount        string // Azure account name OR S3/GCP bucket name; HIGH cardinality
	DestEndpointKind          string // "public" | "private-endpoint"
	CloudType                 string // "public" | "gov" | "china" | "germany"
	SourceAuthMechanism       string // "OAuthToken" | "Anonymous" | "SharedKey" | ...
	DestAuthMechanism         string // "OAuthToken" | "Anonymous" | "SharedKey" | ...
	BlobType                  string // "BlockBlob" | "PageBlob" | "AppendBlob" | "Detect"
	RequestedAccessTier       string // requested --block-blob-tier/--page-blob-tier, "None" when unset
	OptRecursive              bool
	OptOverwrite              string // "true" | "false" | "ifSourceNewer" | "prompt"
	OptCapMbps                bool   // whether --cap-mbps was set (not the value)
	OptPreserveSMBPermissions bool
	OptPutMD5                 bool
	OptBlockSizeMB            int
	OptConcurrency            int      // effective concurrency (AZCOPY_CONCURRENCY_VALUE)
	OptFlagsSet               []string // names of non-default CLI flags (no values)
}

func (jd JobDimensions) props() map[string]string {
	return map[string]string{
		"Command":                   jd.Command,
		"FromTo":                    jd.FromTo,
		"SourceType":                jd.SourceType,
		"DestType":                  jd.DestType,
		"TransferDirection":         jd.TransferDirection,
		"TransferTopology":          jd.TransferTopology,
		"SourceProtocol":            jd.SourceProtocol,
		"SourceMountType":           jd.SourceMountType,
		"SourceStorageAccount":      jd.SourceStorageAccount,
		"DestProtocol":              jd.DestProtocol,
		"DestStorageAccount":        jd.DestStorageAccount,
		"DestEndpointKind":          jd.DestEndpointKind,
		"CloudType":                 jd.CloudType,
		"SourceAuthMechanism":       jd.SourceAuthMechanism,
		"DestAuthMechanism":         jd.DestAuthMechanism,
		"BlobType":                  jd.BlobType,
		"RequestedAccessTier":       jd.RequestedAccessTier,
		"OptRecursive":              strconv.FormatBool(jd.OptRecursive),
		"OptOverwrite":              jd.OptOverwrite,
		"OptCapMbps":                strconv.FormatBool(jd.OptCapMbps),
		"OptPreserveSMBPermissions": strconv.FormatBool(jd.OptPreserveSMBPermissions),
		"OptPutMD5":                 strconv.FormatBool(jd.OptPutMD5),
		"OptBlockSizeMB":            strconv.Itoa(jd.OptBlockSizeMB),
		"OptConcurrency":            strconv.Itoa(jd.OptConcurrency),
		"OptFlagsSet":               truncateValue(strings.Join(jd.OptFlagsSet, ",")),
	}
}

// ---------------------------------------------------------------------------
// Event 1: job.started
// ---------------------------------------------------------------------------

type JobStartedEvent struct {
	Resource   ResourceAttributes
	Dimensions JobDimensions
	// RunID correlates this job.started event with its matching job.finished
	// event. Both events for a single run carry the same value (typically the
	// AzCopy JobID). Emitted as the "RunID" property.
	RunID        string
	Timestamp    time.Time
	StartedCount int64 // monotonic counter increment, always 1
}

// ---------------------------------------------------------------------------
// Event 2: job.finished (+ measurements)
// ---------------------------------------------------------------------------

type JobFinishedEvent struct {
	Resource   ResourceAttributes
	Dimensions JobDimensions
	// RunID correlates this job.finished event with its matching job.started
	// event. Both events for a single run carry the same value (typically the
	// AzCopy JobID). Emitted as the "RunID" property.
	RunID          string
	StartTimestamp time.Time
	EndTimestamp   time.Time

	FinishedCount int64  // monotonic counter increment, always 1
	JobStatus     string // "Completed" | "CompletedWithErrors" | "Failed" | "Cancelled" | ...

	// FailureErrorCodes is a compact, bounded histogram of the error codes seen
	// across failed transfers, e.g. "403:5,500:2" (ordered by descending count).
	// Empty when there were no failures. Contains no PII (only numeric codes).
	FailureErrorCodes string

	// Measurements (from ListJobSummaryResponse + ElapsedTime)
	BytesTransferred   int64   // TotalBytesTransferred (no retries)
	BytesOverWire      int64   // BytesOverWire (includes retries)
	TransfersCompleted int64   // TransfersCompleted
	TransfersFailed    int64   // TransfersFailed
	TransfersSkipped   int64   // TransfersSkipped
	TransfersTotal     int64   // TotalTransfers
	DurationSeconds    float64 // ElapsedTime.Seconds()
	ThroughputMbps     float64 // BytesTransferred * 8 / 1e6 / DurationSeconds
	AvgE2ELatencyMs    int64   // AverageE2EMilliseconds
	AvgIOPS            int64   // AverageIOPS
	ServerBusyPct      float64 // ServerBusyPercentage
	NetworkErrorPct    float64 // NetworkErrorPercentage
	PercentComplete    float64 // PercentComplete (0-100); useful especially for cancelled jobs
}

// namedMetric is a single numeric measurement to be sent to a backend.
type namedMetric struct {
	Name  string
	Value float64
	Count int
}

// MetricEvent is implemented by every telemetry event the telemetry package can
// process and send. A single Reporter can therefore handle either event type.
type MetricEvent interface {
	// EventName returns the metric/event name (e.g. "azcopy.job.started").
	EventName() string
	// attributes returns the flattened resource + dimension properties.
	attributes() map[string]string
	// measurements returns the numeric data points to send.
	measurements() []namedMetric
	// timestamp returns the event time to stamp on the telemetry.
	timestamp() time.Time
}

func (JobStartedEvent) EventName() string  { return "azcopy.job.started" }
func (JobFinishedEvent) EventName() string { return "azcopy.job.finished" }

func (e JobStartedEvent) timestamp() time.Time  { return e.Timestamp }
func (e JobFinishedEvent) timestamp() time.Time { return e.EndTimestamp }

func (e JobStartedEvent) attributes() map[string]string {
	attrs := mergeProps(e.Resource.props(), e.Dimensions.props())
	attrs["RunID"] = e.RunID
	return attrs
}

func (e JobFinishedEvent) attributes() map[string]string {
	attrs := mergeProps(e.Resource.props(), e.Dimensions.props())
	attrs["RunID"] = e.RunID
	attrs["JobStatus"] = e.JobStatus
	if e.FailureErrorCodes != "" {
		attrs["FailureErrorCodes"] = e.FailureErrorCodes
	}
	return attrs
}

func (e JobStartedEvent) measurements() []namedMetric {
	return []namedMetric{
		{Name: "azcopy.job.started", Value: float64(e.StartedCount), Count: 1},
	}
}

func (e JobFinishedEvent) measurements() []namedMetric {
	return []namedMetric{
		{Name: "azcopy.job.finished", Value: float64(e.FinishedCount), Count: 1},
		{Name: "azcopy.bytes_transferred", Value: float64(e.BytesTransferred), Count: 1},
		{Name: "azcopy.bytes_over_wire", Value: float64(e.BytesOverWire), Count: 1},
		{Name: "azcopy.transfers_completed", Value: float64(e.TransfersCompleted), Count: 1},
		{Name: "azcopy.transfers_failed", Value: float64(e.TransfersFailed), Count: 1},
		{Name: "azcopy.transfers_skipped", Value: float64(e.TransfersSkipped), Count: 1},
		{Name: "azcopy.transfers_total", Value: float64(e.TransfersTotal), Count: 1},
		{Name: "azcopy.duration_seconds", Value: e.DurationSeconds, Count: 1},
		{Name: "azcopy.throughput_mbps", Value: e.ThroughputMbps, Count: 1},
		{Name: "azcopy.avg_e2e_latency_ms", Value: float64(e.AvgE2ELatencyMs), Count: 1},
		{Name: "azcopy.avg_iops", Value: float64(e.AvgIOPS), Count: 1},
		{Name: "azcopy.server_busy_pct", Value: e.ServerBusyPct, Count: 1},
		{Name: "azcopy.network_error_pct", Value: e.NetworkErrorPct, Count: 1},
		{Name: "azcopy.percent_complete", Value: e.PercentComplete, Count: 1},
	}
}

// ---------------------------------------------------------------------------
// Event 3: command.invoked
//
// Emitted once for commands that do not run a transfer job and therefore do not
// emit job.started/job.finished (everything except copy and sync), e.g.
// benchmark, login, logout, remove, resume, list, make, set-properties. It is a
// lightweight usage signal carrying only the resource attributes plus the
// command name, so we can answer "which subcommands do customers use?".
// ---------------------------------------------------------------------------

type CommandInvokedEvent struct {
	Resource ResourceAttributes
	Command  string // "benchmark" | "login" | "logout" | "remove" | "resume" | ...
	// RunID is the AzCopy JobID for this invocation when one exists, else empty.
	RunID        string
	Timestamp    time.Time
	InvokedCount int64 // monotonic counter increment, always 1
}

func (CommandInvokedEvent) EventName() string { return "azcopy.command.invoked" }

func (e CommandInvokedEvent) timestamp() time.Time { return e.Timestamp }

func (e CommandInvokedEvent) attributes() map[string]string {
	attrs := e.Resource.props()
	attrs["Command"] = e.Command
	if e.RunID != "" {
		attrs["RunID"] = e.RunID
	}
	return attrs
}

func (e CommandInvokedEvent) measurements() []namedMetric {
	return []namedMetric{
		{Name: "azcopy.command.invoked", Value: float64(e.InvokedCount), Count: 1},
	}
}

// maxPropValueLen bounds the length of a property value (e.g. OptFlagsSet) so a
// run with many CLI flags cannot bloat the telemetry payload. Values longer
// than this are truncated with a trailing marker.
const maxPropValueLen = 1024

// truncateValue caps a property value at maxPropValueLen, appending a marker
// when truncation occurs.
func truncateValue(v string) string {
	const marker = "...(truncated)"
	if len(v) <= maxPropValueLen {
		return v
	}
	return v[:maxPropValueLen-len(marker)] + marker
}

// mergeProps merges the given property maps into a single map. Later maps win
// on key collisions.
func mergeProps(maps ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
