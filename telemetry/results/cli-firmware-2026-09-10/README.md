# Local Firmware Detection Regression: 2026-09-10

Replacing telemetry's IMDS request with local chassis asset-tag detection removed
the large working-set increase on this Windows development host. The new enabled
binary still detected this host as Azure public cloud in the native probe test,
and all eight enabled transfers successfully sent both lifecycle events.

## Implementation

The implementation matches the documented Azure public-cloud chassis asset tag:
Windows reads SMBIOS type 3 through `GetSystemFirmwareTable`; Linux reads
`/sys/class/dmi/id/chassis_asset_tag`. Firmware buffers, file reads, and malformed
record handling are bounded. There is no telemetry IMDS fallback, subprocess, or
WMI query. Managed-identity SDK authentication is unchanged.

`AzureVMDetected` remains a boolean in schema version 3. False means not detected,
including unavailable firmware, unsupported platforms, and unmatched markers. This
is not attestation and does not identify sovereign clouds or Azure Local. Raw
firmware and asset tags are not emitted. See Microsoft's
[guest identification documentation](https://learn.microsoft.com/en-us/azure/virtual-machines/identify-azure-vm-from-guest).

## Method

- Local Windows amd64, Go 1.26.5, real Blob-to-local downloads and isolated
  Application Insights ingestion. No new VM was provisioned.
- Two actual production binaries, telemetry off/on for each. No diagnostic
  overlays or stage removals; all `Skip` values are empty. The existing ablation
  runner's generic metadata label does not mean these binaries contain overlays.
- One 64 MiB random blob, uncapped, default buffer setting, 4 MiB chunks,
  concurrency 16, `GOMAXPROCS=8`, Azure CLI authentication, open idle stdin.
- One excluded warm-up per condition, followed by three measured rounds with
  rotated/reversed condition order: 16 transfers total, 1 GiB downloaded.
- Fresh user/log/plan/output directories and the same precreated installation ID.
  Fixture upload, permission propagation, and SHA-256 checks are outside timing.
- Profiling disabled. Windows process counters sampled nominally every 10 ms;
  averages use elapsed-time-weighted trapezoidal integration. Measured coverage
  was 99.837% to 99.887%. Authentication subprocess memory/CPU is excluded, but
  waiting for authentication is included in wall time.
- Every transfer exited successfully, reported Completed with one completed file,
  zero failures/skips, exactly 64 MiB, and matched SHA-256. Each enabled process
  sent exactly one started and one finished event, with no pipeline stop. Each
  disabled process sent neither. Sender success was checked; stored-event queries
  were not part of this regression.

Binary SHA-256 values:

- Old IMDS: `2a695b1561cbb0cf3957748c9adcde5e668a3bc4013e6bea62ee5673cc4fd2a1`
- New firmware: `c1cfbb6afa29bd975b0316175f3bc89ca1c8e5c32d62b10ca6ed275e9591b457`

The old binary is the preserved baseline from the preceding investigations. The
new binary was built from the current working tree; this is not a same-binary
stage-removal experiment. The telemetry-off controls have similar memory values.

## Results

Values are medians of three measured processes per condition. Memory is MiB.

| Condition | Average working set | Peak working set | Average private commit | Peak private commit | Wall seconds | CPU seconds |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Old, off | 52.836 | 118.828 | 85.326 | 141.168 | 9.019 | 0.656 |
| Old, on | 98.351 | 197.535 | 86.564 | 141.152 | 9.479 | 0.609 |
| Firmware, off | 53.150 | 118.848 | 84.884 | 141.074 | 9.069 | 0.562 |
| Firmware, on | 56.127 | 119.582 | 87.404 | 141.043 | 9.370 | 0.594 |

Within-round median on-minus-off differences:

| Binary | Average working set | Peak working set | Wall seconds |
| --- | ---: | ---: | ---: |
| Old | +44.614 MiB | +78.551 MiB | +0.434 |
| Firmware | +2.978 MiB | +0.652 MiB | +0.301 |

Paired medians need not equal differences between condition medians. The enabled
condition medians fell from 98.35 to 56.13 MiB average working set and from 197.54
to 119.58 MiB peak working set. Peak private commit remained approximately 141 MiB.
This confirms the requested local residency regression is resolved in the actual
firmware-based binary; it is not a claim of zero telemetry cost or a universal
throughput guarantee. Three rounds on one host are insufficient for those claims.
No resident-page classification was collected in this unprofiled comparison, so
it does not identify the Windows paging actor. See the preceding
[causal investigation](../cli-local64-ablation-2026-09-09/README.md).

## Validation And Cleanup

- Focused Windows detection, malformed-input, no-HTTP resource construction,
  telemetry, and installation tests passed. The native Windows probe returned true.
- SMBIOS fuzzing passed 14,851 executions in five seconds with two workers.
- Native Linux tests passed in WSL using a cross-compiled test binary; Windows
  ARM64 package build passed. No Linux transfer benchmark was repeated here.
- macOS ARM64 package cross-build remains blocked by the existing
  `azidentity/cache` dependency's undefined `accessor.New` and
  `accessor.WithAccount`; native macOS behavior was not validated.
- An initial standard benchmark failed during its five-minute scoped-RBAC setup
  window, before any transfer. Its failure record is retained separately. The
  comparison used a new tracked fixture and the existing ten-minute setup window;
  no failed transfer was replaced and permissions were not broadened.
- Both temporary containers and their container-scoped roles were independently
  confirmed absent at 2026-09-10 05:19:15 UTC. No account authentication, sampling,
  quota, or security settings were changed.

## Evidence

- [metadata.json](metadata.json), [trials.jsonl](trials.jsonl), and [run.log](run.log)
  are byte-identical copies of the successful run.
- Per-process sender diagnostics are under `stderr/`; the initial setup-only
  failure is [setup-failure.jsonl](setup-failure.jsonl).
- [fixture.json](fixture.json) records the temporary scope, and
  [cleanup.json](cleanup.json) records independent absence checks for both attempts.
- Full local artifacts remain under
  `%TEMP%/azcopy-firmware64-confirm-sep10/comparison`. The new binary and initial
  attempt remain under `%TEMP%/azcopy-firmware64-sep10`.