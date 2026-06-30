# Design for Telemetry for AzCopy 

## Project Context 
 
AzCopy (Azure/azure-storage-azcopy) is an open-source command-line utility for copying data to, from, and between supported storage locations. Depending on the scenario, supported endpoints include the local file system, Azure Blob Storage, Azure Files, Azure Data Lake Storage Gen2, Amazon S3, and Google Cloud Storage. 
 
AzCopy works by calling dataplane Azure Storage APIs, typically via the Azure Storage SDKs for Go. We add a header with every request of the form `User-Agent: AzCopy/10.24.0 azsdk-go-azblob/v1.3.1 (go1.19.12; Windows_NT)`, which helps collect some data about AzCopy usage on the server side. Example of using this can be accessed here: https://aka.ms/BlobClientTools (Ask Vishnu Charan TJ for access)

Storage Mover is an Azure cloud service which helps manage the transfer of data from and to different locations. It internally uses AzCopy. We collect metrics for Storage Mover, which can be accessed here: https://storagemoverprod-aaa9f9c6eyh6e7ex.eus.grafana.azure.com/dashboards/f/tpjJBAG4z/storage-mover-monitoring
 
## Problem statement 

Add client side telemetry/metrics collection to the AzCopy command line tool which will help us identify how customers are using AzCopy and its trends in usage over time.

## Goals and Non-Goals
 
- We want to collect metrics about: 
 - Amount of data transferred 
 - Number of objects transferred 
 - Latency of transfer 
 - Platform, such as Linux or Windows 
 - Geo region of the system where the command is run 
 - System configuration, such as CPU, memory, network, NIC speed, and whether data was transferred over public or private network 
 - Subcommand and CLI options usage 
   - Secret variables as part of CLI options should not be logged 
   - Need to consider very long argument lists 
 - Source, destination, and other source-target info, such as protocol, on-prem or intra-Azure 
   - Source mount information such as NAS, Windows Server, or clouds 
   - Destination information, including type and storage account 
 - Cloud type, such as public or government 
 - Auth mechanism, such as SAS key or Key Vault, without actual secret details 
 - Failure rates 
 - Any other customer info that can be captured, if possible, to give insights into where usage is coming from 
- Scalability such that ingestion of metrics from thousands of parallel AzCopy runs can be handled on the server side without data loss; eventual consistency is fine 
- Metrics should be easily accessible, similar to Azure Monitor or Geneva, and charts/dashboards can be made 
- Endpoint should be easily customizable, especially for testing 
- Retain data for at least 2 years 

## Scope

We are focused on CLI usage of AzCopy primarily. We already collect metrics from when AzCopy is used within the context of Storage Mover. 

The focus is on setting up a pipeline which is reliable and scalable and can help us make business decisions regarding focus on azcopy work.
  
## Stakeholders and Ownership 
 
- Product manager: Daniel Falkner 
- Other PMs: Raj Singh, Scott Hoag, Anusha Subramanian 
- Engineering managers: Sridhar Lanka and Raj Pathak 
- Engineer: Ankur Sharma

## Technical Design 
 
We plan to use a global Application Insights backend for collecting the metrics, provisioned as **two instances within a single dedicated subscription** — one **Test** instance fed only by the E2E pipeline, and one **Prod** instance fed by preview and GA builds (see [Staging the Application Insights instance](#staging-the-application-insights-instance-environments)). This subscription is dedicated to telemetry and has nothing to do with the customer's subscription. 
 
### Why Azure Monitor is a defensible metrics backend 

Azure Monitor gives us concrete, documented limits that are easier to defend than a custom logs-based pipeline. Application Insights has a financially backed 99.9% availability SLA.

For retention, workspace-based Application Insights data can be retained for up to 730 days, which matches the two-year requirement. Existing Application Insights guidance also documents a default daily cap of 100 GB/day, with increase paths up to 500 GB/day. This gives a concrete starting point for expected volume while leaving room to scale if adoption grows. 
 
Azure Monitor does not guarantee zero loss under unlimited burst traffic. Throttling can occur when ingestion limits are exceeded.

The Azure Monitor platform exposes utilization metrics and recommended alerts at 75% and 95% of ingestion limits, supports retry and offline storage behavior in the exporter, and gives us time to react before sustained throttling. If we stay within documented limits and monitor utilization, we can be reasonably sure Azure Monitor will not lose data.

### Scaling & service limits

Publically document Azure Monitor service limits - Azure Monitor | Microsoft Learn 
 
- Throttling: 32,000 events/second, measured over a minute 
- Total data per day limit: 100 GB 
 
After talking with Zaki Maksyutov from the Application Insights team, these publicly documented limits do not really apply to us and Application Insights is almost infinitely scalable for us: 
 
> Zaki: 100 GB - this limit you can bump yourself or completely remove. For 32k DPS - if you need more and have sustained ingestion, then we can bump it almost indefinitely. We will bump to next level only when the app is 10 times less than it, meaning once your app reaches 3.2k DPS your request will be approved. 

Lets run a rough analysis (with assumptions) to see if we will need to request a extension to the limits to begin with. The full derivation is in [Appendix A — Rough capacity analysis](#appendix-a--rough-capacity-analysis); in short, at 100% sampling our estimated volume (~1.5 billion runs/day, ~20k events/sec) would exceed the *default* limits, but those limits are liftable (per Zaki above) and we launch at **1% sampling**, which sits comfortably within them.

### Cost estimates for Azure Monitor
 
https://azure.microsoft.com/en-us/pricing/details/monitor 
 
If we assume 1 billion AzCopy runs/day (see Appendix A for this assumption): 

- Each metric payload is ~1 KB → 1000 GB/day → ~30 TB/month of ingested data 
- Pay-as-you-go at $2.30/GB: ~$24,150/month 
- Best commitment tier, 500 GB/day at $1.73/GB: ~$18,165/month 
- Storage and other costs are negligible in comparison to ingestion

With 1% sampling rate, this will be about $4000/month

### Subscription
 
We can use a single global non-customer-specific subscription in Azure to receive and store the metric data. 

Within that subscription we will provision **two Application Insights instances**: one **Test** instance that only receives synthetic telemetry from the E2E pipeline, and one **Prod** instance that receives real (sampled) telemetry from both preview/early-adopter and GA builds. Each instance gets its own connection string, which is embedded into the corresponding AzCopy build, so test telemetry never mixes with real customer telemetry. See [Staging the Application Insights instance (environments)](#staging-the-application-insights-instance-environments) for the detailed rationale and isolation options. 
 
We have to do a privacy review with related teams with PM help and agree that no privacy-sensitive data is saved. 
 
Seanmcc@microsoft.com is the contact person for this. 
 
### Staging the Application Insights instance (environments) 

Because the App Insights **connection string is embedded in the AzCopy binary** (see Authorization), the telemetry backend is staged primarily by *which connection string a given build embeds*, not by a runtime config. This gives us a clean, low-cost way to separate test traffic from real customer traffic. Recommended approach: 

- **Provision two App Insights resources**, each with its own connection string / instrumentation key, both defined in the same Bicep/ARM template and deployed with an environment parameter: 
  - **Test** — target of the E2E pipeline. Receives only synthetic telemetry from automated test runs. Lets us validate ingestion, schema, and dashboards without polluting real data. 
  - **Prod** — target of both the **preview/early-adopter** build and the **GA** build. Receives real (sampled) telemetry. The preview build is the controlled-exposure ("canary") phase against this same instance — exposure is bounded by the small preview audience plus the 1% sampling rate — and the GA build then ramps the same instance to the full installed base. 
  The preview and GA builds embed the **Prod** connection string; only automated E2E runs use the **Test** connection string. Synthetic test telemetry never mixes with real customer telemetry. 
- **Isolation options (in increasing order of separation):** 
  1. Two App Insights resources within the same resource group (simplest, shared RBAC/cost view). 
  2. Separate resource groups for Test vs Prod (cleaner cost attribution and access control). 
  3. Separate subscriptions for Prod vs non-Prod (strongest blast-radius and quota isolation; matches how many Microsoft services separate prod/non-prod). 
- **Alternative (single instance + dimension):** a single App Insights resource where every event carries an `Environment` attribute (we already emit resource attributes, so this is cheap to add). This is the lowest-cost option but mixes test and prod data in one resource, complicating cost accounting, RBAC, retention, and bogus-data filtering. Prefer two separate resources — App Insights has no fixed per-instance cost, you pay per GB ingested, so a low-traffic Test instance is nearly free. 
- **Region staging:** App Insights has no deployment-slot concept, but the Bicep template can be rolled out region-by-region (or you can start Prod in a single region) to bound early ingestion. 
- **Promotion** is by *build ring against the same Prod instance* — preview build (small audience + 1% sampling) → GA build (full audience) — validating ingestion and dashboards before ramping. The Test instance is provisioned once and only ever fed by E2E. 

### Azure Monitor OpenTelemetry Exporter 
 
AzCopy is written in Go. Use the industry-standard, vendor-neutral OpenTelemetry SDK. Add an exporter as a code component in the same binary which receives OTel data and forwards it to Application Insights. It ultimately does a POST request to the `/v2.1/track` endpoint of App Insights. 
 
POC standalone Go binary sending metrics to Application Insights: go-scratchpad/metrics/metrics.go at main · sharankur_microsoft/go-scratchpad 
 
## Authorization 
 
Authorization to Application Insights can be done in two ways: standard AAD and connection-string based. 
 
- Microsoft Entra ID / Azure AD token-based authentication 
 - The client obtains an OAuth2 bearer token from Entra ID and presents it on ingestion, so only authenticated callers can write telemetry 
 - Requires an identity, such as managed identity or service principal, granted the Monitoring Metrics Publisher role on the App Insights resource 
- Connection-string / Instrumentation-key 
 - Microsoft states the ikey is not a secret/security token; it only routes telemetry to a resource and does not gate who can write 
 - Simplest to deploy: no identity, no role assignment. Works for anonymous/OSS binaries 

The proposed solution is to hardcode/embed the connection string of the environment-appropriate Application Insights resource (**Test** for E2E runs, or **Prod** for preview and GA builds — see [Staging the Application Insights instance](#staging-the-application-insights-instance-environments)) hosted in the XClient subscription into the open-source AzCopy binary and use it to send client-side metrics. Each build embeds the connection string of its corresponding instance. 
 
The pitfall is that anyone who can extract the connection string will be able to send bogus metrics to Application Insights. However, they will not be able to read any metrics data just based on a connection string. 
 
Azure CLI and Azurite are two other open-source projects using publicly accessible connection strings for sending metrics to Application Insights. 
 
### Is it possible to use customer Entra ID identity to send metrics to Application Insights? 
 
Likely not possible. The telemetry backend is a Microsoft-owned Application Insights resource in a separate subscription. To use Entra ID for telemetry ingestion, the caller identity must be granted the Monitoring Metrics Publisher role on that specific Application Insights resource. 
 
### How to differentiate between bogus data and genuine data? 
 
Heuristics: 
 
- Compare that we received a corresponding number of requests on Azure Storage data plane APIs for the duration of the job run 
- Count and relative timings of start job metric events and finish job metric events should make sense 
- Discard wrong schema 

## Exact metrics being sent 

Each event is sent to Application Insights as a single `Microsoft.ApplicationInsights.Metric` envelope. The numeric data points live under `data.baseData.metrics`, and all resource attributes + job dimensions are sent once as a shared `data.baseData.properties` bag (all property values are strings). The `job.started` and `job.finished` events for one run share the same `RunID` so they can be correlated.

The examples below are generated directly from the AzCopy serialization code (an on-prem-style block-blob upload from local disk to a public-cloud Blob account, with a few failed transfers).

### Event 1 — `azcopy.job.started`

```json
{
  "name": "Microsoft.ApplicationInsights.Metric",
  "time": "2026-06-25T14:30:00Z",
  "iKey": "00000000-0000-0000-0000-000000000000",
  "data": {
    "baseType": "MetricData",
    "baseData": {
      "metrics": [
        {
          "name": "azcopy.job.started",
          "value": 1,
          "count": 1
        }
      ],
      "properties": {
        "BlobType": "BlockBlob",
        "CloudType": "public",
        "Command": "copy",
        "DestAuthMechanism": "OAuthToken",
        "DestEndpointKind": "public",
        "DestProtocol": "https",
        "DestStorageAccount": "mystorageacct",
        "DestType": "Blob",
        "FromTo": "LocalBlob",
        "GeoCountry": "United States",
        "GeoRegion": "eastus",
        "GeoTimezone": "America/New_York",
        "HostArch": "amd64",
        "HostCPUModel": "Intel(R) Xeon(R) Platinum 8370C CPU @ 2.80GHz",
        "HostMemoryTotalGB": "32",
        "HostNICSpeedMbps": "10000",
        "HostNumCPU": "8",
        "HostVirtualization": "azure-vm",
        "InstallationID": "8f14e45fceea167a5a36dedd4bea2543",
        "InvocationContext": "ci",
        "NetworkRunContext": "azure-vm",
        "OSType": "linux",
        "OSVersion": "Ubuntu 22.04.3 LTS",
        "OptBlockSizeMB": "8",
        "OptCapMbps": "false",
        "OptConcurrency": "32",
        "OptFlagsSet": "recursive,put-md5,block-size-mb,block-blob-tier",
        "OptOverwrite": "true",
        "OptPreserveSMBPermissions": "false",
        "OptPutMD5": "true",
        "OptRecursive": "true",
        "RequestedAccessTier": "Cool",
        "RunID": "b3f2c1a4-9d5e-4f8a-bc12-3456789abcde",
        "ServiceName": "azcopy",
        "ServiceVersion": "10.32.2",
        "SourceAuthMechanism": "Anonymous",
        "SourceMountType": "local-disk",
        "SourceProtocol": "local",
        "SourceStorageAccount": "",
        "SourceType": "Local",
        "TransferDirection": "upload",
        "TransferTopology": "intra-azure"
      }
    }
  }
}
```

### Event 2 — `azcopy.job.finished`

The finished event carries the same property bag plus `JobStatus` and (when there were failures) `FailureErrorCodes`, and adds all the numeric measurements.

```json
{
  "name": "Microsoft.ApplicationInsights.Metric",
  "time": "2026-06-25T14:42:18Z",
  "iKey": "00000000-0000-0000-0000-000000000000",
  "data": {
    "baseType": "MetricData",
    "baseData": {
      "metrics": [
        {
          "name": "azcopy.job.finished",
          "value": 1,
          "count": 1
        },
        {
          "name": "azcopy.bytes_transferred",
          "value": 5368709120,
          "count": 1
        },
        {
          "name": "azcopy.bytes_over_wire",
          "value": 5402263552,
          "count": 1
        },
        {
          "name": "azcopy.transfers_completed",
          "value": 1280,
          "count": 1
        },
        {
          "name": "azcopy.transfers_failed",
          "value": 3,
          "count": 1
        },
        {
          "name": "azcopy.transfers_skipped",
          "value": 12,
          "count": 1
        },
        {
          "name": "azcopy.transfers_total",
          "value": 1295,
          "count": 1
        },
        {
          "name": "azcopy.duration_seconds",
          "value": 738,
          "count": 1
        },
        {
          "name": "azcopy.throughput_mbps",
          "value": 58.2,
          "count": 1
        },
        {
          "name": "azcopy.avg_e2e_latency_ms",
          "value": 42,
          "count": 1
        },
        {
          "name": "azcopy.avg_iops",
          "value": 173,
          "count": 1
        },
        {
          "name": "azcopy.server_busy_pct",
          "value": 0.8,
          "count": 1
        },
        {
          "name": "azcopy.network_error_pct",
          "value": 0.2,
          "count": 1
        }
      ],
      "properties": {
        "BlobType": "BlockBlob",
        "CloudType": "public",
        "Command": "copy",
        "DestAuthMechanism": "OAuthToken",
        "DestEndpointKind": "public",
        "DestProtocol": "https",
        "DestStorageAccount": "mystorageacct",
        "DestType": "Blob",
        "FailureErrorCodes": "403:2,500:1",
        "FromTo": "LocalBlob",
        "GeoCountry": "United States",
        "GeoRegion": "eastus",
        "GeoTimezone": "America/New_York",
        "HostArch": "amd64",
        "HostCPUModel": "Intel(R) Xeon(R) Platinum 8370C CPU @ 2.80GHz",
        "HostMemoryTotalGB": "32",
        "HostNICSpeedMbps": "10000",
        "HostNumCPU": "8",
        "HostVirtualization": "azure-vm",
        "InstallationID": "8f14e45fceea167a5a36dedd4bea2543",
        "InvocationContext": "ci",
        "JobStatus": "CompletedWithErrors",
        "NetworkRunContext": "azure-vm",
        "OSType": "linux",
        "OSVersion": "Ubuntu 22.04.3 LTS",
        "OptBlockSizeMB": "8",
        "OptCapMbps": "false",
        "OptConcurrency": "32",
        "OptFlagsSet": "recursive,put-md5,block-size-mb,block-blob-tier",
        "OptOverwrite": "true",
        "OptPreserveSMBPermissions": "false",
        "OptPutMD5": "true",
        "OptRecursive": "true",
        "RequestedAccessTier": "Cool",
        "RunID": "b3f2c1a4-9d5e-4f8a-bc12-3456789abcde",
        "ServiceName": "azcopy",
        "ServiceVersion": "10.32.2",
        "SourceAuthMechanism": "Anonymous",
        "SourceMountType": "local-disk",
        "SourceProtocol": "local",
        "SourceStorageAccount": "",
        "SourceType": "Local",
        "TransferDirection": "upload",
        "TransferTopology": "intra-azure"
      }
    }
  }
}
```

## Timing of metrics being sent 
 
We can send metrics in two batches: one at the start of the process with details about source, target, platform, CLI options, subcommand, etc.; and one at the end with number of objects, number of bytes transferred, number of failures, latency stats, etc. 
 
This allows us to get some information even if the AzCopy command does not run to completion, for example due to crash or being killed. 
 
### Error handling and failure mode 
 
HTTP 429 and 503 can be retried asynchronously a couple of times with exponential backoff and jittering. Permanent failures like DNS error, bad configuration, or authorization error should not be retried. 
 
### Does AzCopy stall if Azure Monitor throttles? 
 
No. The exporter path should run in a separate asynchronous pipeline. Go has the concept of goroutines, which should make this easier to program. We should perform batching and send metrics network requests in a batch. We can queue the batches and prefer to drop batches if the queue is full. 
 
### Backoff policy 
 
Use exponential backoff with jitter for retryable failures, with a max cap of maybe three retries. 
 
### How the error is surfaced to the customer 
 
Non-fatal warning in debug/verbose logs, but it should not fail the AzCopy command. Making the error more visible may confuse customers about whether there was a critical issue with their data transfer versus a metrics issue. Developers may rely on E2E validation to detect telemetry pipeline issues. 

### Do we ask customers to turn off telemetry? 
 
We can expose a command-line option to turn off telemetry, but it should be on by default. Otherwise, customer adoption and data will be low. Retry policy, bounded buffering, and dropping telemetry when needed should make it so customers do not have to think about this. 
 
### Local caching / buffering 
 
Not required to cache or buffer the metrics. AzCopy CLI is a short-lived process and we need to dispatch metrics immediately upon job start and job finish. 
 
### Competing for resources with AzCopy 
 
Investigate how much additional memory usage is added by metrics collection and whether it can be turned on without significant impact, for example no more than 100 KB. 
 
# Timelines

| Category | Activity | Owner | ETA | Comments |
|---|---|---|---|---|
| Spec Closure | Security review | Raj, Sridhar, Daniel | 17/06/2026 | DONE |
| Spec Closure | Present design spec in Scrum group | Ankur | 01/07/2026 |  |
| Spec Closure | Present design in Tuesday XDataManagement review meeting | Ankur | --/06/2026 |  |
| Spec Closure | Privacy review | Daniel Falkner | 02/07/2026 |  |
| Development and Integration | Provision the two Application Insights instances (Test + Prod) in XClient subscription using ARM or Bicep template in AzCopy pipeline | Ankur | 02/07/2026 |  |
| Development and Integration | Implement sending of metrics with configurable sampling rate in AzCopy, default 1% | Ankur | 04/07/2026 |  |
| Development and Integration | Create the dashboards and metrics on Application Insights Azure portal, and integrate with other server-side Grafana dashboard to filter out bogus data | Ankur | 07/07/2026 |  |
| Development and Integration | Add E2E tests for receiving the metrics in Azure Monitor from AzCopy | Ankur | 09/07/2026 |  |
| Milestone | M1 — Code Complete (telemetry implemented, E2E tests implemented, merged to main, unit tests green, design/security/privacy reviews closed) | Ankur | 09/07/2026 |  |
| Development and Integration | Open PR against the AzCopy `main` branch (CI runs build + E2E across Linux/Windows/macOS) | Ankur | 10/07/2026 |  |
| Development and Integration | Feature sign-off for AzCopy | Sridhar | 14/07/2026 |  |
| Development and Integration | Merge PR into `main` after approvals and required checks pass | Ankur | 15/07/2026 |  |
| Development and Integration | R2D Checklist sign-off |  | - |  |
| Deployment | Publish a prerelease / early-adopter build of AzCopy (emitting to the Prod Application Insights instance, bounded by the small preview audience + 1% sampling) | Ankur | 17/07/2026 |  |
| Deployment | Monitor the metrics ingestion rates and bogus metric percentage |  | Throughout |  |
| Milestone | M2 — E2E testing validated in Test environment / preview build (Canary SignOff) — telemetry backend validated end-to-end via E2E pipeline; client shipped as a GitHub **prerelease/preview** build with sampling at 1% as the controlled-exposure knob | Ankur | 23/07/2026 |  |
| Milestone | M3 — Production deployment and signoff — promote prerelease to GA release across all channels (download/version-check container, GitHub release, Docker/ACR, Linux PMC repos) (date depends on AzCopy's release cadence — stable minors ship ~quarterly, with a ~1–2 month preview→GA soak window) | Ankur | --/--/2026 |  |
|
# Staggered rollout 
 
Start deployment with a minimum sampling rate of 1%. Observe ingestion rates and costs. We can also do this by geography to start with, for example roll it out only for EMEA. Depending on costs and confidence in metrics, increase sampling rate to 5% or 10% in the next rollout, and then add more geographies. 

## How this maps to AzCopy's actual release process

AzCopy is a **client-side CLI**, not a hosted service, so there is no blue/green canary fleet or deployed environment to soak. The release pipeline (`build-1es-pipeline.yaml`) is a manually-triggered build → ESRP sign → verify → publish pipeline (run off `refs/tags/release`) that pushes binaries to four independent channels: the download/version-check storage container (powers `azcopy --check-version`), GitHub releases, Docker images on ACR, and the Linux package repositories via PMC (apt/yum). Each channel is gated by its own pipeline parameter, so publishing can be staged channel-by-channel.

Given that, the "canary" and "soak" for AzCopy are achieved through two knobs rather than a separate environment:

1. **Prerelease / preview build (the canary ring).** The pipeline can publish a build as a GitHub **prerelease** (`prerelease`/`draft` parameters) before promoting it to a full GA release. Early adopters run the preview build first; we observe telemetry ingestion and correctness, then promote to GA. This prerelease-then-promote window is the effective soak period — it is process-driven, not a fixed time gate in the pipeline.
2. **Sampling rate (controlled exposure).** Even on a GA build, the 1% default sampling rate limits how much of the installed base actually emits metrics, so the blast radius is bounded independently of which build is published. We ramp sampling (1% → 5% → 10%) and/or geography as confidence grows.

Rollback, if a bad telemetry build reaches production, is handled by the pipeline's `RemovePackagesFromLinuxRepository` stage for Linux repos and by replacing the published binary/GitHub release for the other channels. The `--disable-telemetry` flag (and the opt-out env var) provide an additional customer-side kill switch.

# Appendix

## Appendix A — Rough capacity analysis

This is the back-of-the-envelope capacity math referenced under [Analysis of service limits](#analysis-of-service-limits). It is preserved for reference; the practical conclusion is that we launch at 1% sampling, well within limits, and the default limits are liftable if/when we ramp.

Max AzCopy runs per day or per second a single instance of Azure Monitor can support: 
 
- Number of metric events per AzCopy run that will be ingested: 2 
- Amount of metrics data per AzCopy run that will be transferred: ~2 KB 
- Assume 100% sampling rate
- Max AzCopy runs supported per day in terms of data limit: 400 million 
- Max AzCopy runs supported per second in terms of events throttling: 16,000

Screenshot from https://aka.ms/BlobClientTools: 
 
- Number of requests from AzCopy towards Azure Storage per week: ~200 billion, which is ~1 billion per hour, or ~300k/sec 
- Number of unique storage accounts: ~1 billion per week 
- Number of AzCopy job runs in a week: not known 
- If we assume number of AzCopy runs to be 10x the number of unique storage accounts, we get 10 billion AzCopy runs per week, 1.5 billion per day, or 20k per second
 
1.5 billion is higher than 400 million calculated earlier. 20k per second is higher than the 16k per second limit calculated earlier. 

So based on the above stated estimates, assumptions and at at 100% sampling rate, we will need to request extension to the service limits.

But we are planning to release with 1% sampling rate which will be comfortably within the limits and will also give us a more accurate estimate of how much metrics we will expect to ingest with a higher sampling rate.
