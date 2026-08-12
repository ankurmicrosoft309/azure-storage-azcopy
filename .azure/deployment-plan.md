# Azure Deployment Plan

> **Status:** Deployed

Generated: 2026-08-12T03:45:37+05:30

---

## 1. Project Overview

**Goal:** Deploy the existing workspace-based Application Insights backend used to validate AzCopy E2E telemetry ingestion.

**Path:** Add Components to Existing Project

---

## 2. Requirements

| Attribute | Value |
|-----------|-------|
| Classification | Development / E2E test infrastructure |
| Scale | Small |
| Budget | Cost-Optimized |
| Subscription | XDatamove-Dev-Playground1 (`31347be8-d066-464e-9866-7e58d85027b7`) |
| Resource group | `azcopy-e2e-test-rg` |
| Location | `eastus` |

The user confirmed the subscription/resource group and selected `eastus`.

---

## 3. Components Detected

| Component | Type | Technology | Path |
|-----------|------|------------|------|
| Telemetry infrastructure | Infrastructure as Code | Bicep | `infra/telemetry/main.bicep` |
| Test deployment parameters | Configuration | Bicep parameters | `infra/telemetry/test.bicepparam` |
| E2E telemetry verifier | Test integration | Go and Log Analytics REST API | `e2etest/newe2e_app_insights_validation.go` |
| E2E pipeline integration | CI | Azure Pipelines | `azurePipelineTemplates/run-e2e.yml` |

---

## 4. Recipe Selection

**Selected:** Bicep

**Rationale:** The repository already contains a purpose-built, standalone Bicep template and parameter file. Direct resource-group deployment preserves the existing architecture and avoids introducing AZD or EV2.

---

## 5. Architecture

**Stack:** Azure Monitor telemetry backend

### Service Mapping

| Component | Azure Service | SKU |
|-----------|---------------|-----|
| E2E telemetry ingestion and correlation | Workspace-based Application Insights | Consumption-based |
| E2E telemetry storage and query | Log Analytics workspace | `PerGB2018` |

### Configuration

| Setting | Value |
|---------|-------|
| Environment | `test` |
| Application Insights name | `azcopy-telemetry-test-ai` |
| Log Analytics workspace name | `azcopy-telemetry-test-law` |
| Retention | 90 days |
| Daily ingestion cap | 1 GB |
| Public ingestion/query | Enabled |
| Local ingestion authentication | Enabled because the AzCopy client uses a runtime connection string |
| Optional E2E principal RBAC | Omitted for this initial local run |

### Security

- No credentials or secret values are stored in source control.
- The Application Insights connection string is treated as a routing value, not an authorization secret.
- The first live query uses the current signed-in Azure identity.
- Pipeline query RBAC remains optional until the Azure DevOps service principal object ID is resolved.

---

## 6. Provisioning Limit Checklist

The Microsoft.Quota API returned `BadRequest` for both `Microsoft.OperationalInsights` and `Microsoft.Insights`, so the documented fallback was used: Azure Resource Graph/resource inventory plus the official Azure Resource Manager general limit of 800 resources per resource type per resource group.

| Resource Type | Number to Deploy | Current in Target RG | Total After Deployment | Limit/Quota | Notes |
|---------------|------------------|----------------------|------------------------|-------------|-------|
| `Microsoft.OperationalInsights/workspaces` | 1 | 0 | 1 | 800 per resource type per resource group | Quota CLI unsupported; ARG found 9 workspaces in `eastus` subscription-wide and 0 in the target RG; limit from official Azure subscription/service limits |
| `Microsoft.Insights/components` | 1 | 0 | 1 | 800 per resource type per resource group | Quota CLI unsupported; ARG found 2 components in `eastus` subscription-wide and 0 in the target RG; limit from official Azure subscription/service limits |

**Status:** All planned resources are within limits.

---

## 7. Execution Checklist

### Phase 1: Planning

- [x] Analyze workspace
- [x] Gather requirements
- [x] Confirm subscription and location with user
- [x] Prepare resource inventory
- [x] Fetch quotas and validate capacity
- [x] Scan codebase
- [x] Select Bicep recipe
- [x] Plan architecture
- [x] User approved this plan

### Phase 2: Preparation

- [x] Research Application Insights and Bicep deployment guidance
- [x] Use existing infrastructure and configuration files
- [x] Confirm no additional application configuration or Dockerfiles are required
- [x] Update plan status to `Ready for Validation`

### Phase 3: Validation

- [x] Invoke `azure-validate`
- [x] All validation checks pass
  - [x] Confirm Azure CLI authentication and subscription
  - [x] Compile Bicep
  - [x] Run Bicep lint
  - [x] Review assigned Azure policies
  - [x] Validate deployment against the target resource group
  - [x] Run deployment what-if
  - [x] Verify static RBAC assignments
- [x] Record validation proof below

### Phase 4: Deployment

- [x] Invoke `azure-deploy`
- [x] Execute the validated resource-group deployment
- [x] Verify both resources and the Application Insights workspace link
- [x] Run a Log Analytics smoke query
- [x] Retrieve runtime values for the live E2E run
- [x] Run a live E2E transfer and verify its terminal telemetry
- [x] Update plan status to `Deployed`

---

## 8. Validation Proof

| Check | Command Run | Result | Timestamp |
|-------|-------------|--------|-----------|
| Azure context | `az account show` | PASS - authenticated as `sharankur@microsoft.com` to `XDatamove-Dev-Playground1` (`31347be8-d066-464e-9866-7e58d85027b7`) | 2026-08-12 |
| Bicep compile | `az bicep build --file infra\telemetry\main.bicep --stdout` | PASS | 2026-08-12 |
| Bicep lint | `az bicep lint --file infra\telemetry\main.bicep` | PASS | 2026-08-12 |
| Policy review | `az policy assignment list` | PASS - reviewed 314 inherited/subscription assignments; the relevant public-network deny policy applies to Key Vault only, and tag assignments modify rather than deny these resources | 2026-08-12 |
| ARM validation | `az deployment group validate --subscription 31347be8-d066-464e-9866-7e58d85027b7 --resource-group azcopy-e2e-test-rg --template-file .\infra\telemetry\main.bicep --parameters .\infra\telemetry\test.bicepparam --name azcopy-telemetry-test-validation` | PASS - correlation ID `c3697fe6-4787-4f26-a488-351bb8a966ef` | 2026-08-12 |
| ARM what-if | `az deployment group what-if --subscription 31347be8-d066-464e-9866-7e58d85027b7 --resource-group azcopy-e2e-test-rg --template-file .\infra\telemetry\main.bicep --parameters .\infra\telemetry\test.bicepparam --name azcopy-telemetry-test --no-pretty-print --output json` | PASS - exactly two creates and no deletes or modifications | 2026-08-12 |
| Static RBAC review | Inspect `infra\telemetry\main.bicep`; `az role assignment list --assignee <signed-in-user> --include-groups --scope /subscriptions/31347be8-d066-464e-9866-7e58d85027b7 --include-inherited` | PASS - optional Log Analytics Reader and Reader assignments are deterministic, service-principal-only, least-privilege, and resource-scoped; omitted because `e2eQueryPrincipalId` is empty. Signed-in user inherits Owner and Contributor through `TM-XDataMove` for local verification | 2026-08-12 |

---

## 9. Deployment Proof

| Check | Result | Timestamp |
|-------|--------|-----------|
| Resource-group deployment | PASS - deployment `azcopy-telemetry-test` completed in approximately 26 seconds; correlation ID `31bb1e92-bd4e-4106-af0f-955ac7d05aad` | 2026-08-12 |
| Resource verification | PASS - `azcopy-telemetry-test-ai` and `azcopy-telemetry-test-law` are in `eastus` with provisioning state `Succeeded`; Application Insights is linked to the intended workspace | 2026-08-12 |
| Configuration verification | PASS - 90-day retention, 1 GB daily workspace quota, `PerGB2018` workspace SKU, and public ingestion/query enabled | 2026-08-12 |
| Query authentication | PASS - direct Log Analytics REST query `print SmokeTest=1` returned `1` using the signed-in Azure identity | 2026-08-12 |
| Live E2E validation | PASS - `TestNewE2E/BasicFunctionalitySuite/Scenario_SingleFile/copyBlobLocal` completed and suite teardown found one `azcopy.job.finished` event for run `local-20260811T225428Z-f1cc599d`, Job ID `8e363944-1d95-7646-574a-f43858d6a40b` | 2026-08-12 |
| Storage security restoration | PASS - Shared Key was enabled temporarily on the dedicated standard E2E account for fixture creation, then restored to `allowSharedKeyAccess=false` in a `finally` block | 2026-08-12 |
| Live RBAC verification | PASS for local validation - no direct role assignments were created because `e2eQueryPrincipalId` is empty; the signed-in user queried successfully through inherited access | 2026-08-12 |

---

## 10. Files

| File | Purpose | Status |
|------|---------|--------|
| `.azure/deployment-plan.md` | Deployment source of truth | Complete |
| `infra/telemetry/main.bicep` | Workspace-based Application Insights infrastructure | Existing |
| `infra/telemetry/test.bicepparam` | Cost-bounded E2E test parameters | Existing |

---

## 11. Next Steps

> Current: Deployed and verified with a live E2E transfer

1. Resolve the Azure DevOps service connection principal object ID when project access is available.
2. Redeploy with `e2eQueryPrincipalId` so CI receives the resource-scoped Reader and Log Analytics Reader assignments.
3. Configure the pipeline resource lookup variables and run the telemetry-enabled E2E stage.
