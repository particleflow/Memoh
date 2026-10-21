# Optional Windows-only acceptance; synthetic values, no real gateway request.
# Copies the fixed reader unchanged into an owned temporary fixture directory.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ($env:OS -ne 'Windows_NT' -or $PSVersionTable.PSVersion.Major -ne 5 -or $PSVersionTable.PSVersion.Minor -ne 1) {
    throw 'DPAPI acceptance requires Windows PowerShell 5.1.'
}
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('pi-dpapi-test-' + [Guid]::NewGuid().ToString('N'))
$expected = 'SYNTHETIC_PI_DPAPI_42'
try {
    [void][IO.Directory]::CreateDirectory((Join-Path $fixture 'private'))
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'read-dpapi.ps1') -Destination $fixture
    $cipherPath = Join-Path $fixture 'private\magpie.dpapi'
    foreach ($case in @('valid', 'missing', 'corrupt', 'empty-file', 'whitespace-token')) {
        if (Test-Path -LiteralPath $cipherPath) { Remove-Item -LiteralPath $cipherPath }
        if ($case -eq 'valid') {
            $secure = ConvertTo-SecureString -String $expected -AsPlainText -Force
            try { $cipher = ConvertFrom-SecureString -SecureString $secure }
            finally { $secure.Dispose() }
            [IO.File]::WriteAllText($cipherPath, $cipher)
        }
        if ($case -eq 'corrupt') { [IO.File]::WriteAllText($cipherPath, 'not-a-dpapi-value') }
        if ($case -eq 'empty-file') { [IO.File]::WriteAllText($cipherPath, '') }
        if ($case -eq 'whitespace-token') {
            $secure = ConvertTo-SecureString -String ' ' -AsPlainText -Force
            try { $cipher = ConvertFrom-SecureString -SecureString $secure }
            finally { $secure.Dispose() }
            [IO.File]::WriteAllText($cipherPath, $cipher)
        }
        $start = New-Object Diagnostics.ProcessStartInfo
        $start.FileName = Join-Path $PSHOME 'powershell.exe'
        $start.Arguments = '-NoLogo -NoProfile -NonInteractive -File "' + (Join-Path $fixture 'read-dpapi.ps1') + '"'
        $start.UseShellExecute = $false
        $start.CreateNoWindow = $true
        $start.RedirectStandardOutput = $true
        $start.RedirectStandardError = $true
        $process = [Diagnostics.Process]::Start($start)
        try {
            $output = $process.StandardOutput.ReadToEnd()
            $diagnostic = $process.StandardError.ReadToEnd()
            $process.WaitForExit()
            if ($case -eq 'valid') {
                if ($process.ExitCode -ne 0 -or $output -cne $expected -or $diagnostic.Length -ne 0) {
                    throw 'Synthetic success contract failed.'
                }
            }
            elseif ($process.ExitCode -eq 0 -or $output.Length -ne 0 -or $diagnostic.Trim() -cne 'Pi credential unavailable.') {
                throw 'Synthetic failure contract failed.'
            }
        }
        finally { $process.Dispose() }
        Write-Output ('PASS DPAPI ' + $case)
    }
}
finally {
    if (Test-Path -LiteralPath $fixture) { Remove-Item -LiteralPath $fixture -Recurse -Force }
}
