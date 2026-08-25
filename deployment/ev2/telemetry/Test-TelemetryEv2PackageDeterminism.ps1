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

$OutputPath = [System.IO.Path]::GetFullPath($OutputPath)
if (Test-Path -LiteralPath $OutputPath) {
    throw "The EV2 output path already exists: $OutputPath"
}

$firstOutput = "$OutputPath.determinism-a-$PID"
$secondOutput = "$OutputPath.determinism-b-$PID"
$builder = Join-Path $PSScriptRoot 'New-TelemetryEv2Package.ps1'

function Get-PackageManifest {
    param(
        [Parameter(Mandatory = $true)]
        [string] $Root
    )

    $rootWithSeparator = $Root.TrimEnd(
        [System.IO.Path]::DirectorySeparatorChar,
        [System.IO.Path]::AltDirectorySeparatorChar
    ) + [System.IO.Path]::DirectorySeparatorChar

    return @(
        Get-ChildItem -LiteralPath $Root -Recurse -File |
            ForEach-Object {
                [PSCustomObject]@{
                    Path = $_.FullName.Substring($rootWithSeparator.Length)
                    Hash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash
                }
            } |
            Sort-Object Path
    )
}

try {
    $builderParameters = @{
        BuildVersion = $BuildVersion
    }
    if (-not [string]::IsNullOrWhiteSpace($RepositoryRoot)) {
        $builderParameters.RepositoryRoot = $RepositoryRoot
    }

    & $builder -OutputPath $firstOutput @builderParameters
    & $builder -OutputPath $secondOutput @builderParameters

    $firstManifest = Get-PackageManifest -Root $firstOutput
    $secondManifest = Get-PackageManifest -Root $secondOutput
    $difference = Compare-Object `
        -ReferenceObject $firstManifest `
        -DifferenceObject $secondManifest `
        -Property Path, Hash

    if ($null -ne $difference) {
        throw "Repeated EV2 package generation was not deterministic: $($difference | Out-String)"
    }

    Move-Item -LiteralPath $firstOutput -Destination $OutputPath
    Write-Host "Verified deterministic EV2 package generation at '$OutputPath'."
}
finally {
    foreach ($temporaryPath in @($firstOutput, $secondOutput)) {
        if (Test-Path -LiteralPath $temporaryPath) {
            Remove-Item -LiteralPath $temporaryPath -Recurse -Force
        }
    }
}
