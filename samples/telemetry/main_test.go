package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aztelemetry "github.com/Azure/azure-storage-azcopy/v10/telemetry"
)

func TestBuildSampleEvents(t *testing.T) {
	events := buildSampleEvents(time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC))
	require.Len(t, events, 21)

	counts := map[string]map[string]int{
		"copy": {},
		"sync": {},
	}
	commandEvents := 0
	for _, event := range events {
		switch typed := event.(type) {
		case aztelemetry.CommandInvokedEvent:
			commandEvents++
			assert.Equal(t, "jobs.list", typed.Command)
			assert.NotEmpty(t, typed.RunID)
			assert.NotEmpty(t, typed.InvocationID)
		case aztelemetry.JobStartedEvent:
			counts[typed.Dimensions.Command]["started"]++
			assert.Equal(t, "telemetry-sample-v1", typed.Resource.SamplerVersion)
			assert.Equal(t, "2", typed.Resource.SchemaVersion)
			assert.Equal(t, 1.0, typed.Resource.SamplingRate)
			assert.Empty(t, typed.Dimensions.SourceCloudType)
			assert.Equal(t, "public", typed.Dimensions.DestCloudType)
			assert.NotEmpty(t, typed.RunID)
			assert.NotEmpty(t, typed.InvocationID)
		case aztelemetry.JobFinishedEvent:
			counts[typed.Dimensions.Command]["finished"]++
			assert.Positive(t, typed.BytesEnumerated)
			assert.Positive(t, typed.ObjectsScheduled)
			assert.Positive(t, typed.StorageHTTPAttemptCount)
			assert.Positive(t, typed.JobDurationSeconds)
			assert.NotEmpty(t, typed.TerminalReason)
			assert.GreaterOrEqual(t, typed.BytesOverWire, typed.BytesTransferred)
			assert.Equal(t, typed.ObjectsScheduled, typed.RegularFilesScheduled+typed.SymlinksScheduled+typed.HardlinksConvertedScheduled)
			assert.Equal(t, typed.ObjectsScheduled, typed.ObjectsCompleted+typed.ObjectsFailed+typed.ObjectsSkipped)
			assert.Equal(t, typed.FolderPropertiesScheduled, typed.FolderPropertiesCompleted+typed.FolderPropertiesFailed+typed.FolderPropertiesSkipped)
			assert.Equal(t, typed.TransfersTotal, typed.TransfersCompleted+typed.TransfersFailed+typed.TransfersSkipped)
			assert.InDelta(t, 100*float64(typed.NetworkErrorAttemptCount)/float64(typed.StorageHTTPAttemptCount), typed.NetworkErrorPct, 0.0001)
		default:
			t.Fatalf("unexpected event type %T", event)
		}
	}

	assert.Equal(t, 1, commandEvents)
	assert.Equal(t, jobsPerCommand, counts["copy"]["started"])
	assert.Equal(t, jobsPerCommand, counts["copy"]["finished"])
	assert.Equal(t, jobsPerCommand, counts["sync"]["started"])
	assert.Equal(t, jobsPerCommand, counts["sync"]["finished"])
}

func TestBuildSampleEventsVariesByRun(t *testing.T) {
	first := buildSampleEvents(time.Date(2026, 7, 21, 12, 0, 0, 1, time.UTC))
	second := buildSampleEvents(time.Date(2026, 7, 21, 12, 0, 0, 2, time.UTC))

	firstFinished := first[2].(aztelemetry.JobFinishedEvent)
	secondFinished := second[2].(aztelemetry.JobFinishedEvent)
	assert.NotEqual(t, firstFinished.BytesEnumerated, secondFinished.BytesEnumerated)
	assert.NotEqual(t, firstFinished.JobDurationSeconds, secondFinished.JobDurationSeconds)
	assert.NotEqual(t, firstFinished.AvgIOPS, secondFinished.AvgIOPS)
}

func TestPairedEventsShareCorrelationIDs(t *testing.T) {
	events := buildSampleEvents(time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC))

	started := make(map[string]string)
	for _, event := range events {
		switch typed := event.(type) {
		case aztelemetry.JobStartedEvent:
			started[typed.RunID] = typed.InvocationID
		case aztelemetry.JobFinishedEvent:
			require.Contains(t, started, typed.RunID)
			assert.Equal(t, started[typed.RunID], typed.InvocationID)
			assert.Equal(t, typed.StartTimestamp, typed.EndTimestamp.Add(-time.Duration(typed.JobDurationSeconds)*time.Second))
		}
	}
}

func TestWritePayloadSamples(t *testing.T) {
	events := buildSampleEvents(time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC))
	outputPath := filepath.Join(t.TempDir(), "payloads.md")
	require.NoError(t, writePayloadSamples(outputPath, events))

	contents, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	markdown := string(contents)
	for _, section := range []string{
		"## Command Invoked",
		"## Copy Job Started",
		"## Copy Job Finished",
		"## Sync Job Started",
		"## Sync Job Finished",
	} {
		assert.Contains(t, markdown, section)
	}
	assert.Equal(t, 5, strings.Count(markdown, "```json"))
	// Command/start events each have one measurement; finish events each have 50.
	assert.Equal(t, 103, strings.Count(markdown, "\"name\": \"Microsoft.ApplicationInsights.Metric\""))
	assert.Equal(t, 2, strings.Count(markdown, "\"name\": \"azcopy.job.finished\""))
	assert.Contains(t, markdown, "\"name\": \"azcopy.bytes_transferred\"")
	assert.NotContains(t, markdown, "IngestionEndpoint")
	assert.NotContains(t, markdown, "LiveEndpoint")
	assert.NotContains(t, markdown, "ApplicationId")
}
