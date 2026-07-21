[CmdletBinding()]
param([string] $ProjectName = 'payment-drill')

$ErrorActionPreference = 'Stop'
if ($ProjectName -notmatch 'drill') { throw 'ProjectName must contain drill' }
if (-not (Test-Path '.env.drill')) { throw 'Create an isolated .env.drill first' }
& docker compose -p $ProjectName --env-file .env.drill -f deploy/drill/compose.multi-node.yaml up -d --build
if ($LASTEXITCODE -ne 0) { throw 'unable to start multi-node drill stack' }
try {
    foreach ($node in @('api-1', 'api-2')) {
        $deadline = (Get-Date).AddMinutes(3)
        do {
            & docker compose -p $ProjectName --env-file .env.drill -f deploy/drill/compose.multi-node.yaml exec -T $node wget -q -O - http://127.0.0.1:8080/health | Out-Null
            if ($LASTEXITCODE -eq 0) { break }
            Start-Sleep 2
        } while ((Get-Date) -lt $deadline)
        if ($LASTEXITCODE -ne 0) { throw "$node did not become healthy" }
    }
    $deadline = (Get-Date).AddMinutes(3)
    do { try { $health = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:18080/health -TimeoutSec 3; if ($health.StatusCode -eq 200) { break } } catch {}; Start-Sleep 2 } while ((Get-Date) -lt $deadline)
    if ($health.StatusCode -ne 200) { throw 'load-balanced health check did not pass' }
    $victim = (& docker compose -p $ProjectName --env-file .env.drill -f deploy/drill/compose.multi-node.yaml ps -q api-1 | Select-Object -First 1)
    if ([string]::IsNullOrWhiteSpace($victim)) { throw 'no API node available for fault injection' }
    & docker stop $victim | Out-Null
    $deadline = (Get-Date).AddSeconds(30)
    do {
        try { $health = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:18080/health -TimeoutSec 3; if ($health.StatusCode -eq 200) { break } } catch {}
        Start-Sleep 2
    } while ((Get-Date) -lt $deadline)
    if ($null -eq $health -or $health.StatusCode -ne 200) { throw 'service did not remain healthy after one node stopped' }
    [pscustomobject]@{ Project = $ProjectName; StoppedNode = $victim; HealthAfterFailure = $health.StatusCode; Result = 'passed' } | ConvertTo-Json
} finally {
    & docker compose -p $ProjectName --env-file .env.drill -f deploy/drill/compose.multi-node.yaml down -v --remove-orphans
}
