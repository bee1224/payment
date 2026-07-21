[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)] [string] $SourceContainer,
    [Parameter(Mandatory = $true)] [string] $SourceDatabase,
    [string] $BackupDirectory = (Join-Path (Get-Location) 'output\drills'),
    [string] $DrillContainer = 'payment-restore-drill-db',
    [string] $MariaDBImage = 'mariadb:10.11'
)

$ErrorActionPreference = 'Stop'
if ($SourceContainer -eq $DrillContainer) { throw 'source and drill targets must differ' }
if ($DrillContainer -notmatch '(^|-)restore-drill-db$') { throw 'drill target must end in restore-drill-db' }
if ($SourceDatabase -notmatch '^[A-Za-z0-9_]+$') { throw 'database name may contain only letters, numbers, and underscore' }
if ([string]::IsNullOrWhiteSpace($env:MYSQL_ROOT_PASSWORD)) { throw 'MYSQL_ROOT_PASSWORD must be provided through the environment' }

New-Item -ItemType Directory -Force -Path $BackupDirectory | Out-Null
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$backup = Join-Path $BackupDirectory ("{0}-{1}.sql" -f $SourceDatabase, $stamp)
& docker inspect $SourceContainer | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'source database container was not found' }

# A consistent InnoDB backup does not lock payment writes.
& docker exec -e "MYSQL_PWD=$env:MYSQL_ROOT_PASSWORD" $SourceContainer mariadb-dump --single-transaction --routines --events --triggers --databases $SourceDatabase | Out-File -LiteralPath $backup -Encoding utf8
if ($LASTEXITCODE -ne 0 -or (Get-Item -LiteralPath $backup).Length -lt 1kb) { throw 'backup failed or was empty' }
$hash = (Get-FileHash -LiteralPath $backup -Algorithm SHA256).Hash
& docker run --rm -d --name $DrillContainer -e "MARIADB_ROOT_PASSWORD=$env:MYSQL_ROOT_PASSWORD" $MariaDBImage --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci | Out-Null
try {
    $deadline = (Get-Date).AddSeconds(90)
    do {
        & docker exec -e "MYSQL_PWD=$env:MYSQL_ROOT_PASSWORD" $DrillContainer mariadb-admin ping -h 127.0.0.1 -uroot --silent | Out-Null
        if ($LASTEXITCODE -eq 0) { break }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    if ($LASTEXITCODE -ne 0) { throw 'drill database did not become ready' }
    Get-Content -LiteralPath $backup -Raw | & docker exec -i -e "MYSQL_PWD=$env:MYSQL_ROOT_PASSWORD" $DrillContainer mariadb -uroot
    if ($LASTEXITCODE -ne 0) { throw 'restore failed' }
    $sourceTables = & docker exec -e "MYSQL_PWD=$env:MYSQL_ROOT_PASSWORD" $SourceContainer mariadb -N -uroot -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$SourceDatabase'"
    $drillTables = & docker exec -e "MYSQL_PWD=$env:MYSQL_ROOT_PASSWORD" $DrillContainer mariadb -N -uroot -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$SourceDatabase'"
    if ($sourceTables -ne $drillTables -or [int]$drillTables -eq 0) { throw 'restored table count differs from source' }
    [pscustomobject]@{ Backup = $backup; SHA256 = $hash; SourceTables = $sourceTables; RestoredTables = $drillTables; Result = 'passed' } | ConvertTo-Json
} finally {
    & docker rm -f $DrillContainer 2>$null | Out-Null
}
