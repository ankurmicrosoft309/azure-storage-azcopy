param(
    [string]$OutputPath = (Join-Path $PSScriptRoot "azcopy-telemetry-metrics.dashboard.json")
)

$ErrorActionPreference = "Stop"

function New-StableGuid([string]$Seed) {
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        $hash = $sha.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($Seed))
        $bytes = [byte[]]::new(16)
        [Array]::Copy($hash, $bytes, 16)
        return ([Guid]::new($bytes)).ToString()
    } finally {
        $sha.Dispose()
    }
}

function Read-Query([string]$RelativePath) {
    $path = Join-Path $PSScriptRoot $RelativePath
    $text = (Get-Content $path | Where-Object { $_ -notmatch '^\s*//' }) -join "`n"
    $appInsightsTable = "cluster('https://adx.monitor.azure.com/subscriptions/31347be8-d066-464e-9866-7e58d85027b7/resourcegroups/sharankur_playground/providers/microsoft.insights/components/sharankur_insights1').database('sharankur_insights1').customMetrics"
    return ([regex]::Replace($text, '(?m)^customMetrics', $appInsightsTable, 1)).Trim()
}

function New-VisualOptions([string]$VisualType) {
    if ($VisualType -eq "multistat") {
        return [ordered]@{
            multiStat__textSize = "large"
            multiStat__valueColumn = $null
            colorRulesDisabled = $true
            colorStyle = "light"
            multiStat__displayOrientation = "horizontal"
            multiStat__labelColumn = $null
            multiStat__slot = [ordered]@{ width = 1; height = 2 }
            colorRules = @()
        }
    }

    if ($VisualType -eq "table") {
        return [ordered]@{
            table__enableRenderLinks = $true
            colorRulesDisabled = $true
            colorStyle = "light"
            crossFilterDisabled = $false
            drillthroughDisabled = $false
            crossFilter = @()
            drillthrough = @()
            table__renderLinks = @()
            colorRules = @()
        }
    }

    return [ordered]@{
        multipleYAxes = [ordered]@{
            base = [ordered]@{
                id = "-1"
                label = ""
                columns = @()
                yAxisMaximumValue = $null
                yAxisMinimumValue = $null
                yAxisScale = "linear"
                horizontalLines = @()
            }
            additional = @()
            showMultiplePanels = $false
        }
        hideLegend = $false
        legendLocation = "bottom"
        xColumnTitle = ""
        xColumn = $null
        yColumns = $null
        seriesColumns = $null
        xAxisScale = "linear"
        verticalLine = ""
        crossFilterDisabled = $false
        drillthroughDisabled = $false
        crossFilter = @()
        drillthrough = @()
        selectedDataOnLoad = [ordered]@{ all = $true; limit = 20 }
        dataPointsTooltip = [ordered]@{ all = $false; limit = 1 }
    }
}

$prefix = "azcopy-telemetry-metrics"
$dataSourceId = New-StableGuid "${prefix}:data-source:xstore"
$pages = @(
    [ordered]@{ name = "Overview"; id = New-StableGuid "${prefix}:page:overview" },
    [ordered]@{ name = "Data Volume"; id = New-StableGuid "${prefix}:page:data-volume" },
    [ordered]@{ name = "Source Profile"; id = New-StableGuid "${prefix}:page:source-profile" },
    [ordered]@{ name = "Performance"; id = New-StableGuid "${prefix}:page:performance" },
    [ordered]@{ name = "Reliability"; id = New-StableGuid "${prefix}:page:reliability" },
    [ordered]@{ name = "Environment"; id = New-StableGuid "${prefix}:page:environment" }
)
if (@($pages.id | Sort-Object -Unique).Count -ne $pages.Count) {
    throw "Dashboard page IDs must be unique."
}
$pageIds = @{}
foreach ($page in $pages) { $pageIds[$page.name] = $page.id }

$manifest = @(
    @{ Page = "Overview"; Title = "Lifecycle activity"; File = "queries/01_lifecycle_trend.kql"; Visual = "timechart"; X = 0; Y = 0; W = 12; H = 9 },
    @{ Page = "Overview"; Title = "Complete metric catalog"; File = "queries/16_metric_catalog.kql"; Visual = "table"; X = 12; Y = 0; W = 10; H = 18 },
    @{ Page = "Overview"; Title = "Transfer dimension mix"; File = "queries/17_transfer_dimensions.kql"; Visual = "bar"; X = 0; Y = 9; W = 12; H = 9 },

    @{ Page = "Data Volume"; Title = "Bytes"; File = "queries/02_bytes_trend.kql"; Visual = "timechart"; X = 0; Y = 0; W = 22; H = 9 },
    @{ Page = "Data Volume"; Title = "Scheduled object composition"; File = "queries/03_scheduled_objects_trend.kql"; Visual = "timechart"; X = 0; Y = 9; W = 11; H = 9 },
    @{ Page = "Data Volume"; Title = "Object and folder outcomes"; File = "queries/04_object_outcomes_trend.kql"; Visual = "timechart"; X = 11; Y = 9; W = 11; H = 9 },
    @{ Page = "Data Volume"; Title = "Transfer outcomes"; File = "queries/05_transfer_outcomes_trend.kql"; Visual = "timechart"; X = 0; Y = 18; W = 22; H = 9 },

    @{ Page = "Source Profile"; Title = "Scanned and touched inventory"; File = "queries/06_source_inventory.kql"; Visual = "multistat"; X = 0; Y = 0; W = 22; H = 6 },
    @{ Page = "Source Profile"; Title = "Source object sizes"; File = "queries/07_source_object_sizes.kql"; Visual = "table"; X = 0; Y = 6; W = 12; H = 10 },
    @{ Page = "Source Profile"; Title = "Small objects and directory depth"; File = "queries/08_source_shape.kql"; Visual = "table"; X = 12; Y = 6; W = 10; H = 10 },

    @{ Page = "Performance"; Title = "Phase durations"; File = "queries/09_duration_trend.kql"; Visual = "timechart"; X = 0; Y = 0; W = 22; H = 9 },
    @{ Page = "Performance"; Title = "Throughput"; File = "queries/10_throughput_trend.kql"; Visual = "timechart"; X = 0; Y = 9; W = 12; H = 9 },
    @{ Page = "Performance"; Title = "Storage latency and IOPS"; File = "queries/11_storage_performance.kql"; Visual = "table"; X = 12; Y = 9; W = 10; H = 9 },

    @{ Page = "Reliability"; Title = "HTTP, network, and overflow error counts"; File = "queries/12_attempt_counts_trend.kql"; Visual = "timechart"; X = 0; Y = 0; W = 11; H = 9 },
    @{ Page = "Reliability"; Title = "Server busy counts"; File = "queries/13_server_busy_counts_trend.kql"; Visual = "timechart"; X = 11; Y = 0; W = 11; H = 9 },
    @{ Page = "Reliability"; Title = "Error and completion percentages"; File = "queries/14_rates_trend.kql"; Visual = "timechart"; X = 0; Y = 9; W = 11; H = 9 },
    @{ Page = "Reliability"; Title = "Job error distribution"; File = "queries/15_error_distribution.kql"; Visual = "bar"; X = 11; Y = 9; W = 11; H = 9 },

    @{ Page = "Environment"; Title = "Host and runtime environment"; File = "queries/18_host_environment.kql"; Visual = "table"; X = 0; Y = 0; W = 22; H = 10 },
    @{ Page = "Environment"; Title = "Recent finished attempts and dimensions"; File = "queries/19_recent_attempts.kql"; Visual = "table"; X = 0; Y = 10; W = 22; H = 12 }
)

foreach ($item in $manifest) {
    if ($item.W -lt 3 -or $item.H -lt 6) {
        throw "Tile '$($item.Title)' is $($item.W)x$($item.H); ADX requires at least 3x6."
    }
}

foreach ($page in $pages.name) {
    $pageTiles = @($manifest | Where-Object Page -eq $page)
    for ($leftIndex = 0; $leftIndex -lt $pageTiles.Count; $leftIndex++) {
        for ($rightIndex = $leftIndex + 1; $rightIndex -lt $pageTiles.Count; $rightIndex++) {
            $left = $pageTiles[$leftIndex]
            $right = $pageTiles[$rightIndex]
            $overlaps =
                $left.X -lt ($right.X + $right.W) -and
                ($left.X + $left.W) -gt $right.X -and
                $left.Y -lt ($right.Y + $right.H) -and
                ($left.Y + $left.H) -gt $right.Y
            if ($overlaps) {
                throw "Tiles '$($left.Title)' and '$($right.Title)' overlap on page '$page'."
            }
        }
    }
}

$eventSourcePath = Join-Path $PSScriptRoot "../../telemetry/events.go"
$eventSource = Get-Content -Raw $eventSourcePath
$expectedMetrics = @(
    [regex]::Matches($eventSource, '\{Name:\s*"(azcopy\.[^"]+)"') |
        ForEach-Object { $_.Groups[1].Value } |
        Sort-Object -Unique
)
$queryCorpus = (Get-ChildItem (Join-Path $PSScriptRoot "queries") -Filter *.kql | ForEach-Object { Get-Content -Raw $_.FullName }) -join "`n"
$missingMetrics = @($expectedMetrics | Where-Object { $queryCorpus -notmatch [regex]::Escape($_) })
if ($missingMetrics.Count -gt 0) {
    throw "Dashboard queries do not cover emitted metrics: $($missingMetrics -join ', ')"
}

$queries = @()
$tiles = @()
foreach ($item in $manifest) {
    $queryId = New-StableGuid "${prefix}:query:$($item.File)"
    $tileId = New-StableGuid "${prefix}:tile:$($item.File)"
    $queries += [ordered]@{
        dataSource = [ordered]@{ kind = "inline"; dataSourceId = $dataSourceId }
        text = Read-Query $item.File
        id = $queryId
        usedVariables = @("_startTime", "_endTime")
    }
    $tiles += [ordered]@{
        id = $tileId
        title = $item.Title
        visualType = $item.Visual
        pageId = $pageIds[$item.Page]
        layout = [ordered]@{ x = $item.X; y = $item.Y; width = $item.W; height = $item.H }
        queryRef = [ordered]@{ kind = "query"; queryId = $queryId }
        visualOptions = New-VisualOptions $item.Visual
    }
}

$dashboard = [ordered]@{
    '$schema' = "https://dataexplorer.azure.com/static/d/schema/60/dashboard.json"
    id = New-StableGuid "${prefix}:dashboard"
    eTag = New-StableGuid "${prefix}:etag:v1"
    title = "AzCopy Telemetry Metrics"
    schema_version = "60"
    tiles = $tiles
    baseQueries = @()
    parameters = @(
        [ordered]@{
            kind = "duration"
            id = New-StableGuid "${prefix}:parameter:time-range"
            displayName = "Time range"
            description = ""
            beginVariableName = "_startTime"
            endVariableName = "_endTime"
            defaultValue = [ordered]@{ kind = "dynamic"; count = 24; unit = "hours" }
            showOnPages = [ordered]@{ kind = "all" }
        }
    )
    dataSources = @(
        [ordered]@{
            id = $dataSourceId
            kind = "manual-kusto"
            scopeId = "kusto"
            name = "AzCopy Telemetry"
            clusterUri = "https://azcore.centralus.kusto.windows.net/"
            database = "Xstore"
        }
    )
    pages = $pages
    queries = $queries
}

$dashboard | ConvertTo-Json -Depth 30 | Set-Content -Path $OutputPath -Encoding utf8
Write-Host "Generated $OutputPath with $($tiles.Count) tiles across $($pages.Count) pages covering $($expectedMetrics.Count) emitted metrics."