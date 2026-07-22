# AzCopy Business Metrics ADX Dashboard

This directory contains an importable ADX dashboard and its source KQL for the business metrics requested in `StorageMoverAzCopyComparison.md`.

## Import The Dashboard

1. Open [ADX Dashboards](https://dataexplorer.azure.com/dashboards) in a browser that supports your Microsoft certificate authentication.
2. Select the arrow next to **New dashboard**.
3. Select **Import dashboard from file**.
4. Select `azcopy-business-metrics.dashboard.json` from this directory.
5. Name the dashboard `AzCopy Business Metrics` and select **Create**.

The imported dashboard contains 15 tiles across five pages and uses one XStore data source. Client telemetry and XDataAnalytics queries use explicit cross-cluster references, so no additional dashboard data sources are required.

If the imported XStore source needs reconnecting, set it to:

```text
Cluster: https://azcore.centralus.kusto.windows.net
Database: Xstore
```

Regenerate the dashboard after changing a source `.kql` file:

```powershell
./dashboards/azcopy-business-metrics/generate-dashboard.ps1
```

The generator uses Microsoft's documented ADX dashboard schema version 60 and stable IDs, so regeneration produces reviewable output rather than unrelated ID churn.

## Data Sources

The generated dashboard uses XStore as its single dashboard data source and references the other sources through cross-cluster KQL. It includes a 24-hour default time-range parameter that defines `_startTime` and `_endTime`. The source details are documented below for troubleshooting and manual use.

### Client Telemetry

Name: `AzCopyClientTelemetry`

```text
https://ade.applicationinsights.io/subscriptions/31347be8-d066-464e-9866-7e58d85027b7/resourcegroups/sharankur_playground/providers/microsoft.insights/components/sharankur_insights1
```

Database: `sharankur_insights1`

Table: `customMetrics`

For cross-service queries initiated from a native ADX cluster, use `https://adx.monitor.azure.com/...` as shown in `queries/server/04_client_server_account_hour.kql`.

### XStore User-Agent Telemetry

Name: `XStoreUserAgent`

```text
https://azcore.centralus.kusto.windows.net
```

Database: `Xstore`

Function: `XAggUserAgentTelemetryMetric()`

### Storage Transaction And Account Data

Name: `XDataAnalytics`

```text
https://xdataanalytics.westcentralus.kusto.windows.net
```

Database: `XDataAnalytics`

Tables:

- `XStoreAccountTransactionsHourly`
- `XStoreAccountPropertiesDaily`

### Subscription Tenant Enrichment

ARG 1P is required to map `SubscriptionId` to the owning Microsoft Entra tenant. This query is intentionally not placed directly on the dashboard until ARG access and privacy controls are approved. Use `InternalSubscriptionResources` with a 60-hour lookback and latest-record `arg_max` as documented by ARG 1P.

## Parameters

Every ADX dashboard has the default time-range parameter. The queries use:

- `_startTime`
- `_endTime`

Add one free-text parameter:

| Label | Variable | Type | Default | Pages |
| --- | --- | --- | --- | --- |
| Storage account | `_account` | String | Empty | Customer Drilldown, Server Correlation |

## Page And Tile Manifest

### Overview

| Tile | Query | Data source | Visual |
| --- | --- | --- | --- |
| Jobs, bytes, completion and unmatched starts | `queries/client/01_overview_cards.kql` | AzCopyClientTelemetry | Multi stat |
| Volume by topology | `queries/client/02_volume_topology_trend.kql` | AzCopyClientTelemetry | Stacked column or time chart |
| Outcomes by command | `queries/client/03_outcome_distribution.kql` | AzCopyClientTelemetry | Stacked bar |
| Weekly command and version trend | `queries/client/10_weekly_command_version_trend.kql` | AzCopyClientTelemetry | Stacked column chart |

### Performance

| Tile | Query | Data source | Visual |
| --- | --- | --- | --- |
| Throughput and completion-time percentiles | `queries/client/04_performance_percentiles.kql` | AzCopyClientTelemetry | Table |
| Platform and version mix | `queries/client/07_platform_mix.kql` | AzCopyClientTelemetry | Table or bar chart |

### Reliability

| Tile | Query | Data source | Visual |
| --- | --- | --- | --- |
| Reliability rates | `queries/client/05_reliability_cards.kql` | AzCopyClientTelemetry | Multi stat |
| Top job error categories | `queries/client/06_error_distribution.kql` | AzCopyClientTelemetry | Bar chart |
| Telemetry quality | `queries/client/08_telemetry_quality.kql` | AzCopyClientTelemetry | Multi stat |

### Customer Drilldown

| Tile | Query | Data source | Visual |
| --- | --- | --- | --- |
| Observed job attempts | `queries/client/09_account_drilldown.kql` | AzCopyClientTelemetry | Table |
| Top source/destination movers | `queries/client/11_top_account_movers.kql` | AzCopyClientTelemetry | Table or bar chart |
| Account ownership | `queries/server/03_account_ownership.kql` | XDataAnalytics | Table |

### Server Correlation

| Tile | Query | Data source | Visual |
| --- | --- | --- | --- |
| Server-observed AzCopy requests | `queries/server/01_xagg_azcopy_requests.kql` | XStoreUserAgent | Time chart |
| Storage API and error mix | `queries/server/02_storage_operation_mix.kql` | XDataAnalytics | Bar chart or table |
| Requests per estimated job by account/hour | `queries/server/04_client_server_account_hour.kql` | XStoreUserAgent | Table or time chart |

The account/hour correlation is not a job-to-request join. It correlates client jobs and server requests by normalized destination account and hour.

## Interpretation Rules

- `ObservedAttempts` is the sampled row count.
- `EstimatedAttempts` and estimated byte totals use inverse-probability weighting from each event's `SamplingRate`.
- Percentiles are calculated over sampled jobs. Always show `SampleSize`; do not weight percentile values.
- Aggregate retry/throttle rates from raw numerators and denominators, never by averaging per-job percentages.
- Keep source and destination account roles separate. Do not duplicate S2S jobs in global totals.
- Treat unmatched starts as probable abandonment only after an agreed ingestion timeout.
- XStore `Tenant`/`LogicalTenant` values are Storage deployment tenants, not customer Entra tenant IDs.

## Unsupported Or Partial Business Metrics

Do not create authoritative tiles for these until their dependencies exist:

- First-ever job success, time to first success, and second-job conversion: requires an account/subscription cohort sampler.
- Exact last-active date per customer: JobID sampling can omit the actual latest job.
- Finalization duration: not emitted separately.
- Comprehensive retries and retry overhead: current counters do not cover all SDK/body-read retries.
- Public/private network bytes: configured endpoint kind is not actual route attribution.
- Support cases, diagnosis, mitigation, resolution, repeat contact, and escalation: require support-system joins.
- Exact job-to-Storage-request attribution: requires a job/run correlation value on Storage requests.

## Validate Client Tiles

The validator substitutes a two-day time range and an empty account filter, then executes every client query against the configured Application Insights resource.

```powershell
./dashboards/azcopy-business-metrics/validate-client-queries.ps1
```

Override the defaults when needed:

```powershell
./dashboards/azcopy-business-metrics/validate-client-queries.ps1 `
  -App sharankur_insights1 `
  -ResourceGroup sharankur_playground `
  -Offset 7d
```

## Production Evolution

The MVP can use live cross-service queries. For production, persist curated attempt facts and daily/hourly aggregates in a central ADX database before client and XAgg retention expires. Point the final Azure Managed Grafana dashboard at those curated tables rather than repeatedly joining raw telemetry in every panel.

References:

- [ADX dashboards](https://learn.microsoft.com/azure/data-explorer/azure-data-explorer-dashboards)
- [ADX dashboard parameters](https://learn.microsoft.com/azure/data-explorer/dashboard-parameters)
- [ADX and Azure Monitor cross-service queries](https://learn.microsoft.com/azure/data-explorer/query-monitor-data)