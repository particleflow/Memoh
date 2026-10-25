$ErrorActionPreference = 'Stop'
# Run from the reviewed extracted bundle. No source build, plugin installation,
# production state, credentials, ACL repair, firewall or service changes.
$bundle = $PSScriptRoot
$manifest = Get-Content -LiteralPath (Join-Path $bundle 'SHA256SUMS.json') -Raw | ConvertFrom-Json
$expected = @('transferqueue.exe', 'transferqueue.test.exe', 'transferqueue-cli.test.exe', 'herdr-plugin.toml', 'verify-fixtures.ps1', 'README.md', 'transfer.example.json')
$names = @($manifest.files | ForEach-Object { $_.name } | Sort-Object -Unique)
if ($manifest.files.Count -ne 7 -or $names.Count -ne 7 -or (Compare-Object $expected $names)) {
    throw 'Incomplete or duplicated bundle manifest'
}
if ($manifest.source_commit -notmatch '^[0-9a-f]{40}$') { throw 'Invalid source commit' }
foreach ($entry in $manifest.files) {
    if ($entry.name -notin $expected) {
        throw 'Unexpected bundle manifest entry'
    }
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $bundle $entry.name)).Hash.ToLowerInvariant()
    if ($actual -ne $entry.sha256) { throw "Bundle checksum mismatch: $($entry.name)" }
}
# The test executable creates private directories ONLY under its own temporary
# fixtures. It does not consume HERDR_PLUGIN_STATE_DIR or remote endpoints.
# Match the source CI split: keep every native case, including every journal
# durability assertion, without sharing its timeout with publication/catalog I/O.
& (Join-Path $bundle 'transferqueue.test.exe') '-test.run=^TestWindowsNativeActiveJournal$' '-test.v' '-test.timeout=5m'
if ($LASTEXITCODE -ne 0) { throw "Native journal fixture validation failed: $LASTEXITCODE" }
& (Join-Path $bundle 'transferqueue.test.exe') '-test.run=^TestWindowsNative' '-test.skip=^TestWindowsNativeActiveJournal$' '-test.v' '-test.timeout=10m'
if ($LASTEXITCODE -ne 0) { throw "Remaining native fixture validation failed: $LASTEXITCODE" }

# The CLI test binary invokes its real command dispatcher in fresh processes,
# with an ephemeral TLS root supplied only by test code. Production has no bypass.
& (Join-Path $bundle 'transferqueue-cli.test.exe') '-test.run=^TestCLIPath' '-test.v' '-test.timeout=5m'
if ($LASTEXITCODE -ne 0) { throw "CLI fixture validation failed: $LASTEXITCODE" }
