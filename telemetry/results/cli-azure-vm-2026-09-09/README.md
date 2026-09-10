# Azure Windows VM telemetry comparison: 2026-09-09

## Outcome

Both unprofiled and profiled experiments passed on a newly provisioned Windows
Server VM in East US 2 EUAP. All 16 transfers completed with expected file count,
bytes, status, and SHA-256. All eight telemetry-enabled runs, including warm-ups,
sent exactly one started and one finished event without latching telemetry off.

The large resident-memory increase seen on the development Windows machine did
not reproduce here. The unprofiled median paired increase in average working set
was 3.66 MiB (individual pairs 8.38, 3.40, and 3.66 MiB). With profiling it was
0.20 MiB, with mixed-sign pairs. The executable remained around 16 MiB resident
in both modes, rather than rising to roughly 74 MiB when telemetry was enabled
as it did in the earlier local diagnostic runs.

All temporary cloud resources and the two external Reader role assignments were
deleted. Subscription-level group, resource, and role inventories independently
confirmed cleanup at 2026-09-09T14:11:49Z. A direct group-existence query returned
Forbidden after deletion; that error was not used as evidence of absence.

## Setup

- Subscription: XDatamove-Dev-Playground1,
  `31347be8-d066-464e-9866-7e58d85027b7`.
- Region: `eastus2euap`; VM `azcopy-perf`, `Standard_B2als_v2` Spot, two vCPUs,
  four GiB RAM, AMD EPYC 7763, Windows Server 2022 Datacenter Core build 20348.
- Image: `MicrosoftWindowsServer:WindowsServer:2022-datacenter-core-smalldisk-g2:20348.5622.260906`.
- One 32 GiB Standard LRS HDD OS disk, no zone/redundant VM, no public IP,
  deny-all inbound NSG. System-assigned managed identity; no personal login
  credentials or storage keys copied to the guest.
- Fresh Standard LRS Blob test storage in the same EUAP region. Existing isolated
  `azcopy-telfault-sep08-shutdown` Application Insights target retained unchanged.
- Defender real-time protection was enabled. Subscription-installed Geneva,
  antimalware, and Azure Policy extensions were not disabled. This was not a
  security-agent-free host. Recorded power plan: High performance.
- Lowest-priced suitable checked candidate used Spot with a USD 0.05/hour bid
  ceiling. EUAP retail pricing was unavailable; this is not an invoice or a cap
  on disk, storage, and other ancillary charges. A shutdown backstop was configured
  and the group was deleted after evidence retrieval.

## Method

Each phase ran only one 2 GiB blob at 128 Mbps, three alternating on/off pairs,
and one warm-up per mode excluded from comparisons. Total downloaded: 32 GiB.
Four-MiB block size, transfer concurrency 16, GOMAXPROCS 8, and a fixed
`AZCOPY_BUFFER_GB=0.125` in both modes. The fixed 128 MiB budget controls in-flight
buffering, not total process memory or cached idle slices. The VM's default
buffer-budget behavior was not tested.

The production CLI is byte-identical to the previous local Windows experiments:
SHA-256 `2a695b1561cbb0cf3957748c9adcde5e668a3bc4013e6bea62ee5673cc4fd2a1`.
Go 1.26.5; release flags `-trimpath -buildvcs=false -ldflags='-s -w'`.
Harness-only changes enable managed identity for ARM validation, fixture upload,
and the CLI transfer. The VM ran the prebuilt executables through Azure Run
Command; no compiler or Azure CLI was installed in the guest.

Average memory is time-weighted across current-counter samples at nominal 10 ms
intervals. Scope is process startup through exit, including enumeration, transfer,
and telemetry flush, not just steady state. Unobserved endpoints are not
extrapolated. Minimum measured-process memory coverage was 99.11% unprofiled and
99.37% profiled. Working set is resident process memory including image/shared
pages; private commit is a different quantity and is not Go heap size.

The profiled phase adds CPU and post-GC exit heap profiles plus 250 ms Windows
resident-page classification. Use the unprofiled phase for overhead comparisons.
Setup, fixture uploads, post-transfer hashing, and archive uploads are outside
the per-process elapsed measurements.

## Unprofiled Results

Off/on values are medians across three measured processes; average-memory values
are medians of individual process averages. Paired deltas are medians of within-pair
differences and need not equal the difference of mode medians.

| Metric | Telemetry off | Telemetry on | Paired delta | Descriptive 95% interval |
| --- | ---: | ---: | ---: | ---: |
| Average working set MiB | 163.499 | 171.877 | +3.662 | +3.402 to +8.379 |
| Average private commit MiB | 190.417 | 198.777 | +3.238 | +3.195 to +8.360 |
| Peak working set MiB | 197.945 | 211.332 | +11.910 | +5.746 to +16.887 |
| Peak private commit MiB | 222.211 | 235.242 | +11.219 | +5.293 to +16.496 |
| Wall seconds | 137.339 | 137.409 | +0.084 | -2.019 to +0.123 |
| CPU seconds | 4.094 | 4.141 | +0.047 | -0.172 to +0.250 |

Paired median wall-time change: +0.061%. Three pairs are not sufficient to claim
a universal sub-1% overhead guarantee. This capped run is not a maximum-throughput
benchmark. CPU time is OS process user plus kernel time, not CPU utilization.

Unprofiled phase: 13:24:59Z to 13:43:58Z, test duration 1137.15 seconds.
Profiled phase: 13:44:32Z to 14:03:17Z, test duration 1123.38 seconds.
The phases together ran for approximately 38 minutes, excluding deployment and
artifact handling. CPU-credit metrics returned 43 one-minute data points through
14:02Z; credits ranged from 59.01 to 73.82. Available metrics show no credit
exhaustion, but are not a per-request scheduling trace or complete final-minute
coverage. VM-wide sampled one-minute average CPU ranged from 3.40% to 57.13%.

## Profiled Attribution

| Metric | Telemetry off | Telemetry on | Paired delta |
| --- | ---: | ---: | ---: |
| Average working set MiB | 168.549 | 170.395 | +0.195 |
| Average private commit MiB | 195.608 | 197.270 | -0.137 |
| Peak working set MiB | 201.609 | 205.270 | +4.457 |
| Peak private commit MiB | 225.863 | 229.652 | +4.371 |
| Wall seconds | 135.383 | 137.043 | +0.122 |
| CPU seconds | 4.391 | 4.281 | -0.109 |

Time-weighted page-category means within each profiled process, in MiB:

| Pair | Private resident off/on | Image resident off/on | Mapped off/on | Executable off/on |
| --- | ---: | ---: | ---: | ---: |
| 1 | 145.146 / 147.007 | 23.015 / 23.473 | 0.321 / 0.322 | 16.016 / 16.470 |
| 2 | 144.840 / 144.549 | 22.929 / 23.342 | 0.321 / 0.321 | 15.928 / 16.343 |
| 3 | 149.330 / 146.644 | 23.119 / 23.389 | 0.321 / 0.321 | 16.120 / 16.391 |

The approximately 58 MiB image and 4.2 MiB mapped-page differences seen on the
development machine were absent. Both hosts used the identical production binary
and the same explicit buffer control, but differ in OS edition/build, hardware,
CPU count, storage, installed components, and auth mechanism. Managed identity
also changes the startup/network path compared with the local Azure CLI login.
This narrows the issue to an environment/execution-path-dependent effect; it does
not establish which difference caused it, nor exclude the behavior on other VMs.

All six profiled heap snapshots were dominated by the normal transfer slice pool:

| Pair | Total retained heap off/on MiB | RentSlice retained off/on MiB |
| --- | ---: | ---: |
| 1 | 113.36 / 109.95 | 92.03 / 88.03 |
| 2 | 111.49 / 106.48 | 92.03 / 88.03 |
| 3 | 114.46 / 113.01 | 92.03 / 92.03 |

These post-GC exit profiles do not explain every transient unprofiled allocation.
They provide no evidence for a large retained telemetry heap allocation or the
local image-page-in effect on this VM. They do not prove that Defender, FIPS,
prefetching, or any particular OS component caused the earlier local increase.

## Evidence And Cleanup

- [Unprofiled metadata](unprofiled/metadata.json), [trials](unprofiled/trials.jsonl),
  [comparisons](unprofiled/comparison.json).
- [Profiled metadata](profiled/metadata.json), [trials](profiled/trials.jsonl),
  [comparisons](profiled/comparison.json), and six retained heap profiles with
  readable pprof summaries in the same directory.
- [Guest environment](environment.json), [phase results](results.json),
  [VM configuration](vm-configuration.json), [CPU/credit metrics](cpu-metrics-final.json),
  [resource inventory](resource-inventory.json), [cleanup verification](cleanup.json).

Guest archives were downloaded and SHA-256 verified before deletion:

- Unprofiled: `6d861c51a06c20fea744b5c5053445febbf8a3b7a64f9d877995bee964f98b5d`.
- Profiled: `1449dbd1bd28a8980f879774d649b6cdcb8d2a78d350b76374a1d6b80ba96b0d`.

Full archives, executables, job logs, and the initial setup-failure archive remain
locally under `%TEMP%/azcopy-vmperf-sep09-b0890ce0`. The first launch failed before
transfers because PowerShell 5.1 split unquoted dotted Go test arguments. Corrected
flags passed guest preflight checks, and both phases were rerun in a fresh directory.
No failed transfer sample was silently replaced in either completed phase.

Deleted group: `azcopy-vmperf-sep09-b0890ce0`, including the VM, disk, NIC, VNet,
NSG, LRS storage account, artifacts, shutdown schedule, and contained roles.
The two Reader assignments on the existing isolated telemetry component/workspace
were also removed. The telemetry resources themselves were not deleted or altered.
Production source, telemetry send/flush deadlines, sampling, and authentication
settings on the existing resources were unchanged. No commit or push was made.