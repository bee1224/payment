[CmdletBinding()]
param(
    [ValidateSet('config', 'up', 'down', 'logs', 'health', 'rollback')]
    [string] $Action = 'config',
    [string] $EnvFile = '.env.sandbox'
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $EnvFile)) { throw "Sandbox env file not found: $EnvFile" }
$compose = @('compose', '--env-file', $EnvFile)

switch ($Action) {
    'config' { & docker @compose config }
    'up' { & docker @compose up -d --build }
    'down' { & docker @compose down }
    'logs' { & docker @compose logs --tail 200 payment-api }
    'health' { Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8081/health | Select-Object StatusCode, Content }
    'rollback' { & docker @compose down; Write-Output 'Sandbox stopped. Restore only a sandbox backup into the sandbox DB.' }
}
