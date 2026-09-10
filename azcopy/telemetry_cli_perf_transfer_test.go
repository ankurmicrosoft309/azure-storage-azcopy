//go:build telemetrylive && telemetryperf && (windows || linux)

package azcopy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-storage-azcopy/v10/common"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type telemetryCLIPerfTrial struct {
	telemetryCLIProcessMetrics
	Workload            string
	Mode                string
	Pair                int
	Order               int
	Warmup              bool
	JobID               string
	Bytes               uint64
	Files               int
	TelemetryState      string
	TelemetryStopReason string
}

type telemetryCLIPerfComparison struct {
	Metric              string
	Unit                string
	DisabledMedian      float64
	EnabledMedian       float64
	PairedDeltaMedian   float64
	DeltaLower95        float64
	DeltaUpper95        float64
	PairedPercentMedian float64
	PercentLower95      float64
	PercentUpper95      float64
}

func telemetryCLIPerfCompare(metric, unit string, disabled, enabled []float64) telemetryCLIPerfComparison {
	median := func(values []float64) float64 {
		ordered := append([]float64(nil), values...)
		sort.Float64s(ordered)
		return (ordered[(len(ordered)-1)/2] + ordered[len(ordered)/2]) / 2
	}
	deltas, percentages := make([]float64, len(disabled)), make([]float64, len(disabled))
	for index := range disabled {
		deltas[index] = enabled[index] - disabled[index]
		percentages[index] = 100 * (enabled[index]/disabled[index] - 1)
	}
	delta := telemetryPerfInterval(metric, deltas, 0)
	percent := telemetryPerfInterval(metric, percentages, 0)
	return telemetryCLIPerfComparison{
		Metric: metric, Unit: unit, DisabledMedian: median(disabled), EnabledMedian: median(enabled),
		PairedDeltaMedian: delta.Median, DeltaLower95: delta.Lower95, DeltaUpper95: delta.Upper95,
		PairedPercentMedian: percent.Median, PercentLower95: percent.Lower95, PercentUpper95: percent.Upper95,
	}
}

func TestTelemetryCLIPerformanceAnalysis(t *testing.T) {
	result := telemetryCLIPerfCompare("time", "s", []float64{1, 2, 3, 4}, []float64{1.1, 2.2, 3.3, 4.4})
	require.Equal(t, 2.5, result.DisabledMedian)
	require.InDelta(t, 2.75, result.EnabledMedian, 0.000001)
	require.InDelta(t, 0.25, result.PairedDeltaMedian, 0.000001)
	require.InDelta(t, 10, result.PairedPercentMedian, 0.000001)
	require.InDelta(t, 10, result.PercentLower95, 0.000001)
	require.InDelta(t, 10, result.PercentUpper95, 0.000001)
}

type telemetryCLIWorkload struct {
	Name         string
	Files        int
	BytesPerFile int64
}

func telemetryCLIWorkloads(profile string) ([]telemetryCLIWorkload, int, error) {
	switch profile {
	case "", "standard":
		return []telemetryCLIWorkload{{"large-blob", 1, 64 * 1024 * 1024}, {"small-blobs", 128, 16 * 1024}}, 0, nil
	case "long":
		return []telemetryCLIWorkload{{"large-blob", 1, 2 * 1024 * 1024 * 1024}, {"small-blobs", 4096, 256 * 1024}}, 128, nil
	case "large-only":
		return []telemetryCLIWorkload{{"large-blob", 1, 2 * 1024 * 1024 * 1024}}, 128, nil
	case "64m-only":
		return []telemetryCLIWorkload{{"large-blob", 1, 64 * 1024 * 1024}}, 0, nil
	default:
		return nil, 0, fmt.Errorf("unknown CLI performance workload profile: %q", profile)
	}
}

func TestTelemetryCLIWorkloads(t *testing.T) {
	standard, capMbps, err := telemetryCLIWorkloads("standard")
	require.NoError(t, err)
	require.Zero(t, capMbps)
	require.EqualValues(t, 64*1024*1024, standard[0].BytesPerFile)
	long, capMbps, err := telemetryCLIWorkloads("long")
	require.NoError(t, err)
	require.Equal(t, 128, capMbps)
	require.EqualValues(t, 2*1024*1024*1024, int64(long[0].Files)*long[0].BytesPerFile)
	require.EqualValues(t, 1024*1024*1024, int64(long[1].Files)*long[1].BytesPerFile)
	require.Equal(t, 4096, long[1].Files)
	large, capMbps, err := telemetryCLIWorkloads("large-only")
	require.NoError(t, err)
	require.Equal(t, 128, capMbps)
	require.Equal(t, long[:1], large)
	smallSingle, capMbps, err := telemetryCLIWorkloads("64m-only")
	require.NoError(t, err)
	require.Zero(t, capMbps)
	require.Equal(t, standard[:1], smallSingle)
	_, _, err = telemetryCLIWorkloads("invalid")
	require.Error(t, err)
}

func TestTelemetryCLIPerformance(t *testing.T) {
	if os.Getenv("AZCOPY_RUN_CLI_TELEMETRY_PERF") != "1" || os.Getenv("AZCOPY_RUN_LIVE_TELEMETRY") != "1" {
		t.Skip("manual full CLI performance comparison; use telemetry-live.ps1 -Scenario cli-performance")
	}
	pairs, err := strconv.Atoi(os.Getenv("AZCOPY_CLI_TELEMETRY_PERF_PAIRS"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, pairs, 3)
	require.LessOrEqual(t, pairs, 15)
	workloadProfile := os.Getenv("AZCOPY_CLI_TELEMETRY_PERF_WORKLOAD")
	workloads, capMbps, err := telemetryCLIWorkloads(workloadProfile)
	require.NoError(t, err)
	bufferOverride := os.Getenv("AZCOPY_CLI_TELEMETRY_PERF_BUFFER_GB")
	if bufferOverride != "" {
		bufferGB, err := strconv.ParseFloat(bufferOverride, 64)
		require.NoError(t, err)
		require.True(t, bufferGB >= 0.0625 && bufferGB <= 16, "buffer override must be 0.0625..16 GiB")
	}
	experimentTimeout, processTimeout := 35*time.Minute, 3*time.Minute
	if workloadProfile == "long" || workloadProfile == "large-only" {
		experimentTimeout = time.Duration(15+12*pairs) * time.Minute
		processTimeout = 6 * time.Minute
	}
	profiling := os.Getenv("AZCOPY_CLI_TELEMETRY_PROFILE") == "1"
	authMode := os.Getenv("AZCOPY_LIVE_TELEMETRY_AUTH_MODE")
	if authMode == "" {
		authMode = "AZCLI"
	}
	require.Contains(t, []string{"AZCLI", "MSI"}, authMode)
	executable, err := filepath.Abs(os.Getenv("AZCOPY_LIVE_TELEMETRY_EXECUTABLE"))
	require.NoError(t, err)
	info, err := os.Stat(executable)
	require.NoError(t, err)
	require.False(t, info.IsDir())
	account := os.Getenv("AZCOPY_LIVE_TELEMETRY_STORAGE_ACCOUNT")
	require.Regexp(t, `^[a-z0-9]{3,24}$`, account)
	output := os.Getenv("AZCOPY_LIVE_TELEMETRY_OUTPUT")
	require.NotEmpty(t, output)
	output = filepath.Join(output, "comparison-"+uuid.NewString())
	require.NoError(t, os.MkdirAll(output, 0700))
	t.Logf("performance evidence: %s", output)
	binary, err := os.ReadFile(executable)
	require.NoError(t, err)
	binaryHash := sha256.Sum256(binary)
	binary = nil
	memoryScope := "Windows peak working set/commit and time-weighted average working set/private commit over valid live samples at nominal 10ms intervals; includes startup and exit flush; not Go heap"
	if runtime.GOOS == "linux" {
		memoryScope = "Linux /proc RSS high-water and sampled anonymous RSS peak; time-weighted average RSS/anonymous RSS at nominal 10ms intervals; optional smaps_rollup; includes startup and exit flush; not Windows commit or Go heap"
	}
	metadata := map[string]any{
		"StartedUTC": time.Now().UTC(), "OS": runtime.GOOS, "Architecture": runtime.GOARCH,
		"GoVersion": runtime.Version(), "LogicalCPUs": runtime.NumCPU(), "GOMAXPROCS": 8, "TransferConcurrency": 16,
		"BinarySHA256": hex.EncodeToString(binaryHash[:]), "PairsPerWorkload": pairs, "Order": "alternating AB/BA",
		"WallTimeScope": "CLI launch through process exit, including authentication, enumeration, transfers and telemetry flush",
		"CPUScope":      "AzCopy process user+kernel time; excludes Azure CLI authentication subprocesses",
		"MemoryScope":   memoryScope,
		"Identity":      "precreated installation ID for both modes; fresh isolated user directory per process",
		"Warmups":       "one discarded enabled and disabled transfer per workload", "BandwidthCapMbps": capMbps,
		"Workloads": workloads, "WorkloadProfile": workloadProfile,
		"BufferGBOverride":         bufferOverride,
		"AuthenticationMode":       authMode,
		"ExperimentTimeoutSeconds": experimentTimeout.Seconds(), "ProcessTimeoutSeconds": processTimeout.Seconds(),
		"TelemetryFailures": "retain and label configured-enabled failures; report healthy-pair subset separately without retrying samples",
		"Profiling":         profiling,
		"Stdin":             "open idle pipe; avoids the existing CLI EOF retry loop triggered by null stdin",
	}
	writeJSON := func(name string, value any) {
		encoded, err := json.MarshalIndent(value, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(output, name), encoded, 0600))
	}
	writeJSON("metadata.json", metadata)
	ctx, cancel := context.WithTimeout(context.Background(), experimentTimeout)
	defer cancel()
	target := loadLiveTelemetryTarget(t, "shutdown")
	storage, container := newLiveCLIContainer(t, ctx, target, account)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	azureConfig := os.Getenv("AZURE_CONFIG_DIR")
	if azureConfig == "" {
		azureConfig = filepath.Join(home, ".azure")
	}
	azureConfig, err = filepath.Abs(azureConfig)
	require.NoError(t, err)
	trialsFile, err := os.Create(filepath.Join(output, "trials.jsonl"))
	require.NoError(t, err)
	defer trialsFile.Close()
	encoder := json.NewEncoder(trialsFile)
	comparisons := map[string][]telemetryCLIPerfComparison{}
	for _, workload := range workloads {
		hashes := map[string][32]byte{}
		seed, err := os.CreateTemp(t.TempDir(), "payload-*.bin")
		require.NoError(t, err)
		t.Cleanup(func() { _ = seed.Close() })
		hash := sha256.New()
		_, err = io.CopyN(io.MultiWriter(seed, hash), rand.Reader, workload.BytesPerFile)
		require.NoError(t, err)
		var expectedHash [32]byte
		copy(expectedHash[:], hash.Sum(nil))
		t.Logf("seeding %s: %d files, %d bytes each", workload.Name, workload.Files, workload.BytesPerFile)
		for index := 0; index < workload.Files; index++ {
			name := fmt.Sprintf("file-%04d.bin", index)
			_, err := storage.UploadFile(ctx, container, workload.Name+"/"+name, seed, nil)
			require.NoError(t, err)
			hashes[name] = expectedHash
			if (index+1)%512 == 0 {
				t.Logf("seeded %s: %d/%d files", workload.Name, index+1, workload.Files)
			}
		}
		require.NoError(t, seed.Close())
		require.NoError(t, os.Remove(seed.Name()))
		runtime.GC()
		run := func(mode string, pair, order int, warmup bool) telemetryCLIPerfTrial {
			trialDir := filepath.Join(output, fmt.Sprintf("%s-%02d-%s", workload.Name, pair, mode))
			destination, logs, plans, userDir := filepath.Join(trialDir, "download"), filepath.Join(trialDir, "logs"), filepath.Join(trialDir, "plans"), filepath.Join(trialDir, "home")
			for _, directory := range []string{destination, logs, plans, filepath.Join(userDir, ".azcopy")} {
				require.NoError(t, os.MkdirAll(directory, 0700))
			}
			require.NoError(t, os.WriteFile(filepath.Join(userDir, ".azcopy", "installation_id"), []byte("1234567890abcdef1234567890abcdef"), 0600))
			stdout, err := os.Create(filepath.Join(trialDir, "stdout.jsonl"))
			require.NoError(t, err)
			stderr, err := os.Create(filepath.Join(trialDir, "stderr.txt"))
			require.NoError(t, err)
			processCtx, processCancel := context.WithTimeout(ctx, processTimeout)
			defer processCancel()
			command := exec.CommandContext(processCtx, executable, "copy",
				"https://"+account+".blob.core.windows.net/"+container+"/"+workload.Name+"/*", destination,
				"--from-to=BlobLocal", "--recursive=true", "--output-type=json", "--log-level=INFO", "--check-version=false", "--block-size-mb=4")
			if capMbps > 0 {
				command.Args = append(command.Args, "--cap-mbps="+strconv.Itoa(capMbps))
			}
			command.Env = liveCLIEnvironment(os.Environ(), map[string]string{
				"AZCOPY_DISABLE_TELEMETRY": strconv.FormatBool(mode == "disabled"), "AZCOPY_TELEMETRY_CONNECTION_STRING": target.connection,
				"AZCOPY_AUTO_LOGIN_TYPE": authMode, "AZURE_CONFIG_DIR": azureConfig,
				"AZCOPY_CONCURRENCY_VALUE": "16", "GOMAXPROCS": "8",
				"AZCOPY_BUFFER_GB":    bufferOverride,
				"AZCOPY_LOG_LOCATION": logs, "AZCOPY_JOB_PLAN_LOCATION": plans,
				"AZCOPY_E2E_TELEMETRY_RUN_ID":              "cli-performance/" + uuid.NewString(),
				common.EEnvironmentVariable.UserDir().Name: userDir,
			})
			if profiling {
				command.Env = append(command.Env, "AZCOPY_PROFILE_CPU="+filepath.Join(trialDir, "cpu.pprof"))
				command.Args = append(command.Args, "--memory-profile="+filepath.Join(trialDir, "heap.pprof"))
			}
			command.Stdout, command.Stderr = stdout, stderr
			stdin, err := command.StdinPipe()
			require.NoError(t, err)
			metrics, runErr := measureTelemetryCLIProcess(command)
			_ = stdin.Close()
			require.NoError(t, stdout.Close())
			require.NoError(t, stderr.Close())
			require.NoError(t, runErr, "invalid performance sample; inspect %s", trialDir)
			outputBytes, err := os.ReadFile(stdout.Name())
			require.NoError(t, err)
			if profiling {
				var jsonLines []string
				for _, line := range strings.Split(string(outputBytes), "\n") {
					if !strings.HasPrefix(line, "INFO: pprof start CPU profiling") {
						jsonLines = append(jsonLines, line)
					}
				}
				outputBytes = []byte(strings.Join(jsonLines, "\n"))
				for _, name := range []string{"cpu.pprof", "heap.pprof"} {
					info, err := os.Stat(filepath.Join(trialDir, name))
					require.NoError(t, err)
					require.Positive(t, info.Size(), "missing profile: %s", name)
				}
			}
			summary, err := liveCLISummary(outputBytes)
			require.NoError(t, err)
			require.Equal(t, common.EJobStatus.Completed(), summary.JobStatus)
			require.Zero(t, summary.TransfersFailed)
			require.Zero(t, summary.TransfersSkipped)
			require.EqualValues(t, workload.Files, summary.TransfersCompleted)
			require.EqualValues(t, int64(workload.Files)*workload.BytesPerFile, summary.TotalBytesTransferred)
			jobLog, err := os.ReadFile(filepath.Join(logs, summary.JobID.String()+".log"))
			require.NoError(t, err)
			stderrBytes, err := os.ReadFile(stderr.Name())
			require.NoError(t, err)
			startedSent := strings.Count(string(stderrBytes), "telemetry: sent packed azcopy.job.started event")
			finishedSent := strings.Count(string(stderrBytes), "telemetry: sent packed azcopy.job.finished event")
			state, stopReason := mode, ""
			const stopPrefix = "telemetry: disabled for this process after delivery failure sending "
			for _, line := range strings.Split(string(jobLog), "\n") {
				if index := strings.Index(line, stopPrefix); index >= 0 {
					stopReason = strings.TrimSpace(line[index:])
				}
			}
			if mode == "disabled" {
				require.Zero(t, startedSent+finishedSent, "disabled sample emitted telemetry")
				require.Empty(t, stopReason, "disabled sample initialized telemetry")
			} else if stopReason != "" {
				state = "enabled-stopped"
				t.Logf("%s telemetry stopped: %s", trialDir, stopReason)
			} else {
				require.Equal(t, 1, startedSent, "enabled sample lacks verified telemetry start: %s", trialDir)
				require.Equal(t, 1, finishedSent, "enabled sample lacks verified telemetry finish: %s", trialDir)
				state = "enabled-healthy"
			}
			for name, expected := range hashes {
				file, err := os.Open(filepath.Join(destination, name))
				require.NoError(t, err)
				hash := sha256.New()
				_, err = io.Copy(hash, file)
				require.NoError(t, err)
				require.NoError(t, file.Close())
				require.Equal(t, expected[:], hash.Sum(nil), "download content mismatch")
			}
			require.NoError(t, os.RemoveAll(destination))
			trial := telemetryCLIPerfTrial{telemetryCLIProcessMetrics: metrics, Workload: workload.Name, Mode: mode, Pair: pair, Order: order, Warmup: warmup, JobID: summary.JobID.String(), Bytes: summary.TotalBytesTransferred, Files: workload.Files, TelemetryState: state, TelemetryStopReason: stopReason}
			require.Positive(t, metrics.CPUSeconds)
			require.NoError(t, encoder.Encode(trial))
			require.NoError(t, trialsFile.Sync())
			secondaryName, secondaryAverage, secondaryPeak := "commit", metrics.AverageCommitBytes, metrics.PeakCommitBytes
			if runtime.GOOS == "linux" {
				secondaryName, secondaryAverage, secondaryPeak = "anonymousRSS", metrics.AverageAnonymousRSSBytes, metrics.PeakAnonymousRSSBytes
			}
			t.Logf("%s pair=%d mode=%s warmup=%t wall=%.3fs cpu=%.3fs avgResident=%.2fMiB avg%s=%.2fMiB peakResident=%.2fMiB peak%s=%.2fMiB observed=%.3fs samples=%d state=%s", workload.Name, pair, mode, warmup, metrics.WallSeconds, metrics.CPUSeconds, metrics.AverageWorkingSetBytes/(1024*1024), secondaryName, secondaryAverage/(1024*1024), float64(metrics.PeakWorkingSetBytes)/(1024*1024), secondaryName, float64(secondaryPeak)/(1024*1024), metrics.MemoryObservedSeconds, metrics.MemorySamples, state)
			return trial
		}
		run("disabled", 0, 0, true)
		run("enabled", 0, 1, true)
		disabled, enabled := []telemetryCLIPerfTrial{}, []telemetryCLIPerfTrial{}
		for pair := 1; pair <= pairs; pair++ {
			modes := []string{"disabled", "enabled"}
			if pair%2 == 0 {
				modes[0], modes[1] = modes[1], modes[0]
			}
			for order, mode := range modes {
				trial := run(mode, pair, order, false)
				if mode == "disabled" {
					disabled = append(disabled, trial)
				} else {
					enabled = append(enabled, trial)
				}
			}
		}
		for _, metric := range []struct {
			name string
			unit string
			get  func(telemetryCLIPerfTrial) float64
		}{
			{"wall", "seconds", func(trial telemetryCLIPerfTrial) float64 { return trial.WallSeconds }},
			{"cpu", "seconds", func(trial telemetryCLIPerfTrial) float64 { return trial.CPUSeconds }},
			{"average-working-set", "MiB", func(trial telemetryCLIPerfTrial) float64 { return trial.AverageWorkingSetBytes / (1024 * 1024) }},
			{"average-commit", "MiB", func(trial telemetryCLIPerfTrial) float64 { return trial.AverageCommitBytes / (1024 * 1024) }},
			{"peak-working-set", "MiB", func(trial telemetryCLIPerfTrial) float64 { return float64(trial.PeakWorkingSetBytes) / (1024 * 1024) }},
			{"peak-commit", "MiB", func(trial telemetryCLIPerfTrial) float64 { return float64(trial.PeakCommitBytes) / (1024 * 1024) }},
			{"average-anonymous-rss", "MiB", func(trial telemetryCLIPerfTrial) float64 { return trial.AverageAnonymousRSSBytes / (1024 * 1024) }},
			{"peak-anonymous-rss", "MiB", func(trial telemetryCLIPerfTrial) float64 { return float64(trial.PeakAnonymousRSSBytes) / (1024 * 1024) }},
		} {
			if strings.Contains(metric.name, "commit") && runtime.GOOS != "windows" || strings.Contains(metric.name, "anonymous-rss") && runtime.GOOS != "linux" {
				continue
			}
			if runtime.GOOS == "linux" {
				metric.name = strings.ReplaceAll(metric.name, "working-set", "rss")
			}
			disabledValues, enabledValues := make([]float64, pairs), make([]float64, pairs)
			for index := range disabled {
				disabledValues[index], enabledValues[index] = metric.get(disabled[index]), metric.get(enabled[index])
			}
			healthyDisabled, healthyEnabled := []float64{}, []float64{}
			for index := range enabled {
				if enabled[index].TelemetryState == "enabled-healthy" {
					healthyDisabled, healthyEnabled = append(healthyDisabled, disabledValues[index]), append(healthyEnabled, enabledValues[index])
				}
			}
			if len(healthyEnabled) >= 3 {
				comparisons[workload.Name+"/healthy-only"] = append(comparisons[workload.Name+"/healthy-only"], telemetryCLIPerfCompare(metric.name, metric.unit, healthyDisabled, healthyEnabled))
			}
			if metric.name == "wall" {
				t.Logf("%s measured enabled health: %d/%d healthy; %d stopped on delivery failure", workload.Name, len(healthyEnabled), pairs, pairs-len(healthyEnabled))
			}
			comparison := telemetryCLIPerfCompare(metric.name, metric.unit, disabledValues, enabledValues)
			comparisons[workload.Name] = append(comparisons[workload.Name], comparison)
			t.Logf("%s %s: disabled median=%.3f enabled median=%.3f paired delta=%.3f %s (95%% %.3f..%.3f), paired overhead=%.2f%% (95%% %.2f..%.2f)", workload.Name, metric.name, comparison.DisabledMedian, comparison.EnabledMedian, comparison.PairedDeltaMedian, metric.unit, comparison.DeltaLower95, comparison.DeltaUpper95, comparison.PairedPercentMedian, comparison.PercentLower95, comparison.PercentUpper95)
		}
		writeJSON("comparison.json", comparisons)
	}
}
