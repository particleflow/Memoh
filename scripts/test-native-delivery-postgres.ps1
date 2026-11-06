param(
    [string]$TestPattern = '^TestWindowsNativePostgresDelivery$',
    [string]$TestTimeout = '90s'
)
$ErrorActionPreference = 'Stop'
# Use only the runner's installed binaries and a new disposable cluster. Never
# start/reconfigure its installed service, change a firewall, or contact a NAS.
$fixtureBin = $env:PGBIN
if (-not $fixtureBin -or -not (Test-Path -LiteralPath (Join-Path $fixtureBin 'initdb.exe'))) {
    throw 'Installed PostgreSQL binaries unavailable; no installation fallback'
}
& (Join-Path $fixtureBin 'postgres.exe') '--version'
if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL binary probe failed' }
$fixtureRoot = Join-Path $env:RUNNER_TEMP ('memoh-native-pg-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $fixtureRoot | Out-Null
$fixtureData = Join-Path $fixtureRoot 'data'
$fixturePassword = Join-Path $fixtureRoot 'password.txt'
$fixtureLog = Join-Path $fixtureRoot 'postgres.log'
[IO.File]::WriteAllText($fixturePassword, 'isolated-native-postgres-fixture', [Text.UTF8Encoding]::new($false))
$probe = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$probe.Start()
$fixturePort = $probe.LocalEndpoint.Port
$probe.Stop()
$started = $false
$oldDSN = $env:TEST_POSTGRES_DSN
try {
    & (Join-Path $fixtureBin 'initdb.exe') '-D' $fixtureData '-U' 'fixture_admin' '--auth=scram-sha-256' "--pwfile=$fixturePassword" '--no-locale' '--encoding=UTF8'
    if ($LASTEXITCODE -ne 0) { throw 'Disposable cluster initialization failed' }
    & (Join-Path $fixtureBin 'pg_ctl.exe') 'start' '-D' $fixtureData '-w' '-t' '30' '-l' $fixtureLog '-o' "-h 127.0.0.1 -p $fixturePort"
    if ($LASTEXITCODE -ne 0) { throw 'Disposable loopback PostgreSQL start failed' }
    $started = $true
    $socket = [Net.Sockets.TcpClient]::new()
    try { $socket.Connect([Net.IPAddress]::Loopback, $fixturePort) } finally { $socket.Dispose() }
    Write-Host 'Loopback TCP probe passed; running actual NTFS + async handler + PostgreSQL test'
    # Database transport is confined to this disposable loopback cluster. File
    # transfer itself uses httptest TLS with its verified fixture CA, not -k.
    $env:TEST_POSTGRES_DSN = "postgres://fixture_admin:isolated-native-postgres-fixture@127.0.0.1:$fixturePort/postgres?sslmode=disable"
    go test -v -tags=integration -count=1 "-timeout=$TestTimeout" "-run=$TestPattern" ./internal/db
    if ($LASTEXITCODE -ne 0) { throw 'Native PostgreSQL combination failed' }
} finally {
    $env:TEST_POSTGRES_DSN = $oldDSN
    if ($started) {
        & (Join-Path $fixtureBin 'pg_ctl.exe') 'stop' '-D' $fixtureData '-m' 'immediate' '-w' '-t' '30'
        if ($LASTEXITCODE -ne 0) { throw 'Disposable PostgreSQL cleanup failed; inspect runner' }
    }
    Remove-Item -LiteralPath $fixtureRoot -Recurse -Force
}
