# Sustained CLI telemetry comparison: 2026-09-09

Completed against real Azure Blob Storage and the existing isolated Application
Insights endpoint. All 16 transfers passed exit/status/count/byte/SHA-256 checks.
All eight enabled runs, including warm-ups, sent both lifecycle events without
latching telemetry off. Temporary container and container-scoped role removal
were independently verified after the test.

## Configuration

- Windows amd64, Go 1.26.5, 32 logical CPUs; GOMAXPROCS 8 and transfer concurrency 16.
- One 2 GiB blob; 4,096 blobs of 256 KiB each (1 GiB total).
- Three measured on/off pairs per workload, alternating order; one warm-up per mode
  per workload excluded from comparisons. Total downloaded: 24 GiB.
- Same release binary for every process; SHA-256
  `2a695b1561cbb0cf3957748c9adcde5e668a3bc4013e6bea62ee5673cc4fd2a1`.
  This also matches the September 8 unprofiled run's binary.
- Source revision: `d813019869c281edb99bf07e00fde68fd8471829` with local harness-only
  changes for long workloads and average memory. No production code changes.
- 128 Mbps cap and 4 MiB blocks for both modes. This is a sustained-run comparison,
  not a maximum-throughput benchmark, and differs from the prior uncapped workload.
- Profiling disabled, no pprof files; open idle stdin in both modes.
- Test duration: 2,799.61 seconds, including fixture setup, validation, and cleanup.

## Results

Off/on values below are medians across three processes. Each process's average
memory is its time-weighted average, not its peak. Paired deltas are the median of
the three within-pair differences, so they need not equal the difference between
the off/on medians.

| Workload | Metric | Telemetry off | Telemetry on | Paired delta | Descriptive 95% interval |
| --- | --- | ---: | ---: | ---: | ---: |
| 2 GiB single blob | Wall seconds | 139.766 | 142.523 | +2.778 | +2.533 to +3.052 |
| 2 GiB single blob | CPU seconds | 7.078 | 7.641 | +0.562 | -0.234 to +0.781 |
| 2 GiB single blob | Average working set MiB | 340.088 | 427.610 | +89.081 | +51.285 to +121.369 |
| 2 GiB single blob | Average private commit MiB | 371.940 | 386.028 | +48.089 | -24.188 to +54.250 |
| 2 GiB single blob | Peak working set MiB | 502.672 | 554.254 | -0.621 | -26.703 to +104.480 |
| 2 GiB single blob | Peak commit MiB | 461.965 | 498.570 | -0.598 | -87.328 to +43.992 |
| 4,096 files / 1 GiB | Wall seconds | 77.672 | 81.446 | +3.624 | +1.971 to +3.838 |
| 4,096 files / 1 GiB | CPU seconds | 21.172 | 21.672 | +0.047 | -0.938 to +0.594 |
| 4,096 files / 1 GiB | Average working set MiB | 186.527 | 189.718 | +3.191 | -3.663 to +7.016 |
| 4,096 files / 1 GiB | Average private commit MiB | 143.561 | 139.055 | -4.506 | -10.122 to +0.424 |
| 4,096 files / 1 GiB | Peak working set MiB | 221.582 | 218.969 | -2.695 | -6.875 to +5.324 |
| 4,096 files / 1 GiB | Peak commit MiB | 166.340 | 163.715 | -3.312 | -7.059 to +5.500 |

Paired wall-time overhead was 1.99% for the single blob and 4.66% for many files.
With only three pairs, bootstrap intervals are descriptive and are not evidence
of a universal overhead or an acceptance-gate pass. Large-blob average working set
increased in all three pairs; private commit and CPU changes had mixed signs.
This unprofiled run does not identify the cause of the memory difference and does
not attribute it to the Go heap, FIPS, or a specific allocator.

## Memory interpretation

Working set is resident process memory including shared/image pages. Private commit
is private committed virtual memory, not necessarily resident memory and not Go
heap size. Peaks use Windows high-water counters. Averages use a trapezoidal
integral of current WorkingSetSize and PrivateUsage sampled at nominal 10 ms
intervals, divided by observed seconds. Zero post-exit samples are excluded and
unobserved endpoints are not extrapolated. Measured-run coverage was
99.5896% to 99.8557% of wall time.

The scope includes startup, authentication waits, enumeration, transfer, and exit
flush. It is not a transfer-only steady-state average. Memory and CPU cover only
AzCopy, excluding its Azure CLI authentication subprocesses. The parent fixture
generator and post-transfer hash verification are outside process measurements.

## Reproduce and evidence

```powershell
./testSuite/telemetry-live.ps1 -Run -Scenario cli-performance `
  -PerformanceWorkload long -PerformancePairs 3 -Profile:$false `
  -SubscriptionId '31347be8-d066-464e-9866-7e58d85027b7' `
  -ResourceGroup 'azcopy-telemetry-test-rg' -Suffix 'sep08' `
  -StorageAccountName 'ankursstorage' -GrantStoragePermission
```

The explicit permission switch creates and removes a role only on the unique test
container, as in the prior run. No authentication, sampling, or quota settings were
changed.

- [Metadata](metadata.json)
- [Raw trials, including warm-ups](trials.jsonl)
- [Comparisons and healthy-only subset](comparison.json)

Full binary and per-process logs remain under
`%TEMP%/azcopy-telemetry-long-average-sep09`, with run data in
`comparison-8bd35bcb-fb2d-486e-8f95-15f2a084f855`.

Cleanup verification: container `telemetry-cli-663668b9-a5b0-4241-8d54-4077a6efaee8`
returned `exists: false`; its scoped role-assignment list was empty.