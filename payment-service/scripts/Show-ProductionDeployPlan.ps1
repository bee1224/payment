[CmdletBinding()]
param([string] $EnvFile = '.env.production')

$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $EnvFile)) { throw "Production env file not found: $EnvFile" }
Write-Output 'Production deploy is intentionally not executed by this script.'
Write-Output 'Impact: production API and database migration may affect live payments.'
Write-Output "Plan: docker compose --env-file $EnvFile config; backup; migrate via controlled API start; health check; validate callbacks."
Write-Output 'Rollback: restore the approved production image and use a pre-approved database recovery plan; do not run destructive migration down automatically.'
