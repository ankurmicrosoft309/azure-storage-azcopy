[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
if ($args.Count -gt 0) { throw "Unrecognized arguments: $($args -join ' ')" }
Get-Command go -ErrorAction Stop | Out-Null
$settings = @{
    AZCOPY_RUN_TELEMETRY_CLI_E2E = '1'
    AZCOPY_DISABLE_TELEMETRY = 'true'
}
$previous = @{}
foreach ($name in $settings.Keys) {
    $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
    [Environment]::SetEnvironmentVariable($name, $settings[$name], 'Process')
}
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
    & go test '-tags=telemetrylive' ./azcopy -run '^TestTelemetryCLIEndToEnd$' -count=1 -v -timeout=5m
    if ($LASTEXITCODE -ne 0) { throw 'Local telemetry CLI E2E scenarios failed.' }
} finally {
    Pop-Location
    foreach ($name in $settings.Keys) {
        [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process')
    }
}