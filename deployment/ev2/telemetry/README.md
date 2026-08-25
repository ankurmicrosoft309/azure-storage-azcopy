# AzCopy telemetry EV2 deployment

This package deploys the production resources defined by
`infra/telemetry/main.bicep` to East US through region-agnostic EV2 and Managed
SDP. Bicep remains the source of truth; compiled ARM templates and parameters
are generated only as pipeline artifacts.

The normal AzCopy build runs `Test-TelemetryEv2PackageDeterminism.ps1`, validates
the package twice, and publishes the `ev2-telemetry-prod` artifact. The separate
`release.official.yml` pipeline is manual, consumes a main-branch build artifact,
and defaults to EV2 validation without deployment.

## One-time production onboarding

Before a production rollout can succeed:

1. Onboard `Microsoft.Azure.Storage.AzCopy.Telemetry.Prod` to EV2 Approval
   Service under ServiceTree ID `c88bf702-9a3e-4962-a15d-dbc3c3a8f42f`.
2. Map EV2 subscription key `azcopy-telemetry-prod` to subscription
   `bdc2378a-413b-4b55-bf7b-e97b7fd361cc` in tenant
   `33e01921-4d64-4f8c-a055-5bdaffd5e33d`.
3. Configure the Approval Service submitter and approver groups and grant the
   approved deployment identity the required subscription permissions.
4. Create an Azure Pipelines definition that targets
   `deployment/ev2/telemetry/release.official.yml`.

Queue the release pipeline with a successful main-branch build resource. Keep
`validateOnly` enabled for the first run. Set it to `false` only after validation
passes and the production change is approved.

No production deployment is performed by the normal build pipeline.
