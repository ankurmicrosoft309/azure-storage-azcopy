# Local 64 MiB telemetry-stage investigation

## Finding

The real IMDS detection request is the reproducible trigger for the large local
Windows resident-memory increase in this experiment. Skipping just that request
removed the increase while leaving telemetry collection, serialization, and both
Application Insights lifecycle sends enabled. Performing the request and then
forcing its result to false still reproduced the increase. Returning true without
a request did not. The result was confirmed without CPU, heap, or resident-page
profiling enabled.

This identifies a triggering operation, not the underlying Windows paging actor.
We did not establish whether a network/security component, OS behavior, or another
request-path effect pages in the executable. It is not evidence that the IMDS
response allocates approximately 78 MiB of Go heap. No security component was
disabled and no VM was created or used for this investigation.

## Method And Controls

- Local Windows amd64, Go 1.26.5; real 64 MiB (67,108,864 bytes) Blob-to-local
  download. No bandwidth cap; four-MiB blocks, concurrency 16, GOMAXPROCS 8,
  default buffer budget, precreated installation ID, and open idle stdin.
- A single tracked temporary container was used for all stage-removal runs.
  Its container-scoped Storage Blob Data Contributor assignment was recorded and
  removed with the container after the investigation. No account-wide grants or
  authentication-setting changes were made.
- One warm-up round and three measured rounds per variant, with reversed/rotated
  ordering. Each process had fresh user, log, plan, and output directories.
- Every valid trial checked exit code, Completed status, exact file count/bytes,
  SHA-256, and the expected send/suppression behavior. Normal sending variants
  required one started and one finished event. Suppression variants were explicitly
  labeled; they were not treated as healthy production telemetry delivery.
- Four valid experiments: 40 + 32 + 24 + 16 = 112 successful transfers (7 GiB),
  including warm-ups; 84 measured trials. Minimum memory coverage was 99.71% in
  profiled experiments and 99.83% in unprofiled experiments.
- Average working set and private commit are time-weighted process-lifetime
  measurements including startup and flush, not transfer-only steady-state values.
  Tables report medians of three process measurements, not pooled averages.
- Production source was unchanged. Diagnostic changes used Go build overlays
  pointing at temporary source copies. All diagnostic variants within an experiment
  used the same binary; runtime stage switches avoided per-variant linker-layout
  differences. Original on/off controls verified reproduction outside the overlay.

## Broad Stage Removal: Profiled

| Variant | Average working set MiB | Peak working set MiB | Peak private commit MiB |
| --- | ---: | ---: | ---: |
| Original binary, telemetry off | 54.54 | 121.38 | 143.29 |
| Original binary, telemetry on | 95.72 | 199.64 | 143.43 |
| Diagnostic full pipeline | 95.62 | 199.50 | 143.16 |
| Skip all resource collection | 57.23 | 122.44 | 143.50 |
| Skip hardware probes | 97.67 | 199.88 | 143.57 |
| Skip IMDS request only | 56.76 | 122.18 | 143.47 |
| Skip installation ID | 95.77 | 199.77 | 143.46 |
| Suppress event sends | 91.52 | 198.71 | 143.18 |
| Serialize events, omit telemetry HTTP | 92.82 | 198.70 | 142.95 |

The hardware, identity, and Application Insights send paths are not necessary for
the large increase. Skipping IMDS alone was sufficient to remove it in all three
measured rounds. The separate diagnostic-off control matched original-off.

## IMDS Controls: Profiled

The production probe sends a proxy-free HTTP GET to
`http://169.254.169.254/metadata/instance/compute/location?api-version=2021-02-01&format=text`
with `Metadata: true`. Recorded responses were HTTP 200, HTTP/1.1, content length
12 bytes. The sampled request duration was about 15 ms, not a one-second timeout.
Only status/protocol/length were logged, not response content. This establishes
what the local endpoint returned, not independent verification of its backend.

| Variant | Average working set MiB | Peak working set MiB |
| --- | ---: | ---: |
| Full pipeline | 95.72 | 200.13 |
| No IMDS | 57.82 | 122.36 |
| Replace IMDS with 20 ms delay | 57.01 | 122.31 |
| Replace IMDS with 1 second delay | 54.17 | 122.52 |
| Create/close IMDS transport, no request | 57.35 | 122.11 |
| Real request, close idle connections | 101.64 | 199.58 |
| Real request, drain body and close connections | 96.36 | 199.99 |
| Real request, disable keep-alive | 95.34 | 199.36 |

The simple hypotheses of a retained idle connection, unread response body, transport
construction, or startup delay do not explain the increase. They were tested as
controlled changes, not inferred from a single profile.

## Confirmation Without Profiling

No pprof or resident-page-classification instrumentation ran in the parent/child
for these phases. Lightweight diagnostic stage markers remain in the overlay
binary; the original production on/off controls contain none of those markers.

| Variant | Average working set MiB | Peak working set MiB | Peak private commit MiB |
| --- | ---: | ---: | ---: |
| Original off | 53.89 | 118.99 | 141.20 |
| Original on | 95.08 | 197.66 | 141.29 |
| Diagnostic full | 100.61 | 197.82 | 141.45 |
| Diagnostic no IMDS | 56.67 | 119.56 | 141.27 |
| Diagnostic 20 ms delay instead | 56.92 | 119.58 | 141.31 |
| Diagnostic no event sends | 92.12 | 198.02 | 141.67 |

The final request-versus-result experiment used a new single binary with both
controls, also unprofiled:

| Variant | Average working set MiB | Peak working set MiB | Peak private commit MiB |
| --- | ---: | ---: | ---: |
| Full | 97.45 | 197.71 | 141.33 |
| No IMDS | 57.07 | 119.87 | 141.84 |
| Real IMDS request, force AzureVMDetected=false | 100.39 | 198.17 | 141.79 |
| No request, force AzureVMDetected=true | 56.28 | 119.91 | 141.48 |

The memory effect follows execution of the request, not the telemetry boolean.
This does not isolate URL, header, destination address, or the OS/host component
processing the request from one another; those are possible next investigation
boundaries, not conclusions here.

## What Memory Increased

Direct Windows page classification in broad-matrix round 1 found:

| Variant | Total image MiB | AzCopy executable MiB | Private resident MiB |
| --- | ---: | ---: | ---: |
| Full | 86.72 | 76.25 | 107.87 |
| No IMDS | 27.11 | 16.94 | 94.73 |

These are category values at the largest classified snapshot, not differences
computed by subtracting independent OS peaks. Approximately 59 MiB of the gap is
the existing executable image becoming resident. Private residency also changes,
but peak private commit stays nearly equal. A sparse page snapshot need not match
the kernel's exact lifetime peak. Time-series snapshots are preserved in trials.

Exit heap profiles for the same round showed 83.48 MiB retained with full telemetry
and 86.87 MiB with IMDS omitted. RentSlice transfer buffers accounted for
60.02/64.02 MiB respectively. No-send and serialize-only retained 78.95/81.41 MiB,
also dominated by 60.02 MiB of transfer buffers. Those sampled post-GC profiles
do not show a large IMDS-retained heap object or prove an absence of all transient
allocations. They corroborate the residency-versus-allocation distinction.

Stage markers show the large increase occurs after the fast IMDS response, not
as an immediate allocation in the probe itself. Short command-only probes exited
without the large increase and were not substituted for the requested transfers.

## Implications

The root code path to evaluate is `probeIMDS()` in `azcopy/hostinfo.go`, called by
`buildResourceAttributes()` in `azcopy/telemetry.go`. Removing Application Insights
delivery, JSON serialization, hardware discovery, or installation identity is not
supported as the fix by these tests. Closing IMDS idle connections alone did not
fix it either.

A Windows-specific policy to skip or avoid the optional IMDS network probe is a
candidate mitigation, but would change Azure VM detection and needs a separate
design decision. The production pipeline was deliberately left intact. The earlier
Azure Windows VM did not exhibit the same image-residency behavior, so this should
not be generalized to all Windows environments or treated as a universal IMDS bug.
An OS-level network/page-fault trace is still needed to name the mechanism behind
the endpoint-triggered image residency; no FIPS, antivirus, or prefetch cause is
claimed.

## Evidence, Exclusions, And Reproduction

- [Broad matrix](matrix-profiled-ready/metadata.json),
  [raw trials](matrix-profiled-ready/trials.jsonl).
- [Corrected IMDS controls](imds-profiled-corrected/metadata.json),
  [raw trials](imds-profiled-corrected/trials.jsonl).
- [Unprofiled confirmation](confirmation-unprofiled/metadata.json),
  [raw trials](confirmation-unprofiled/trials.jsonl).
- [Request-versus-result controls](causal-unprofiled/metadata.json),
  [raw trials](causal-unprofiled/trials.jsonl).
- [Cleanup verification](cleanup.json).

Each experiment directory includes run logs and per-trial stderr stage markers.
The broad matrix also retains all measured heap profiles and readable pprof tops.
The final three overlay sources are preserved as text snapshots under `overlay/`;
they are evidence, not production files to install. A build-overlay manifest maps
the current checkout's original file paths to these snapshots. The final source
snapshot represents the final causal binary, not byte-identical sources for the
earlier two diagnostic builds.

Binary hashes:

- Original: `2a695b1561cbb0cf3957748c9adcde5e668a3bc4013e6bea62ee5673cc4fd2a1`.
- Broad diagnostic: `2f1fee638fa7bfcc999e34ae126ac4b15154b12c6786cc44f3c857dd5d96f89d`.
- IMDS-controls diagnostic: `2d1a3de8c4da9d24591a793f661cdd6d4ac67fbca861b25e6359485028720691`.
- Final causal diagnostic: `d6d02dce472429f1939a694be6ef4576e993fa33c98eb0d02cc3d46e383d6fe5`.

The historical opt-in `testSuite/telemetry-local-ablation.ps1` runner required explicit
original/diagnostic executables, output path, and an authorized temporary container.
It provisioned neither VMs nor permissions and did not delete caller-owned fixtures.
`-EnableProfiling` selects profiles; `-VariantsJson` selects runtime ablations in
the overlay only. `-PerformanceWorkload 64m-only` is also available in the normal
performance runner without any stage-removal switches.

The one-off runner, ablation test, and VM investigation tooling were removed from
the active stack after the investigation. Their original sources are retained in
the local recovery ref `backup/telemetry-before-rework-20260910`. The raw results
and overlay text snapshots remain historical evidence, not executable test hooks.

An initial baseline failed during RBAC propagation before transfers. One early
warm-up subsequently failed on a Blob GET permission check, then was retained and
excluded. A separate 32-transfer IMDS matrix had incorrect custom-variant decoding:
JSON unmarshalling reused pre-populated slice elements and inherited omitted flags.
It is explicitly excluded, saved under `excluded-imds-profiled`, and was rerun
after a fresh-slice decoding fix and regression test. The 112 valid transfers above
exclude those 32 successful but misconfigured transfers. In total the completed
matrices transferred 9 GiB including the excluded matrix. All runs were local;
existing Azure Blob Storage/Application Insights endpoints were used, not a VM.

All binaries, full logs, profiles, failed setup outputs, and readiness data remain
under `%TEMP%/azcopy-local64-ablation-sep09` and the initial baseline root
`%TEMP%/azcopy-local64-baseline-sep09`. Both temporary containers and their scoped
roles were independently verified absent at 2026-09-09T15:07:18Z.
No production telemetry source edits, commits, or pushes were made.