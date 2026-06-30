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

package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testConnString = "InstrumentationKey=11111111-2222-3333-4444-555555555555;IngestionEndpoint=https://eastus.example.com/"

// stubClient is an httpDoer that records the last request it received and
// returns a canned response (or error), so we can assert on telemetry traffic
// without making real network calls.
type stubClient struct {
	lastReq  *http.Request
	lastBody []byte
	status   int
	respBody string
	err      error
	calls    int
}

func (c *stubClient) Do(req *http.Request) (*http.Response, error) {
	c.calls++
	c.lastReq = req
	if req.Body != nil {
		c.lastBody, _ = io.ReadAll(req.Body)
	}
	if c.err != nil {
		return nil, c.err
	}
	status := c.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader([]byte(c.respBody))),
		Header:     make(http.Header),
	}, nil
}

func sampleStarted() JobStartedEvent {
	ts := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	return JobStartedEvent{
		Resource: ResourceAttributes{
			ServiceName:      "azcopy",
			ServiceVersion:   "10.32.2",
			OSType:           "linux",
			HostArch:         "amd64",
			HostNumCPU:       8,
			HostNICSpeedMbps: -1,
			InstallationID:   "abc123",
		},
		Dimensions: JobDimensions{
			Command:        "copy",
			FromTo:         "LocalBlob",
			SourceType:     "Local",
			DestType:       "Blob",
			OptRecursive:   true,
			OptBlockSizeMB: 8,
			OptFlagsSet:    []string{"--recursive", "--put-md5"},
		},
		RunID:        "job-1234",
		Timestamp:    ts,
		StartedCount: 1,
	}
}

func sampleFinished() JobFinishedEvent {
	start := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	return JobFinishedEvent{
		Resource:           sampleStarted().Resource,
		Dimensions:         sampleStarted().Dimensions,
		RunID:              "job-1234",
		StartTimestamp:     start,
		EndTimestamp:       start.Add(time.Minute),
		FinishedCount:      1,
		JobStatus:          "Completed",
		BytesTransferred:   1024,
		BytesOverWire:      1100,
		TransfersCompleted: 10,
		TransfersFailed:    1,
		TransfersSkipped:   2,
		TransfersTotal:     13,
		DurationSeconds:    60,
		ThroughputMbps:     0.1365,
		AvgE2ELatencyMs:    42,
		AvgIOPS:            100,
		ServerBusyPct:      1.5,
		NetworkErrorPct:    0.2,
		PercentComplete:    100,
	}
}

func TestParseConnectionString(t *testing.T) {
	m := parseConnectionString(testConnString)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", m["InstrumentationKey"])
	assert.Equal(t, "https://eastus.example.com/", m["IngestionEndpoint"])

	// Tolerates whitespace and ignores malformed segments.
	m = parseConnectionString(" A = 1 ; bogus ; B=2")
	assert.Equal(t, "1", m["A"])
	assert.Equal(t, "2", m["B"])
	_, ok := m["bogus"]
	assert.False(t, ok)
}

func TestEndpointAndKey(t *testing.T) {
	r := NewReporter(Config{ConnectionString: testConnString})
	endpoint, ikey, err := r.endpointAndKey()
	require.NoError(t, err)
	assert.Equal(t, "https://eastus.example.com", endpoint) // trailing slash trimmed
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", ikey)

	_, _, err = NewReporter(Config{ConnectionString: "garbage"}).endpointAndKey()
	assert.Error(t, err)
}

func TestEventNamesAndTimestamps(t *testing.T) {
	s := sampleStarted()
	f := sampleFinished()
	assert.Equal(t, "azcopy.job.started", s.EventName())
	assert.Equal(t, "azcopy.job.finished", f.EventName())
	assert.Equal(t, s.Timestamp, s.timestamp())
	assert.Equal(t, f.EndTimestamp, f.timestamp())
}

func TestStartedMeasurements(t *testing.T) {
	m := sampleStarted().measurements()
	require.Len(t, m, 1)
	assert.Equal(t, "azcopy.job.started", m[0].Name)
	assert.Equal(t, float64(1), m[0].Value)
}

func TestFinishedMeasurements(t *testing.T) {
	m := sampleFinished().measurements()
	byName := map[string]float64{}
	for _, nm := range m {
		byName[nm.Name] = nm.Value
	}
	assert.Equal(t, float64(1024), byName["azcopy.bytes_transferred"])
	assert.Equal(t, float64(1100), byName["azcopy.bytes_over_wire"])
	assert.Equal(t, float64(10), byName["azcopy.transfers_completed"])
	assert.Equal(t, float64(1), byName["azcopy.transfers_failed"])
	assert.Equal(t, float64(2), byName["azcopy.transfers_skipped"])
	assert.Equal(t, float64(13), byName["azcopy.transfers_total"])
	assert.Equal(t, float64(60), byName["azcopy.duration_seconds"])
	assert.Equal(t, float64(42), byName["azcopy.avg_e2e_latency_ms"])
	assert.Equal(t, float64(100), byName["azcopy.avg_iops"])
	assert.InDelta(t, 1.5, byName["azcopy.server_busy_pct"], 1e-9)
	assert.InDelta(t, 0.2, byName["azcopy.network_error_pct"], 1e-9)
	assert.Equal(t, float64(100), byName["azcopy.percent_complete"])
	assert.Len(t, m, 14)
}

func TestCommandInvokedEvent(t *testing.T) {
	ts := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	e := CommandInvokedEvent{
		Resource:     sampleStarted().Resource,
		Command:      "login",
		RunID:        "job-9999",
		Timestamp:    ts,
		InvokedCount: 1,
	}
	assert.Equal(t, "azcopy.command.invoked", e.EventName())
	assert.Equal(t, ts, e.timestamp())

	m := e.measurements()
	require.Len(t, m, 1)
	assert.Equal(t, "azcopy.command.invoked", m[0].Name)
	assert.Equal(t, float64(1), m[0].Value)

	attrs := e.attributes()
	assert.Equal(t, "login", attrs["Command"])
	assert.Equal(t, "job-9999", attrs["RunID"])
	// Resource attributes are included.
	assert.Equal(t, "azcopy", attrs["ServiceName"])
	// No job dimensions on a command.invoked event.
	_, hasFromTo := attrs["FromTo"]
	assert.False(t, hasFromTo)

	// Empty RunID is omitted.
	e.RunID = ""
	_, hasRunID := e.attributes()["RunID"]
	assert.False(t, hasRunID)
}

func TestAttributesIncludeResourceAndDimensions(t *testing.T) {
	attrs := sampleFinished().attributes()
	assert.Equal(t, "azcopy", attrs["ServiceName"])
	assert.Equal(t, "copy", attrs["Command"])
	assert.Equal(t, "true", attrs["OptRecursive"])
	assert.Equal(t, "8", attrs["OptBlockSizeMB"])
	assert.Equal(t, "--recursive,--put-md5", attrs["OptFlagsSet"])
	// JobStatus is only present on the finished event.
	assert.Equal(t, "Completed", attrs["JobStatus"])
	_, hasStatus := sampleStarted().attributes()["JobStatus"]
	assert.False(t, hasStatus)
}

func TestRunIDCorrelatesEvents(t *testing.T) {
	// The started and finished events for a single run share the same RunID,
	// emitted as the "RunID" property so the two can be joined.
	started := sampleStarted().attributes()
	finished := sampleFinished().attributes()
	assert.Equal(t, "job-1234", started["RunID"])
	assert.Equal(t, "job-1234", finished["RunID"])
	assert.Equal(t, started["RunID"], finished["RunID"])
}

func TestOptFlagsSetTruncation(t *testing.T) {
	// A run with many flags must not produce an OptFlagsSet value larger than
	// the cap, so the telemetry payload stays bounded.
	flags := make([]string, 0, 500)
	for i := 0; i < 500; i++ {
		flags = append(flags, "--some-long-flag-name")
	}
	attrs := JobDimensions{OptFlagsSet: flags}.props()
	val := attrs["OptFlagsSet"]
	assert.LessOrEqual(t, len(val), maxPropValueLen)
	assert.True(t, strings.HasSuffix(val, "...(truncated)"))

	// A short flag set is left untouched.
	short := JobDimensions{OptFlagsSet: []string{"--recursive", "--put-md5"}}.props()
	assert.Equal(t, "--recursive,--put-md5", short["OptFlagsSet"])
	assert.NotContains(t, short["OptFlagsSet"], "truncated")
}

func TestMergeProps(t *testing.T) {
	out := mergeProps(
		map[string]string{"a": "1", "b": "1"},
		map[string]string{"b": "2", "c": "3"},
	)
	assert.Equal(t, map[string]string{"a": "1", "b": "2", "c": "3"}, out)
}

func TestEventToEnvelope(t *testing.T) {
	e := eventToEnvelope("ikey-1", sampleFinished())
	assert.Equal(t, "Microsoft.ApplicationInsights.Metric", e.Name)
	assert.Equal(t, "ikey-1", e.IKey)
	assert.Equal(t, "MetricData", e.Data.BaseType)
	// All 14 measurements live in a single envelope, sharing one property bag.
	require.Len(t, e.Data.BaseData.Metrics, 14)
	assert.Equal(t, "copy", e.Data.BaseData.Properties["Command"])
	assert.Equal(t, "azcopy.job.finished", e.Data.BaseData.Metrics[0].Name)
}

func TestReportEventAppInsights(t *testing.T) {
	client := &stubClient{status: http.StatusOK}
	r := NewReporter(Config{
		Backend:          BackendAppInsights,
		ConnectionString: testConnString,
		HTTPClient:       client,
	})

	err := r.ReportEvent(context.Background(), sampleFinished())
	require.NoError(t, err)
	require.Equal(t, 1, client.calls)

	// Validate the request targets the track endpoint with JSON content.
	assert.Equal(t, http.MethodPost, client.lastReq.Method)
	assert.Equal(t, "https://eastus.example.com/v2.1/track", client.lastReq.URL.String())
	assert.Equal(t, "application/json", client.lastReq.Header.Get("Content-Type"))

	var envs []appInsightsEnvelope
	require.NoError(t, json.Unmarshal(client.lastBody, &envs))
	// A single envelope carries all 14 measurements (no per-metric duplication).
	require.Len(t, envs, 1)
	assert.Len(t, envs[0].Data.BaseData.Metrics, 14)
}

func TestReportEventAppInsightsServerError(t *testing.T) {
	client := &stubClient{status: http.StatusInternalServerError, respBody: "boom"}
	r := NewReporter(Config{
		Backend:          BackendAppInsights,
		ConnectionString: testConnString,
		HTTPClient:       client,
	})
	err := r.ReportEvent(context.Background(), sampleStarted())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestReportEventTransportError(t *testing.T) {
	client := &stubClient{err: errors.New("network down")}
	r := NewReporter(Config{
		Backend:          BackendAppInsights,
		ConnectionString: testConnString,
		HTTPClient:       client,
	})
	err := r.ReportEvent(context.Background(), sampleStarted())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network down")
}

func TestReportEventOTel(t *testing.T) {
	client := &stubClient{status: http.StatusOK}
	r := NewReporter(Config{
		Backend:          BackendOTel,
		ConnectionString: testConnString,
		HTTPClient:       client,
	})

	err := r.ReportEvent(context.Background(), sampleFinished())
	require.NoError(t, err)
	require.Equal(t, 1, client.calls)
	assert.Equal(t, "https://eastus.example.com/v2.1/track", client.lastReq.URL.String())

	var envs []appInsightsEnvelope
	require.NoError(t, json.Unmarshal(client.lastBody, &envs))
	// All OTel counters share one attribute set, so they collapse to a single
	// envelope carrying every measurement.
	require.Len(t, envs, 1)
	assert.Len(t, envs[0].Data.BaseData.Metrics, 14)
	assert.Equal(t, "copy", envs[0].Data.BaseData.Properties["Command"])
}

func TestReportEvents_StopsOnFirstError(t *testing.T) {
	client := &stubClient{status: http.StatusInternalServerError}
	r := NewReporter(Config{
		Backend:          BackendAppInsights,
		ConnectionString: testConnString,
		HTTPClient:       client,
	})
	err := r.ReportEvents(context.Background(), sampleStarted(), sampleFinished())
	require.Error(t, err)
	assert.Equal(t, 1, client.calls) // stopped after the first failed send
}

func TestReportEventUnknownBackend(t *testing.T) {
	r := NewReporter(Config{Backend: "nope", ConnectionString: testConnString})
	err := r.ReportEvent(context.Background(), sampleStarted())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown telemetry backend")
}

func TestReportEventMissingConnectionString(t *testing.T) {
	r := NewReporter(Config{Backend: BackendAppInsights, ConnectionString: ""})
	err := r.ReportEvent(context.Background(), sampleStarted())
	require.Error(t, err)
}
