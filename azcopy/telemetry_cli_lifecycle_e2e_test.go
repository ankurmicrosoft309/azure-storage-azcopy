//go:build telemetrylive

package azcopy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-storage-azcopy/v10/common"
	"github.com/stretchr/testify/require"
)

func testCLIE2ECancelResume(t *testing.T, executable string) {
	receiver := newCLIE2EReceiver(t)
	payload := bytes.Repeat([]byte("cancel-resume-payload\n"), 4096)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var resumed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/account/container/payload.bin" || (request.Method != http.MethodHead && request.Method != http.MethodGet) {
			http.NotFound(writer, request)
			return
		}
		if request.Method == http.MethodGet && !resumed.Load() {
			once.Do(func() { close(started) })
			select {
			case <-request.Context().Done():
			case <-release:
			}
			return
		}
		writer.Header().Set("x-ms-blob-type", "BlockBlob")
		writer.Header().Set("ETag", `"cancel-resume-etag"`)
		http.ServeContent(writer, request, "payload.bin", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), bytes.NewReader(payload))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	home, plans, logs := t.TempDir(), t.TempDir(), t.TempDir()
	destination := filepath.Join(t.TempDir(), "download.bin")
	environment := map[string]string{
		"AZCOPY_DISABLE_TELEMETRY": "false", "AZCOPY_TELEMETRY_CONNECTION_STRING": receiver.connection(),
		"AZCOPY_LOG_LOCATION": logs, "AZCOPY_JOB_PLAN_LOCATION": plans,
		common.EEnvironmentVariable.UserDir().Name: home,
		"GOMAXPROCS": "2", "AZCOPY_CONCURRENCY_VALUE": "4",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "copy", server.URL+"/account/container/payload.bin", destination,
		"--from-to=BlobLocal", "--output-type=json", "--check-version=false", "--cancel-from-stdin")
	command.Env = liveCLIEnvironment(os.Environ(), environment)
	command.Dir = logs
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	stdin, err := command.StdinPipe()
	require.NoError(t, err)
	defer stdin.Close()
	require.NoError(t, command.Start())
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("copy ended before transfer: %v; stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	case <-ctx.Done():
		<-done
		t.Fatal("copy did not reach the controlled GET")
	}
	_, err = io.WriteString(stdin, "cancel\n")
	require.NoError(t, err)
	runErr := <-done
	require.NoError(t, ctx.Err(), "graceful cancellation exceeded process bound")
	if runErr != nil {
		var exit *exec.ExitError
		require.ErrorAs(t, runErr, &exit)
		require.Equal(t, 1, exit.ExitCode())
	}
	cancelled, err := liveCLISummary(stdout.Bytes())
	require.NoError(t, err, "stdout=%s stderr=%s", stdout.String(), stderr.String())
	require.Equal(t, common.EJobStatus.Cancelled(), cancelled.JobStatus)
	require.Zero(t, cancelled.TransfersCompleted)
	require.Zero(t, cancelled.TotalBytesTransferred)
	events := receiver.events(t)
	require.Len(t, events, 2)
	start, finish := events[0].Data.BaseData, events[1].Data.BaseData
	require.Equal(t, "azcopy.job.started", start.Name)
	require.Equal(t, "azcopy.job.finished", finish.Name)
	require.Equal(t, "Cancelled", finish.Properties["JobStatus"])
	require.NotEmpty(t, finish.Properties["TerminalStage"])
	percent, present := finish.Measurements["azcopy.percent_complete"]
	require.True(t, present)
	require.GreaterOrEqual(t, percent, float64(0))
	require.Less(t, percent, float64(100))
	require.EqualValues(t, cancelled.TotalBytesTransferred, finish.Measurements["azcopy.bytes_transferred"])
	require.Equal(t, "1", start.Properties["SchemaVersion"])
	require.Equal(t, "1", finish.Properties["SchemaVersion"])
	require.Regexp(t, `^[a-f0-9]{32}$`, start.Properties["InvocationID"])
	require.Regexp(t, `^[a-f0-9]{32}$`, start.Properties["InstallationID"])
	require.Equal(t, start.Properties["InstallationID"], finish.Properties["InstallationID"])
	require.Equal(t, cancelled.JobID.String(), start.Properties["JobID"])
	require.Equal(t, start.Properties["JobID"], finish.Properties["JobID"])
	require.Equal(t, start.Properties["InvocationID"], finish.Properties["InvocationID"])
	plansFound, err := filepath.Glob(filepath.Join(plans, "*.steV*"))
	require.NoError(t, err)
	require.NotEmpty(t, plansFound)
	resumed.Store(true)
	result := runCLIE2E(t, executable, home, receiver.connection(), map[string]string{"AZCOPY_JOB_PLAN_LOCATION": plans},
		"jobs", "resume", cancelled.JobID.String(), "--output-type=json", "--check-version=false")
	completed, err := liveCLISummary(result.stdout)
	require.NoError(t, err, "%s", result.stdout)
	require.Equal(t, common.EJobStatus.Completed(), completed.JobStatus)
	require.Equal(t, cancelled.JobID, completed.JobID)
	require.EqualValues(t, 1, completed.TransfersCompleted)
	require.Zero(t, completed.TransfersFailed)
	require.EqualValues(t, len(payload), completed.TotalBytesTransferred)
	contents, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(payload), sha256.Sum256(contents))
	events = receiver.events(t)
	require.Len(t, events, 4)
	resumeStart, resumeFinish := events[2].Data.BaseData, events[3].Data.BaseData
	for _, event := range []cliE2EEnvelope{events[2], events[3]} {
		properties := event.Data.BaseData.Properties
		require.Equal(t, cancelled.JobID.String(), properties["JobID"])
		require.Equal(t, "1", properties["SchemaVersion"])
		require.Regexp(t, `^[a-f0-9]{32}$`, properties["InvocationID"])
		require.Equal(t, "jobs.resume", properties["Command"])
		require.Equal(t, "job-cumulative", properties["SummaryCounterScope"])
		require.Equal(t, start.Properties["InstallationID"], properties["InstallationID"])
		require.NotEqual(t, start.Properties["InvocationID"], properties["InvocationID"])
	}
	require.Equal(t, "azcopy.job.started", resumeStart.Name)
	require.Equal(t, "azcopy.job.finished", resumeFinish.Name)
	require.Equal(t, resumeStart.Properties["InvocationID"], resumeFinish.Properties["InvocationID"])
	require.Equal(t, "Completed", resumeFinish.Properties["JobStatus"])
	require.EqualValues(t, len(payload), resumeFinish.Measurements["azcopy.bytes_transferred"])
	require.NotContains(t, resumeFinish.Measurements, "azcopy.job_throughput_mbps")
	require.NotContains(t, resumeFinish.Measurements, "azcopy.transfer_phase_throughput_mbps")
	receiver.mu.Lock()
	wire := bytes.Join(receiver.bodies, nil)
	receiver.mu.Unlock()
	for _, private := range []string{destination, server.URL, "payload.bin", "download.bin"} {
		require.NotContains(t, string(wire), private)
	}
}

func testCLIE2EBenchmarkDownload(t *testing.T, executable string) {
	receiver := newCLIE2EReceiver(t)
	source, payload := newCLIE2EBlob(t)
	proxy := strings.TrimSuffix(source, "/account/container/payload.bin")
	result := runCLIE2E(t, executable, t.TempDir(), receiver.connection(), map[string]string{"HTTP_PROXY": proxy, "http_proxy": proxy, "NO_PROXY": "", "no_proxy": ""},
		"bench", "http://fixture.blob.invalid/account/container/payload.bin", "--mode=download", "--file-count=1", "--size-per-file=1K", "--number-of-folders=0",
		"--delete-test-data=true", "--output-type=json", "--check-version=false")
	summary, err := liveCLISummary(result.stdout)
	require.NoError(t, err, "%s", result.stdout)
	require.Equal(t, common.EJobStatus.Completed(), summary.JobStatus)
	require.EqualValues(t, len(payload), summary.TotalBytesTransferred)
	require.EqualValues(t, 1, summary.TransfersCompleted)
	require.Zero(t, summary.TransfersFailed)
	events := receiver.events(t)
	assertCLIE2ELifecycle(t, events, summary, "bench")
	for _, event := range events {
		properties := event.Data.BaseData.Properties
		require.Equal(t, "download", properties["BenchmarkMode"])
		require.Equal(t, "1", properties["BenchmarkFileCount"])
		require.Equal(t, "1024", properties["BenchmarkFileSizeBytes"])
		require.Equal(t, "0", properties["BenchmarkFolderCount"])
		require.Equal(t, "false", properties["BenchmarkCleanupRequested"])
		require.Equal(t, "false", properties["BenchmarkIsCleanup"])
	}
}

func testCLIE2EBenchmarkCleanup(t *testing.T, executable string) {
	receiver := newCLIE2EReceiver(t)
	type blobProperties struct {
		ContentLength int    `xml:"Content-Length"`
		BlobType      string `xml:"BlobType"`
		LastModified  string `xml:"Last-Modified"`
		ETag          string `xml:"Etag"`
	}
	type blobEntry struct {
		Name       string         `xml:"Name"`
		Properties blobProperties `xml:"Properties"`
	}
	type listing struct {
		XMLName xml.Name    `xml:"EnumerationResults"`
		Blobs   []blobEntry `xml:"Blobs>Blob"`
		Next    string      `xml:"NextMarker"`
	}
	var mutex sync.Mutex
	blobs := map[string][]byte{}
	uploads, deletes := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		writer.Header().Set("ETag", `"benchmark-etag"`)
		writer.Header().Set("Last-Modified", "Thu, 01 Jan 2026 00:00:00 GMT")
		writer.Header().Set("x-ms-request-id", "benchmark-fixture-request")
		if !strings.HasPrefix(request.URL.Path, "/container") {
			http.NotFound(writer, request)
			return
		}
		name := strings.TrimPrefix(request.URL.Path, "/container/")
		switch {
		case request.Method == http.MethodGet && request.URL.Query().Get("comp") == "list":
			response := listing{}
			for name, body := range blobs {
				if strings.HasPrefix(name, request.URL.Query().Get("prefix")) {
					response.Blobs = append(response.Blobs, blobEntry{name, blobProperties{len(body), "BlockBlob", "Thu, 01 Jan 2026 00:00:00 GMT", `"benchmark-etag"`}})
				}
			}
			writer.Header().Set("Content-Type", "application/xml")
			if err := xml.NewEncoder(writer).Encode(response); err != nil {
				t.Errorf("encode list fixture: %v", err)
			}
		case request.Method == http.MethodPut && strings.HasPrefix(name, "benchmark-") && request.URL.Query().Get("comp") == "":
			body, err := io.ReadAll(io.LimitReader(request.Body, 2048))
			if err != nil || len(body) != 1024 {
				t.Errorf("unexpected benchmark upload length=%d error=%v", len(body), err)
				http.Error(writer, "invalid upload", http.StatusBadRequest)
				return
			}
			blobs[name] = body
			uploads++
			writer.WriteHeader(http.StatusCreated)
		case (request.Method == http.MethodHead || request.Method == http.MethodGet) && request.URL.Query().Get("restype") == "container":
			writer.WriteHeader(http.StatusOK)
		case request.Method == http.MethodHead:
			if body, ok := blobs[name]; ok {
				writer.Header().Set("x-ms-blob-type", "BlockBlob")
				writer.Header().Set("Content-Length", fmt.Sprint(len(body)))
				writer.WriteHeader(http.StatusOK)
			} else {
				writer.Header().Set("x-ms-error-code", "BlobNotFound")
				writer.WriteHeader(http.StatusNotFound)
			}
		case request.Method == http.MethodDelete:
			if _, ok := blobs[name]; !ok {
				t.Errorf("delete of unknown fixture blob: %s", name)
			}
			delete(blobs, name)
			deletes++
			writer.WriteHeader(http.StatusAccepted)
		default:
			t.Errorf("unexpected benchmark request %s %s", request.Method, request.URL.Path)
			http.Error(writer, "unexpected fixture request", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	result := runCLIE2E(t, executable, t.TempDir(), receiver.connection(), map[string]string{"HTTP_PROXY": server.URL, "http_proxy": server.URL, "NO_PROXY": "", "no_proxy": ""},
		"bench", "http://fixture.blob.invalid/container", "--mode=upload", "--file-count=2", "--size-per-file=1K", "--number-of-folders=1",
		"--delete-test-data=true", "--output-type=json", "--check-version=false")
	mutex.Lock()
	remaining, written, removed := len(blobs), uploads, deletes
	mutex.Unlock()
	require.Equal(t, 2, written)
	require.Equal(t, 2, removed)
	require.Zero(t, remaining)
	decoder := json.NewDecoder(bytes.NewReader(result.stdout))
	var summaries []common.ListJobSummaryResponse
	for {
		var message struct{ MessageType, MessageContent string }
		err := decoder.Decode(&message)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if message.MessageType == "EndOfJob" {
			var summary common.ListJobSummaryResponse
			require.NoError(t, json.Unmarshal([]byte(message.MessageContent), &summary))
			require.Equal(t, common.EJobStatus.Completed(), summary.JobStatus)
			require.Zero(t, summary.TransfersFailed)
			summaries = append(summaries, summary)
		}
	}
	require.NotEmpty(t, summaries)
	require.EqualValues(t, 2048, summaries[0].TotalBytesTransferred)
	events := receiver.events(t)
	require.Len(t, events, 2, "cleanup must not emit lifecycle or command events")
	require.Equal(t, "azcopy.job.started", events[0].Data.BaseData.Name)
	require.Equal(t, "azcopy.job.finished", events[1].Data.BaseData.Name)
	for _, event := range events {
		properties := event.Data.BaseData.Properties
		require.Equal(t, "1", properties["SchemaVersion"])
		require.Equal(t, "bench", properties["Command"])
		require.Equal(t, "upload", properties["BenchmarkMode"])
		require.Equal(t, "2", properties["BenchmarkFileCount"])
		require.Equal(t, "1024", properties["BenchmarkFileSizeBytes"])
		require.Equal(t, "1", properties["BenchmarkFolderCount"])
		require.Equal(t, "true", properties["BenchmarkCleanupRequested"])
		require.Equal(t, "false", properties["BenchmarkIsCleanup"])
		require.Equal(t, summaries[0].JobID.String(), properties["JobID"])
	}
	require.Equal(t, events[0].Data.BaseData.Properties["InvocationID"], events[1].Data.BaseData.Properties["InvocationID"])
	require.NotEmpty(t, events[0].Data.BaseData.Properties["InvocationID"])
	require.Equal(t, events[0].Data.BaseData.Properties["InstallationID"], events[1].Data.BaseData.Properties["InstallationID"])
	require.EqualValues(t, 2048, events[1].Data.BaseData.Measurements["azcopy.bytes_transferred"])
}
