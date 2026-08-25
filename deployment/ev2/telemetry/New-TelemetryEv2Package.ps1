[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $OutputPath,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\d+\.\d+\.\d+\.\d+$')]
    [string] $BuildVersion,

    [string] $RepositoryRoot
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Write-Utf8File {
    param(
        [Parameter(Mandatory = $true)]
        [string] $Path,

        [Parameter(Mandatory = $true)]
        [string] $Content
    )

    $encoding = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($Path, $Content, $encoding)
}

function Get-NormalizedPath {
    param(
        [Parameter(Mandatory = $true)]
        [string] $Root,

        [Parameter(Mandatory = $true)]
        [string] $RelativePath
    )

    $platformPath = $RelativePath -replace '[\\/]', [string][System.IO.Path]::DirectorySeparatorChar
    return Join-Path $Root $platformPath
}

function Assert-PackageFile {
    param(
        [Parameter(Mandatory = $true)]
        [string] $RelativePath
    )

    $path = Get-NormalizedPath -Root $OutputPath -RelativePath $RelativePath
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "EV2 package reference '$RelativePath' does not exist."
    }
}

function Invoke-AzBicep {
    param(
        [Parameter(Mandatory = $true)]
        [string[]] $Arguments
    )

    & $script:AzCommand @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Azure CLI failed with exit code ${LASTEXITCODE}: az $($Arguments -join ' ')"
    }
}

if ([string]::IsNullOrWhiteSpace($RepositoryRoot)) {
    $RepositoryRoot = Split-Path (Split-Path (Split-Path $PSScriptRoot -Parent) -Parent) -Parent
}

$RepositoryRoot = [System.IO.Path]::GetFullPath($RepositoryRoot)
$OutputPath = [System.IO.Path]::GetFullPath($OutputPath)

if ($OutputPath -eq $RepositoryRoot) {
    throw 'The EV2 output path cannot be the repository root.'
}

if (Test-Path -LiteralPath $OutputPath) {
    throw "The EV2 output path already exists: $OutputPath"
}

$serviceGroupSource = Join-Path $PSScriptRoot 'ServiceGroupRoot'
$bicepSource = Join-Path (Join-Path $RepositoryRoot 'infra') 'telemetry'
$releasePipeline = Join-Path $PSScriptRoot 'release.official.yml'

$requiredSources = @(
    (Join-Path $bicepSource 'main.bicep'),
    (Join-Path $bicepSource 'prod.bicepparam'),
    (Join-Path $serviceGroupSource 'ServiceSpec.json'),
    (Join-Path $serviceGroupSource 'ServiceModel.json'),
    (Join-Path $serviceGroupSource 'ScopeBindings.json'),
    (Join-Path $serviceGroupSource 'RolloutSpec.json'),
    $releasePipeline
)

foreach ($source in $requiredSources) {
    if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
        throw "Required EV2 source file does not exist: $source"
    }
}

$script:AzCommand = (Get-Command az -ErrorAction Stop).Source

New-Item -ItemType Directory -Path $OutputPath | Out-Null
$templatesPath = New-Item -ItemType Directory -Path (Join-Path $OutputPath 'Templates')
$parametersPath = New-Item -ItemType Directory -Path (Join-Path $OutputPath 'Parameters')

foreach ($descriptor in @('ServiceSpec.json', 'ServiceModel.json', 'ScopeBindings.json', 'RolloutSpec.json')) {
    Copy-Item -LiteralPath (Join-Path $serviceGroupSource $descriptor) -Destination (Join-Path $OutputPath $descriptor)
}

$templateOutput = Join-Path $templatesPath.FullName 'Telemetry.json'
$parametersOutput = Join-Path $parametersPath.FullName 'Telemetry.parameters.json'

Invoke-AzBicep -Arguments @(
    'bicep',
    'build',
    '--file',
    (Join-Path $bicepSource 'main.bicep'),
    '--outfile',
    $templateOutput
)

Invoke-AzBicep -Arguments @(
    'bicep',
    'build-params',
    '--file',
    (Join-Path $bicepSource 'prod.bicepparam'),
    '--outfile',
    $parametersOutput
)

$parameters = Get-Content -LiteralPath $parametersOutput -Raw | ConvertFrom-Json
if ($null -eq $parameters.parameters.location) {
    throw "The compiled production parameters do not define 'location'."
}

if ($parameters.parameters.location.value -ne 'eastus') {
    throw "The production Bicep location must remain 'eastus' for this rollout."
}

$parameters.parameters.location.value = '__LOCATION__'
$parameterJson = $parameters | ConvertTo-Json -Depth 100
Write-Utf8File -Path $parametersOutput -Content ($parameterJson + "`n")
Write-Utf8File -Path (Join-Path $OutputPath 'numeric.fileversion.info') -Content ($BuildVersion + "`n")

$jsonFiles = Get-ChildItem -LiteralPath $OutputPath -Recurse -File -Filter '*.json'
foreach ($jsonFile in $jsonFiles) {
    try {
        $null = Get-Content -LiteralPath $jsonFile.FullName -Raw | ConvertFrom-Json
    }
    catch {
        throw "Generated JSON is invalid: $($jsonFile.FullName). $($_.Exception.Message)"
    }
}

$rollout = Get-Content -LiteralPath (Join-Path $OutputPath 'RolloutSpec.json') -Raw | ConvertFrom-Json
$serviceModel = Get-Content -LiteralPath (Join-Path $OutputPath 'ServiceModel.json') -Raw | ConvertFrom-Json
$scopeBindings = Get-Content -LiteralPath (Join-Path $OutputPath 'ScopeBindings.json') -Raw | ConvertFrom-Json
$serviceSpec = Get-Content -LiteralPath (Join-Path $OutputPath 'ServiceSpec.json') -Raw | ConvertFrom-Json
$compiledParameters = Get-Content -LiteralPath $parametersOutput -Raw | ConvertFrom-Json

Assert-PackageFile -RelativePath $rollout.rolloutMetadata.serviceModelPath
Assert-PackageFile -RelativePath $rollout.rolloutMetadata.scopeBindingsPath
Assert-PackageFile -RelativePath $rollout.rolloutMetadata.buildSource.parameters.versionFile
Assert-PackageFile -RelativePath $serviceModel.serviceMetadata.serviceSpecificationPath

$resourceDefinitions = @(
    $serviceModel.serviceResourceGroupDefinitions |
        ForEach-Object { $_.serviceResourceDefinitions } |
        ForEach-Object {
            Assert-PackageFile -RelativePath $_.composedOf.arm.templatePath
            Assert-PackageFile -RelativePath $_.composedOf.arm.parametersPath
            $_.name
        }
)

$rolloutTargets = @(
    $rollout.orchestratedSteps |
        Where-Object { $_.targetType -eq 'ServiceResourceDefinition' } |
        ForEach-Object { $_.targetName }
)

if ($resourceDefinitions.Count -ne @($resourceDefinitions | Sort-Object -Unique).Count) {
    throw 'Service resource definition names must be unique.'
}

if ($rolloutTargets.Count -ne @($rolloutTargets | Sort-Object -Unique).Count) {
    throw 'Rollout target names must be unique.'
}

$definitionDifference = Compare-Object `
    -ReferenceObject ($resourceDefinitions | Sort-Object) `
    -DifferenceObject ($rolloutTargets | Sort-Object)
if ($null -ne $definitionDifference) {
    throw "Every service resource definition must have exactly one rollout step: $($definitionDifference | Out-String)"
}

if ($serviceSpec.identifier -ne $serviceModel.serviceMetadata.serviceIdentifier) {
    throw 'The ServiceTree identifier differs between ServiceSpec.json and ServiceModel.json.'
}

if ($compiledParameters.parameters.location.value -ne '__LOCATION__') {
    throw "The generated location parameter is not scope-bound to '__LOCATION__'."
}

$telemetryBindings = @($scopeBindings.scopeBindings | Where-Object { $_.scopeTagName -eq 'Telemetry' })
if ($telemetryBindings.Count -ne 1) {
    throw "ScopeBindings.json must contain exactly one 'Telemetry' scope binding."
}

$locationBindings = @(
    $telemetryBindings[0].bindings |
        Where-Object { $_.find -eq '__LOCATION__' -and $_.replaceWith -eq '$location()' }
)
if ($locationBindings.Count -ne 1) {
    throw "The Telemetry scope must bind '__LOCATION__' to '`$location()'."
}

if ($serviceModel.serviceResourceGroupDefinitions[0].azureResourceGroupName -notmatch '\$location\(\)') {
    throw 'The telemetry resource-group name must be derived from the selected EV2 region.'
}

$unexpectedTokens = @(
    Get-ChildItem -LiteralPath $OutputPath -Recurse -File |
        ForEach-Object { [regex]::Matches((Get-Content -LiteralPath $_.FullName -Raw), '__[A-Z0-9_.-]+__') } |
        ForEach-Object { $_.Value } |
        Where-Object { $_ -ne '__LOCATION__' } |
        Sort-Object -Unique
)
if ($unexpectedTokens.Count -gt 0) {
    throw "Unexpected generation tokens remain in the package: $($unexpectedTokens -join ', ')"
}

$releaseYaml = Get-Content -LiteralPath $releasePipeline -Raw
$releaseRequirements = @(
    "TaskAction: RegisterAndRollout",
    "EndpointProviderType: ApprovalService",
    "ApprovalServiceEnvironment: Production",
    "StageMapName: 'Microsoft.Azure.SDP.Standard'",
    "Select: 'regions(eastus)'",
    "RolloutSpec.json"
)
foreach ($requirement in $releaseRequirements) {
    if (-not $releaseYaml.Contains($requirement)) {
        throw "The production rollout pipeline is missing required configuration: $requirement"
    }
}

Write-Host "Created and validated deterministic EV2 package at '$OutputPath'."
