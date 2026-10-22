$ErrorActionPreference = 'Stop'
# Run from the reviewed extracted bundle. No source build, plugin installation,
# production state, credentials, ACL repair, firewall or service changes.
$bundle = $PSScriptRoot
$manifest = Get-Content -LiteralPath (Join-Path $bundle 'SHA256SUMS.json') -Raw | ConvertFrom-Json
foreach ($entry in $manifest.files) {
    if ($entry.name -notin @('transferqueue.exe', 'transferqueue.test.exe', 'herdr-plugin.toml', 'verify-fixtures.ps1', 'README.md')) {
        throw 'Unexpected bundle manifest entry'
    }
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $bundle $entry.name)).Hash.ToLowerInvariant()
    if ($actual -ne $entry.sha256) { throw "Bundle checksum mismatch: $($entry.name)" }
}
# The test executable creates private directories ONLY under its own temporary
# fixtures. It does not consume HERDR_PLUGIN_STATE_DIR or remote endpoints.
& (Join-Path $bundle 'transferqueue.test.exe') '-test.run=^TestWindowsNative' '-test.v' '-test.timeout=5m'
if ($LASTEXITCODE -ne 0) { throw "Native fixture validation failed: $LASTEXITCODE" }
