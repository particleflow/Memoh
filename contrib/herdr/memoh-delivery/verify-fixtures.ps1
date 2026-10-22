$ErrorActionPreference = 'Stop'
# Run from the reviewed extracted bundle. No source build, plugin installation,
# production state, credentials, ACL repair, firewall or service changes.
$bundle = $PSScriptRoot
$manifest = Get-Content -LiteralPath (Join-Path $bundle 'SHA256SUMS.json') -Raw | ConvertFrom-Json
$expected = @('transferqueue.exe', 'transferqueue.test.exe', 'herdr-plugin.toml', 'verify-fixtures.ps1', 'README.md', 'transfer.example.json')
$names = @($manifest.files | ForEach-Object { $_.name } | Sort-Object -Unique)
if ($manifest.files.Count -ne 6 -or $names.Count -ne 6 -or (Compare-Object $expected $names)) {
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
& (Join-Path $bundle 'transferqueue.test.exe') '-test.run=^TestWindowsNative' '-test.v' '-test.timeout=5m'
if ($LASTEXITCODE -ne 0) { throw "Native fixture validation failed: $LASTEXITCODE" }
