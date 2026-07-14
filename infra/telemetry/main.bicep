// Provisions the Application Insights backend for AzCopy client-side telemetry.
//
// Deploys, per environment, a workspace-based Application Insights component
// (the only supported type now that classic App Insights is retired) bound to a
// dedicated Log Analytics workspace. Two environments are expected:
//   - test : fed only by the E2E pipeline (synthetic telemetry)
//   - prod : fed by preview/early-adopter and GA builds (real, sampled telemetry)
//
// See azcopy_telemetry_design.md -> "Staging the Application Insights instance"
// and "Provisioning approach" for the rationale.

@description('Deployment environment. Drives resource names and is stamped as a tag.')
@allowed([
  'test'
  'prod'
])
param environmentName string

@description('Azure region for all resources.')
param location string = resourceGroup().location

@description('Base name used to derive resource names.')
param baseName string = 'azcopy-telemetry'

@description('Data retention in days. 730 (2 years) satisfies the telemetry design requirement.')
@minValue(30)
@maxValue(730)
param retentionInDays int = 730

@description('Daily ingestion cap in GB. Bounds cost and protects against runaway/bogus ingestion.')
param dailyQuotaGb int = 10

var workspaceName = '${baseName}-${environmentName}-law'
var appInsightsName = '${baseName}-${environmentName}-ai'

var commonTags = {
  service: 'azcopy-telemetry'
  environment: environmentName
  managedBy: 'bicep'
}

resource workspace 'Microsoft.OperationalInsights/workspaces@2023-09-01' = {
  name: workspaceName
  location: location
  tags: commonTags
  properties: {
    sku: {
      name: 'PerGB2018'
    }
    retentionInDays: retentionInDays
    workspaceCapping: {
      dailyQuotaGb: dailyQuotaGb
    }
    features: {
      enableLogAccessUsingOnlyResourcePermissions: true
    }
    publicNetworkAccessForIngestion: 'Enabled'
    publicNetworkAccessForQuery: 'Enabled'
  }
}

resource appInsights 'Microsoft.Insights/components@2020-02-02' = {
  name: appInsightsName
  location: location
  kind: 'web'
  tags: commonTags
  properties: {
    Application_Type: 'web'
    WorkspaceResourceId: workspace.id
    IngestionMode: 'LogAnalytics'
    // ikey/connection-string ingestion is intentionally open: the connection
    // string is embedded in the open-source AzCopy binary and is not a secret.
    DisableLocalAuth: false
    RetentionInDays: retentionInDays
    publicNetworkAccessForIngestion: 'Enabled'
    publicNetworkAccessForQuery: 'Enabled'
  }
}

@description('The Application Insights connection string to embed in the matching AzCopy build (test build for E2E, prod build for preview/GA).')
output connectionString string = appInsights.properties.ConnectionString

@description('The Application Insights instrumentation key (routing only; not a secret).')
output instrumentationKey string = appInsights.properties.InstrumentationKey

output appInsightsResourceId string = appInsights.id
output workspaceResourceId string = workspace.id
