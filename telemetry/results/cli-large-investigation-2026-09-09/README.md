# Large-file memory investigation: Windows and WSL 2

Only the 2 GiB single-blob workload was run for this investigation. All completed
experiments used three alternating on/off pairs plus one warm-up per mode, a
128 Mbps cap, four-MiB blocks, GOMAXPROCS 8, and transfer concurrency 16. Each
completed experiment transferred 16 GiB. The same binary was used for both modes
on each OS. Production source, telemetry deadlines, and authentication settings
were not changed.

## Conclusion

The Windows resident-memory increase has two distinguishable components:

1. Approximately 58 MiB of additional resident executable-image pages, plus
   approximately 4.2 MiB of mapped pages, consistently accompanied telemetry.
   The executable itself accounts for essentially all the image difference.
2. Variable private-memory usage is dominated by AzCopy's download buffer pool,
   not a comparably large retained telemetry event allocation. Restricting the
   in-flight buffer budget brought private commit and retained buffer allocations
   much closer while the image-residency difference remained.

The unprofiled native Linux run in WSL 2 did not reproduce the increase. Enabled
RSS was lower in all three measured Linux pairs. This is not evidence that
telemetry saves memory universally; the sample is small and transfer buffering
varies. The Windows paging trigger remains unresolved: these measurements locate
the resident pages, not the component that touches or pages them in. They do not
prove FIPS, antivirus scanning, prefetching, or TLS is responsible.

## Experiments

Values are per-mode medians; averages are time-weighted within each process,
including startup, transfer, and exit flush. Paired deltas are medians of the
within-pair differences and need not equal the difference of mode medians.

| Experiment | Average resident off/on MiB | Average private off/on MiB | Peak resident off/on MiB | Wall off/on seconds |
| --- | ---: | ---: | ---: | ---: |
| Prior Windows unprofiled, default buffer | 340.09 / 427.61 | commit 371.94 / 386.03 | 502.67 / 554.25 | 139.77 / 142.52 |
| Windows profiled, default buffer | 317.74 / 438.35 | commit 346.51 / 397.16 | 435.30 / 549.96 | 139.52 / 141.96 |
| Windows profiled, 128 MiB buffer control | 169.74 / 240.17 | commit 192.70 / 189.13 | 235.46 / 287.10 | 140.22 / 142.51 |
| WSL Linux unprofiled, default buffer | RSS 356.81 / 340.12 | anonymous RSS 326.84 / 310.16 | RSS 444.75 / 429.00 | 139.86 / 140.95 |

The prior unprofiled row is from the
[earlier long-workload run](../cli-long-2026-09-09/README.md), not a new run in this
investigation. Windows profiled measurements include CPU/exit-heap profiling and
250 ms resident-page diagnostics and must not replace unprofiled overhead results.
There was no buffer-limited Linux experiment.

Paired average resident changes:

- Windows profiled/default: +120.61 MiB; individual pairs +165.71, +26.34, +120.61.
- Windows profiled/buffer control: +72.97 MiB; pairs +72.97, +78.14, +57.92.
- Linux unprofiled/default: -20.53 MiB; pairs -20.53, -16.69, -37.91.

The buffer-control average private-commit paired median was -0.73 MiB, with a
descriptive bootstrap interval of -15.31 to +5.28 MiB. Linux average anonymous RSS
paired median was -21.02 MiB. Windows and Linux private-memory columns describe
different quantities and must not be subtracted from each other.

## Windows residency attribution

Time-weighted contributions from all resident diagnostic samples, in MiB.

| Buffer setting | Pair | Private pages off/on | Image pages off/on | Mapped pages off/on | Executable pages off/on |
| --- | ---: | ---: | ---: | ---: | ---: |
| Default | 1 | 268.25 / 371.73 | 26.01 / 84.05 | 0.45 / 4.67 | 15.88 / 73.91 |
| Default | 2 | 333.85 / 297.85 | 26.03 / 84.07 | 0.45 / 4.68 | 15.89 / 73.93 |
| Default | 3 | 291.09 / 349.58 | 25.94 / 84.03 | 0.45 / 4.67 | 15.81 / 73.88 |
| 128 MiB | 1 | 139.48 / 150.55 | 26.87 / 84.57 | 0.44 / 4.66 | 16.04 / 73.71 |
| 128 MiB | 2 | 142.50 / 158.80 | 26.70 / 84.34 | 0.45 / 4.66 | 15.85 / 73.56 |
| 128 MiB | 3 | 154.85 / 150.82 | 26.90 / 84.61 | 0.45 / 4.67 | 16.04 / 73.69 |

For example, controlled pair 1's approximately 73 MiB total difference is
11.07 MiB private + 57.70 MiB image + 4.22 MiB mapped. The measured image/private
distinction is based on VirtualQueryEx and QueryWorkingSet, not inferred from
the difference between commit and working set. Mapped-file owners were not
captured. Memory averages covered over 99.6% of measured default Windows runs and
over 99.84% of controlled Windows runs. Endpoints are not extrapolated.

## Heap and buffer evidence

`go tool pprof -top -sample_index=inuse_space` on all six measured profiles per
Windows experiment showed `common.(*multiSizeSlicePool).RentSlice` dominating
the retained heap:

| Pair | Default pool off/on MiB | Controlled pool off/on MiB |
| --- | ---: | ---: |
| 1 | 272.09 / 360.12 | 96.03 / 88.03 |
| 2 | 300.10 / 284.10 | 92.03 / 92.03 |
| 3 | 284.10 / 344.12 | 96.03 / 92.03 |

Default retained heap totals were 292.42/381.03, 321.48/305.45, and
304.84/362.51 MiB off/on. Transfer buffers accounted for approximately 93-95%.
Controlled heap totals were 119.48/105.34, 110.42/112.83, and 117.90/113.42 MiB.
These are sampled post-GC exit snapshots, not continuous heap measurements.

The source explains why buffering can amplify timing differences:

- `common/chunkedFileWriter.go` rents a buffer for each downloaded chunk and
  returns it after writing.
- `common/multiSizeSlicePool.go` allocates when its size-specific channel is empty,
  keeps up to 100 returned medium/large slices per size class, and prunes slowly.
  At a four-MiB chunk size that class can retain 400 MiB of idle buffers.
- `jobsAdmin/JobsAdmin.go:getMaxRamForChunks` chose 16 GiB on this 32-CPU machine,
  confirmed in job logs. Its comment explicitly excludes cached unused pool slices
  from the in-flight limiter's accounting.

The buffer-control intervention and profiles support buffer occupancy as the
private-memory explanation, but do not prove precisely which scheduling, GC, or
I/O timing difference caused each pool to reach its observed size. No claim of a
telemetry memory leak, a total-memory cap, or a universal optimal buffer setting
is made. CPU cost rose between the default and buffer-limited experiments, so
the buffer override is a diagnostic control, not a recommended production change.

## Linux verification

Ubuntu WSL 2 kernel `6.6.87.2-microsoft-standard-WSL2`, x86_64, 32 logical CPUs.
The native Linux binary/test runner were cross-compiled with Go 1.26.5 and
CGO_ENABLED=0. Downloads and temporary fixtures used Linux ext4 under
`/home/sharankur/azcopy-perf-sep09`, not Windows-mounted storage or tmpfs.

Linux measurements read VmRSS, VmHWM and RssAnon at nominal 10 ms intervals.
Only native AzCopy CPU/RSS are measured; Azure CLI authentication is bridged to
the existing Windows login and excluded from process CPU/memory, but included in
elapsed time. No Go or Python runtime was installed in WSL for the test. Linux
file cache outside process RSS is not counted. WSL results are not bare-metal
Linux results and profiled Windows timings are not directly comparable to Linux
unprofiled timings. Initial Linux raw console lines printed zero commit fields;
the JSON comparisons correctly report anonymous RSS, not commit. The launcher
logging has since been corrected.

## Validation, provenance, and cleanup

All three completed experiments passed eight full transfers each, including
SHA-256, exact bytes/file count, completed status, and telemetry health. Every
enabled run sent exactly one start and finish; no enabled-stopped samples occurred.
An initial Windows experiment aborted before transfer on an Azure CLI MSAL cache
error. Its three successful trials are retained separately and excluded from
completed-run comparisons. No individual trial was silently retried.

Windows production binary SHA-256:
`2a695b1561cbb0cf3957748c9adcde5e668a3bc4013e6bea62ee5673cc4fd2a1`.
Linux production binary SHA-256:
`e653e2b89cc04ea05a5357fbb48c28ebf42604c2e21d655e45485e8be481b8df`.
Source revision `d813019869c281edb99bf07e00fde68fd8471829` with uncommitted
harness-only changes. The Windows binary matches the prior unprofiled experiment.

Evidence directories below contain byte-identical metadata, comparisons, and
trials; Windows directories also retain measured heap profiles and pprof summaries:

- [Windows default](windows-default/metadata.json)
- [Windows buffer control](windows-buffer128/metadata.json)
- [Linux unprofiled](linux-unprofiled/metadata.json)
- [Initial failed experiment](windows-auth-abort/metadata.json)

Full binaries, stdout/stderr, CPU profiles, and job logs remain under the Windows
temporary roots `azcopy-large-profiled-sep09`, `azcopy-large-profiled-sep09-retry`,
and `azcopy-large-buffer128-profiled-sep09`, and the WSL root above. Independent
checks confirmed all four temporary containers absent and all four scoped role
assignment lists empty:

- `telemetry-cli-7e88f3fe-dc28-4e06-877c-c2bdb4703895` (aborted)
- `telemetry-cli-12ae3025-e682-4007-a4c7-7927c1cedf5b` (Windows default)
- `telemetry-cli-c24e8f2b-189a-4a42-b03f-0efe39762fd8` (Linux)
- `telemetry-cli-86cb2814-2c66-4c01-8abd-82769132b5df` (Windows buffer control)

No production code edits, cloud auth/sampling/quota changes, commits, or pushes
were performed. A system-level page-fault trace would be the next investigation
for the still-unidentified Windows executable-page-in trigger.