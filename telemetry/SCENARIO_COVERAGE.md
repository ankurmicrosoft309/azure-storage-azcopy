# Telemetry Scenario Coverage: 2026-09-10

This maps the functional and non-functional matrix in
[AzCopy Telemetry Testing Scenarios 2026-09-01.docx](https://microsoftapc-my.sharepoint.com/:w:/g/personal/sharankur_microsoft_com/IQAYinQBNfoAQIJDJ3Nuq8S8ASZTq2hYSFKwTVG413QRYhU?e=lMHdbw)
to the current tests. The Word document is unchanged. Its status cells and earlier
performance numbers are historical, not acceptance results for this checkout.

## New Local CLI E2E

Run from the repository root with Go and PowerShell 7:

```powershell
./testSuite/telemetry-cli-e2e.ps1
```

The runner builds the actual checkout once and launches independent AzCopy CLI
processes. Storage uses bounded range-capable download and two-blob upload/list/delete fixtures, and
ingestion is a loopback HTTP receiver. The real CLI, transfer engine, serializer,
dispatcher, and exit path run; neither remote service is real Azure. No cloud
credentials, resource creation, quota changes, or persistent telemetry settings
are required. Go dependencies must already be available or restorable.

The `telemetrylive` tag and separate `AZCOPY_RUN_TELEMETRY_CLI_E2E=1` opt-in keep
these subprocess/timing tests out of ordinary unit runs. This does not enable the
Azure fault scenarios' separate `AZCOPY_RUN_LIVE_TELEMETRY` opt-in. The parent
telemetry is disabled; each child gets an explicit loopback override, isolated
log/plan paths, and a temporary home. Concurrent workers share only that home to
exercise installation-ID creation. Stdin is an open pipe in every child; only
the cancellation test writes the normal `cancel` command through it.

Cancellation waits until a download GET is blocked, requests graceful shutdown
through `--cancel-from-stdin`, validates Cancelled telemetry and a persisted plan,
then resumes in a fresh CLI process. The completed download is SHA-256 verified.
This tests the portable stdin cancellation path, not OS-specific Ctrl-C delivery.
The controlled cancellation occurs before any payload completes, so nonzero
partial-progress resumes and multiple resume cycles remain untested here.

Benchmark download writes to the null device. Benchmark upload writes two 1 KiB
blobs in one generated folder and runs real CLI cleanup; the fixture verifies
both deletions and the absence of leftover blobs. Only the benchmark lifecycle
pair may be emitted, not a cleanup pair. Benchmark endpoint inference uses a
synthetic `.blob.invalid` URL routed through a child-only HTTP proxy to loopback;
there are no host DNS/proxy changes or Azure requests.

Successful copy/sync downloads and the resumed download must exit zero, report
Completed with exact object/byte counts and no failures/skips, and match SHA-256.
Benchmarks verify summary bytes and fixture reads/writes/deletions, not a local
download hash. Received lifecycle events must
match the authoritative CLI summary, schema 1, command, endpoint/auth categories,
job/invocation/installation IDs, and source-shape values. Ordering is checked
within each job, never by the global interleaving of independent processes.

All captured requests are real serialized envelopes, gated at 4 KiB for starts
and 8 KiB for finishes. Those are representative fixture limits, not a general
maximum or a process-memory guarantee. Privacy canaries are checked in the raw
ingestion bodies; ingestion-error messages must not leak into telemetry logs.

Negative event checks run until the child exits and drains its bounded telemetry
work. They are not real-service ingestion-delay/absence checks. A stalled finish
uses the current four-second flush limit plus one second of scheduler allowance;
the document's two-second exit limit is superseded. Other process bounds include
startup and transfer overhead and are not throughput benchmarks.

## Functional Matrix

Validation on 2026-09-10: the expanded runner passed on Windows in 55.84 seconds
(59.73 seconds including Go package overhead): 36 CLI invocations, 18 completed
hash-verified downloads, a gracefully cancelled attempt followed by resume,
benchmark download and upload/cleanup, four concurrent identity workers, and nine ingestion
failure/latency scenarios. The same suite has not yet been executed on Linux or
macOS. The new tests found and fixed telemetry emission from generated
`completion.bash`, `completion.zsh`, `completion.fish`, and
`completion.powershell` subcommands; the completion exclusion unit test covers
all four as well.

| # | Document scenario | Current coverage and remaining limits |
| --- | --- | --- |
| 1 | Successful copy | New `copy reconciliation`: real CLI BlobLocal transfer, correlated pair, exact summary/shape fields and hash. Existing cloud E2E manifest verifies delivered pairs across its transfer matrix. |
| 2 | Successful sync | New `sync reconciliation`: single-blob sync and exact terminal counters/hash. Multi-file unchanged/deletion reconciliation remains dependent on existing cloud scenarios; not newly exercised here. |
| 3 | Resume correlation | New cancellation-and-resume case uses the original plan and JobID in a fresh process, a new InvocationID, the same InstallationID, jobs.resume and job-cumulative scope, exact final bytes and SHA-256. Attempt-throughput measurements are absent. Nonzero partial-progress and multiple-resume variants remain partial. |
| 4 | Benchmark telemetry | New real CLI download and upload/cleanup cases verify mode, file count/size, folder count and cleanup fields. Upload creates and deletes two blobs, with no cleanup lifecycle or command events. Real Azure benchmark performance is not measured. |
| 5 | Command-only | New `eligible command`: actual jobs list emits one command.invoked, no lifecycle pair. |
| 6 | Excluded commands/dry-run | New help, help flag, env, doc, four shell-completion generators, copy dry-run, sync dry-run: zero events; dry-runs create no downloaded file. Found and fixed completion child commands previously emitting telemetry. |
| 7 | Opt-out/configuration | New CLI/env opt-out for jobs list, copy CLI opt-out with successful transfer/hash, missing key and missing endpoint with normal command completion. Blank override intentionally selects the embedded default, so it is not treated as disabled configuration. |
| 8 | Terminal outcomes | Success and graceful cancellation covered by CLI tests; initialization/failed/finalization categories have existing unit/integration tests. Full controlled process failure-stage matrix remains partial. |
| 9 | Cancellation | New blocked-transfer case uses --cancel-from-stdin, checks Cancelled, terminal stage, percent complete below 100, zero completed payload, persisted plan and raw-wire path privacy. Native Ctrl-C/SIGTERM behavior is not exercised. No process kill is represented as graceful cancellation. |
| 10 | Topologies | Existing cloud E2E suite and dimension tests; new local fixture covers BlobLocal only. No new NFS/S3/GCS/BlobFS environment was provisioned. |
| 11 | Auth/cloud | New anonymous and SAS-shaped input with not-applicable local auth; raw SAS/path/request-ID privacy checks. Fixture does not validate Azure SAS authorization. Real OAuth/shared-key/sovereign-cloud matrix remains partial. |
| 12 | Metric reconciliation | New exact bytes, completed/scheduled/skipped/failed counters, folder counts (zero in fixture), HTTP attempts, scan count/size/depth against CLI summary/fixture. Nonzero folder/retry/hardlink cases remain existing unit/cloud coverage, not new local E2E. |

## Non-Functional Matrix

| # | Document scenario | Current coverage and remaining limits |
| --- | --- | --- |
| 1 | Concurrent identity | Four simultaneous real copy processes, independent job/invocation IDs and correlated pairs, one persisted installation ID, four verified downloads. Explicit blocked-one-job/other-job ordering isolation remains unit-tested rather than newly process-tested. |
| 2 | Ingestion/query resilience | Existing verifier polling/transient/permanent-error tests. New slow receiver covers client delivery/order, not actual ingestion/query latency. |
| 3 | Dashboards/enrichment | Existing offline schema/description tests and read-only synthetic aggregate replay. No live dashboard import, browser validation, or enrichment-service test in this pass. |
| 4 | Non-interference | New actual transfers with rejected/partial/malformed-partial/throttled/unavailable/stalled/slow ingestion. Transfers and hashes must succeed, failures suppress finish/retries, final stall is bounded by the current exit-flush budget. |
| 5 | Performance comparison | Existing opt-in paired CLI/instrumentation benchmarks and recorded results. Not rerun or promoted to a passing 1%/100 KiB release gate; that needs a controlled host and agreed memory definition. |
| 6 | Payload/binary size | New actual CLI start/finish envelope limits plus existing both-backend serialized fixture gates. Binary-growth gate exists in the separate manual pipeline; no new release-pipeline wiring. |
| 7 | Unreachable/reject | New closed loopback port and HTTP 400/206/429/503 cases. Each transfer succeeds; HTTP rejection produces one start attempt, no retry/finish, and one sanitized stop diagnostic. DNS/TLS failures remain covered by transport-level tests. |
| 8 | Alerts/governance | Deferred: requires agreed alert thresholds, action-group recipients, permissions, deployment and delivery verification. Quota exhaustion is not deterministic immediate rejection; prior real tests recorded this limitation. |
| 9 | Bogus data | Existing manifest/schema validation and aggregate replay cover negative cases. Full controlled ingestion plus live Grafana/ADX exclusion remains deferred, not replaced by the loopback receiver. |

## Failure-Mode Notes

The new process-level rejection and stall cases supplement failure modes 2, 3, 4,
and 6. Existing unit tests cover recoverable panic/serialization behavior,
identity recovery, bounds and correlation. No OOM injection, security-agent
disablement, machine-wide DNS/proxy change, or destructive quota experiment is
attempted. Telemetry host detection now uses local firmware/sysfs, so the
document's historical IMDS probe-failure scenario no longer applies to telemetry
classification. Managed-identity SDK use of IMDS is separate and unchanged.