# Telemetry Scenario Coverage: 2026-09-15

This maps the functional and non-functional matrix in
[AzCopy Telemetry Testing Scenarios 2026-09-01.docx](https://microsoftapc-my.sharepoint.com/:w:/g/personal/sharankur_microsoft_com/IQAYinQBNfoAQIJDJ3Nuq8S8ASZTq2hYSFKwTVG413QRYhU?e=lMHdbw)
to the current tests. The Word document is unchanged. Its status cells and earlier
performance numbers are historical, not acceptance results for this checkout.

## Real Azure Functional E2E

Telemetry assertions primarily extend the E2E workflows that existed before
telemetry (verified against baseline `35dc27ad`). Existing copy/sync, selective
sync, failed-job resume, NFS cancellation, dry-run, jobs-list and OAuth tests use
the shared `TestNewE2E` manifest. No parallel copy/sync or cancellation fixture is
required. The existing account registry, resource creation/cleanup, `RunAzCopy`
credentials and Application Insights ingestion/query verifier are reused.
No separate account or role grant is used.

The pipeline uses the `azcopytestworkloadidentity` service connection. Dynamic
accounts come from the account registry; local static runs use the configured
`NEW_E2E_STANDARD_ACCOUNT_NAME` and the existing static credential settings.
An interactive Azure CLI user is not interchangeable with that pipeline identity.

With the normal New_E2E account/identity configuration loaded, a built executable
in `NEW_E2E_AZCOPY_PATH`, and the existing telemetry connection string, workspace
ID, and run ID configured, run:

```powershell
./testSuite/telemetry-cli-e2e.ps1
```

The runner selects relevant existing BasicFunctionality, Sync, JobsList, Dryrun,
FileOAuth, BlobFS, S2S and FilesNFS scenarios, plus the remaining TelemetryFunctional scenarios.
It uses the normal three-hour E2E timeout because the selection includes existing
platform and topology matrices; it is no longer a 31-process standalone smoke test.
It does not use the `telemetrylive` build tag, the manual-fault resource suffix,
mock HTTP endpoints, or a new authentication path. Normal New_E2E pipeline runs
discover these tests automatically. These are billable real Azure operations.

The shared manifest now reconciles bytes, scheduled/completed/failed/skipped
objects and folders, preserved symlinks, converted hardlinks, HTTP and network
attempts, server-busy counts, IOPS and percent complete for every captured summary,
not just commands carrying custom expectations. Original and resumed attempts
must retain InstallationID for the same JobID; InvocationIDs must remain unique.
All resume attempts require job-cumulative scope and omit attempt throughput.

Existing single/multi-file tests add independent source-count/size/depth checks.
The existing selective-sync test checks two scanned objects but only one scheduled
object. Existing SAS/local, FileBlob OAuth, BlobFS SAS/OAuth and mixed SAS-to-OAuth
BlobBlob tests assert auth and cloud fields: Azure public endpoints report
`public`, while local endpoints have an empty cloud value. Existing NFS cancellation tests
assert Cancelled and reject fixture paths/SAS fragments in telemetry, preserving
their original platform gates, cancellation mechanism and resume validation.
These six cancellation tests now also require empty job error category/code,
finite progress in [0, 100), and a stage in enumeration/transfer/completion.
Timer-driven cancellation does not prove which of those stages was reached;
initialization and completed stages are rejected. Explicit terminal-status
expectations require a matching CLI final summary and JobID, even for expected
failures, so an early failure cannot silently avoid the manifest assertion.
JobsListNoJobs adds opt-out/missing-configuration variants; its successful listing
and JobsListAll assert command-only events. Existing dry-runs assert no events.

Only missing flows remain in `TelemetryFunctionalSuite`: help/completion/doc/env
exclusions, successful transfers with telemetry explicitly opted out, four
processes sharing an isolated installation identity, and benchmark download plus
verified upload cleanup. The old upload benchmark is a crash-only smoke test;
it does not verify cleanup, benchmark dimensions or event suppression. The
remaining benchmark case uploads two 1 KiB blobs, checks both cleanup deletions
and an empty container, and permits only the primary benchmark telemetry pair.

The shared verifier queries real `AppEvents` by per-process run ID, checks exact
event counts and summary-derived counters, rejects duplicates and privacy
canaries, and validates shared installation identity and omitted resume rates.
Exclusion cases require absence throughout the existing five-minute observation
window with successful queries. Each query has its independent 30-second bound.
This is bounded observation, not a guarantee that no event can ever arrive later.

## Integration Tests

`./testSuite/telemetry-cli-integration.ps1` runs the retained loopback suite with
`telemetrylive` and `AZCOPY_RUN_TELEMETRY_CLI_INTEGRATION=1`. It covers deterministic
HTTP rejections, partial/malformed responses, stalls, unreachable ingestion,
payload bounds and supplementary command/transfer checks. It is integration
coverage, not Azure E2E. Mocked cancellation/resume and benchmark fixtures were
removed and replaced by the native real-service scenarios.

The four-second exit-flush limit supersedes the Word document's old two-second
limit. Loopback timings are not throughput or production performance acceptance.

## Execution Status

The consolidated suites compile. Local manifest, output-fragmentation,
benchmark-selection, existing-workflow metric/identity/rate, and concurrent-log
regressions pass. The regression deliberately changes nonzero folder/hardlink/
network counters and resume identity/rates to prove the default path rejects them.
Additional terminal regressions exercise the real finalizer's skipped/failed/
cancelled stage/error mappings and reject invalid cancellation stages, nonempty
error fields, missing summaries, private paths and missing/nonfinite/out-of-range
progress. These local checks validate the assertions, not a live service run.
The separated integration runner passed on Windows in 43.59 seconds (44.36
seconds including package overhead). Runner syntax, prerequisite rejection and
editor diagnostics passed. These local checks do not validate Azure transfers.
The updated Azure assertions have NOT completed a live run on any OS. Existing
workflow coverage is not a fresh pass of the stronger telemetry assertions.

A discarded standalone attempt using the interactive CLI user against
`ankursstorage` passed the real query preflight but failed its fixture upload with
403 AuthorizationPermissionMismatch. Its temporary container was independently
confirmed deleted. No role was granted. That separate runner was removed in
favor of the existing New_E2E identity/account path; its variables are absent in
this local shell. Execute in the configured New_E2E pipeline or configured static
test environment to establish a real-service pass.

The 2026-09-10 result (36 processes in 55.84 seconds) belongs to the superseded
loopback suite and is NOT evidence of Azure E2E completion.

## Functional Matrix

"Implemented" below means the assertion is attached to a real-service workflow,
not that a new Azure run has passed. "Missing" distinguishes absent assertions or
event fields from absent transfer fixtures. Legacy Go and Python workflows do not
automatically inherit the New_E2E manifest.

| # | Document scenario | Current coverage and remaining limits |
| --- | --- | --- |
| 1 | Successful copy | Existing SingleFile/MultiFileUploadDownload and other New_E2E copies inherit exact lifecycle and summary checks. Single/multi-file fixtures explicitly check source shape, and SingleFile checks SAS/local auth and full-path privacy. No duplicate copy fixture retained. |
| 2 | Successful sync | Existing selective-sync, deletion and idempotent-resync workflows retained. Shared counters reconcile with summaries; selective sync asserts scanned=2 versus scheduled=1. Dedicated deleted-item counters are not in the current telemetry event contract, so the Word row's deletion-metric requirement remains unresolved, not a missing deletion workflow. |
| 3 | Resume correlation | Existing failed Blob upload/resume and NFS resume cases inherit same-JobID installation continuity, distinct InvocationIDs, cumulative scope, exact summary counters and absent attempt throughput. The Blob case explicitly expects Failed then Completed. Multiple-resume-cycle assertions remain missing. |
| 4 | Benchmark telemetry | Retained new download/verified-cleanup flow because the old upload smoke test does not cover those behaviors. Dimensions, two uploaded/deleted files, empty destination and no cleanup events are asserted. Not performance acceptance. |
| 5 | Command-only | Existing JobsListNoJobs and JobsListAll now expect exactly one command.invoked and no lifecycle events. Other eligible commands can be extended similarly. |
| 6 | Excluded commands/dry-run | Existing DryrunSuite copy/sync cases explicitly expect no events. Only missing help/copy-help/env/doc/four shell-completion exclusions remain standalone. Absence uses the real query observation window. |
| 7 | Opt-out/configuration | Existing JobsListNoJobs gains CLI/env opt-out and missing-key/missing-endpoint variants when telemetry verification is configured. Successful-transfer opt-out cases remain standalone because this policy flow was missing. Blank overrides still select the embedded default. |
| 8 | Terminal outcomes | NoOverwriteSingleFile now asserts CompletedWithSkipped/completed, one skipped transfer, zero completed/failed transfers/bytes and empty job error fields. JobResume asserts Failed/completion/completion-error with one failed transfer, then Completed/completed with successful counts/bytes and cleared errors. Cancellation checks are strengthened as described below. Mixed outcomes and full failure-stage categories remain incomplete. |
| 9 | Cancellation | Six existing Linux-gated NFS copy/sync cancellation-resume cases require Cancelled, finite incomplete progress, stage enumeration/transfer/completion, empty job error fields, and path/SAS privacy. Exact cancellation phase is not pinned by their timer. Legacy Blob cancellation lacks this verifier; native signals and cross-platform telemetry remain gaps. |
| 10 | Topologies | Existing New_E2E Local/Blob/BlobFS/Files/NFS/S2S workflows inherit endpoint-type and metric checks. SAS single-file and OAuth FileBlob cases assert FromTo explicitly. Python S3/GCS transfers exist but still need telemetry-manifest integration and enabled credentials; they are not missing transfer tests. |
| 11 | Auth/cloud | Existing SAS/local, FileBlob OAuth, BlobFS SAS/OAuth upload/multiflush and mixed SAS-source/OAuth-destination BlobBlob workflows now assert auth plus public/empty cloud values. Shared-key CLI, public-anonymous, sovereign/unknown clouds remain E2E gaps. Classification unit tests exist; some historical public-access cases are skipped by policy. See effort assessment below. |
| 12 | Metric reconciliation | Shared assertions now cover nonzero folder/symlink/converted-hardlink and network counters wherever existing summaries supply them, plus source shape in controlled existing fixtures. Remaining work is controlled real-service retry/throttling injection and detailed duration/throughput, distribution and error-histogram reconciliation, not new generic folder/hardlink tests. |

## Functional 8: Detailed Matrix

The matrix has three axes: job outcome, lifecycle boundary, and error category.
It should not be expanded into an arbitrary Cartesian product: for example,
`CompletedWithErrors` is a completed job with failed individual transfers, not an
initialization failure. All emitted terminal attempts need exact event counts,
correlation, summary reconciliation and privacy checks. A test's presence or an
expected nonzero exit is not proof of its exact telemetry stage/category.

The controlling code is `Client.Copy`, `Client.Sync`, `Client.ResumeJob`,
`attemptTelemetryFinalizer.finish`, `terminalAttemptStatus` and
`jobErrorAttributes`. Actual stage values are `initialization`, `enumeration`,
`transfer`, `completion` and `completed`; there is no literal `finalization` value.
Successful and partially successful terminal statuses normalize stage to
`completed`. Failed or cancelled attempts retain the stage reached by control
flow, which can differ from the phase where an individual file failed.

| Outcome/boundary | Expected telemetry contract | Existing E2E evidence | Recommended addition |
| --- | --- | --- | --- |
| Validation/client construction before attempt creation | No job lifecycle pair under the current contract. Do not demand a synthetic Failed event. | Missing-auth/input and invalid-job cases exist. Copy/sync executor construction, missing resume plans and endpoint parsing precede lifecycle start. | Explicit no-lifecycle expectations for selected negative cases, after confirming which boundary each actually reaches. |
| All successful transfers | Completed, stage completed, exact successful counters, no job error category/code. | SingleFile/MultiFileUploadDownload, OAuth and broad topology suites; default manifest compares summaries. | Add explicit completed-stage/empty-error expectations to representative existing tests; no new fixture. |
| No-op sync / zero selected work | Completed with zero scheduled work when applicable; not automatically a skipped-transfer status. | Idempotent resync, empty-container and selective-sync tests. | Pin zero-work versus skipped-transfer semantics in these existing cases. |
| Completed with skipped transfers | CompletedWithSkipped where the engine classifies skipped transfers; stage completed, correct skipped counts. | NonOverwriteSingleFile now pins CompletedWithSkipped/completed, one skipped, zero completed/failed and zero transferred bytes, empty error fields, and unchanged destination content. | Implemented for this fixture. Filtered/unchanged files are still distinct from TransfersSkipped. |
| Some transfers fail, others succeed | CompletedWithErrors; stage completed; job category transfer and code transfer-failures; correct success/failure counts. | Failure workflows exist, but no dedicated mixed-success fixture with explicit telemetry outcome was established in this audit. | High priority: add one failed and one successful object to an existing controlled failure fixture. |
| Successes, failures and skips together | CompletedWithErrorsAndSkipped; stage completed; all three outcome counts and sanitized failure information. | No explicit three-outcome telemetry scenario identified. | Reuse the preceding fixture with one additional skipped object, rather than create a separate harness. |
| Entire job fails during transfer work | Failed; exact stage reached and error representation, failed counters and no raw paths/errors. | JobResume now pins Failed/completion with category completion and code completion-error, one failed transfer and zero completed/skipped/bytes, then exact successful resume counters and cleared errors. This is the current failed-summary path, not a typed 404 at the finalizer. Files-quota and read-only NFS fixtures remain available. | Live-validate the stronger Blob assertion. Add exact stage/category expectations to quota/read-only cases only after confirming their top-level error path; do not infer typed 404 or local-io from a per-transfer message. |
| Enumerator/client initialization after lifecycle start | Failed + initialization; one start/finish even without transfers; sanitized category/code. | Real negative-auth workflows exist, but no proof they hit this exact post-start boundary. TestTelemetryInitializationFailureFinalization covers the finalizer locally. Resume service-client failure has an explicit production reporting path. | One bounded post-start initialization case, ideally extending a resume-client or enumeration-setup failure. Do not substitute pre-start credential failure. |
| Enumeration fails after initialization | Failed + enumeration; accumulated counters if available; one pair and sanitized category/code. | Empty-SAS/error-code tests exercise access errors, but do not pin a failure after enumerator initialization. | One controlled failure after a scan has begun, with an observable boundary; avoid timing-only source deletion. |
| Graceful cancellation | Cancelled; actual stage reached, percent/counters, empty job-level error category/code, no private data. | Six Linux NFS cases now check stage membership in enumeration/transfer/completion, empty job errors, finite percent below 100, matching final summary and expanded path/SAS privacy. Existing timing and resume behavior are unchanged. | Strengthening implemented. Deterministically targeting one cancellation phase, native signals and cross-platform telemetry remain separate variants. |
| Completion/finalization before finish emission | A returned failure can retain completion stage. A panic is not automatically a returned error. | No deterministic real E2E fixture targets this narrow boundary. Local idempotence/panic tests exist. | Lower priority: identify a supported real failure path first; do not add a production-only failure switch solely for a test. |
| Failure after finish emission / forced termination | An already emitted finish cannot be revised. Hard termination may leave only a start; it is not graceful cancellation. | Copy/sync finish before logging and completion callbacks; telemetry collection/sending has local failure tests. | Treat post-finish behavior and unmatched-start handling as separate lifecycle/consumer tests, not a guaranteed Failed terminal pair. |

Error-category assertions should cover authentication, authorization, not-found,
conflict (409/412), timeout, network, local I/O, throttling (429/503), service errors
and the safe generic fallback. Existing missing-auth, missing-container, quota and
read-only workflows supply some inputs. `TestJobErrorAttributes` covers mapping
locally, but real E2E stage/category assertions are not complete. Categories
depend on the typed error reaching the finalizer: a Failed job reported only via
its summary may use a stage fallback, while partial failure uses transfer-failures.

No-overwrite, failed Blob upload/resume and six cancellation assertions have now
been strengthened without new transfer fixtures or fault injection. Next steps:
(1) live-validate these expectations and pin the quota/read-only error paths;
(2) add mixed-result variants to an existing fixture; (3) target one post-start
initialization and one enumeration failure; (4) add controlled network/timeout/
throttling cases if required. Keep post-finish and telemetry-send failures distinct
from transfer outcomes.

## S3/GCS And Functional 11 Effort

**S3/GCS: medium, not a small assertion patch.** Existing Python transfer fixtures
should be retained. `testSuite/scripts/utility.py` launches shell command strings,
combines stdout/stderr and discards successful output in its boolean helper.
`run.py` only checks the unittest result. Neither uses the Go per-process telemetry
manifest, query poller or negative-event observation window.

The minimum reliable change is: add structured per-command expectations and
unique process correlation in the Python runner; retain/parse final JSON summaries
without breaking text-output callers; export a bounded manifest and validate it
using a reusable adapter to the existing query verifier; and configure the test
component/workspace plus query identity in the smoke-test job. Regression tests
must reject missing/duplicate events, partial query results and field mismatches.
S3/GCS credentials and their existing skip gates remain unchanged. Reusing the Go
verifier is preferable to implementing a second independent query checker.

Rough engineering estimate: 1-2 days for the harness bridge and focused tests,
plus credential-enabled live validation; this is not a schedule commitment and
assumes existing test resources/query access. Deferred under the request to do
only small additions now. No Python files, credentials or pipelines were changed.

**Functional 11: small subset implemented.** Added public/empty cloud assertions
to existing SAS/local and FileBlob OAuth tests, auth/cloud assertions to existing
BlobFS SAS/OAuth and multiflush uploads, and mixed SAS-to-OAuth BlobBlob checks.
The focused runner includes these cases. TestTelemetryManifestAuthCloud verifies
incorrect auth/cloud/FromTo values fail in either event. These are local contract
passes, not live Azure passes.

The remaining shared-key BlobFS smoke tests live in the same Python harness, so
they can share the bridge above. Successful public-anonymous coverage depends on
an account where public access is permitted; existing empty-SAS rejection tests
are not substitutes. Sovereign/unknown-cloud classification has unit coverage,
but real E2E requires suitable endpoints and credentials. Do not relabel a public
Azure endpoint or enable public account access merely to claim those rows covered.

## Non-Functional Matrix

| # | Document scenario | Current coverage and remaining limits |
| --- | --- | --- |
| 1 | Concurrent identity | Implemented four concurrent real Blob downloads sharing a temporary home, unique job/invocation IDs, shared queried installation ID and validated contents. Explicit blocked-one-job isolation remains unit-tested rather than real-service tested. |
| 2 | Ingestion/query resilience | Real ingestion/query polling and negative observation are wired to the existing verifier. Controlled live ingestion delay/query-service failures remain missing; verifier retry/error handling has unit coverage. |
| 3 | Dashboards/enrichment | Existing offline schema/description tests and read-only synthetic aggregate replay. No live dashboard import, browser validation, or enrichment-service test in this pass. |
| 4 | Non-interference | Deterministic rejection/partial/throttling/stall cases remain explicitly loopback integration tests. Existing separate real emergency-shutdown tests are retained. Full real-service fault matrix is not implemented here. |
| 5 | Performance comparison | Existing opt-in paired CLI/instrumentation benchmarks and recorded results. Not rerun or promoted to a passing 1%/100 KiB release gate; that needs a controlled host and agreed memory definition. |
| 6 | Payload/binary size | New actual CLI start/finish envelope limits plus existing both-backend serialized fixture gates. Binary-growth gate exists in the separate manual pipeline; no new release-pipeline wiring. |
| 7 | Unreachable/reject | Closed-port and HTTP 400/206/429/503 cases are integration tests; DNS/TLS failures have transport-level tests. Real-service/network failure variants remain missing. |
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