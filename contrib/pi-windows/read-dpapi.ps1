# Windows PowerShell 5.1. Credential output is only for Pi's private stdout pipe.
# Provision private/magpie.dpapi with ConvertFrom-SecureString WITHOUT a key,
# under the same Windows account that will run Pi. No credential is created here.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$secure = $null
$pointer = [IntPtr]::Zero
$plain = $null
$resultCode = 0
try {
    if ($env:OS -ne 'Windows_NT') { throw 'Unsupported platform' }
    $cipherPath = Join-Path $PSScriptRoot 'private\magpie.dpapi'
    $file = Get-Item -LiteralPath $cipherPath -Force
    if ($file.PSIsContainer -or $file.Length -lt 1 -or $file.Length -gt 65536) {
        throw 'Invalid credential file'
    }
    $encrypted = [IO.File]::ReadAllText($file.FullName).Trim()
    $secure = ConvertTo-SecureString -String $encrypted -ErrorAction Stop
    $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    $plain = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
    # A gateway bearer token must be nonempty printable ASCII, with no whitespace.
    if ($plain.Length -lt 1 -or $plain.Length -gt 8192 -or $plain -match '[^\x21-\x7E]') {
        throw 'Invalid credential'
    }
    [Console]::Out.Write($plain)
}
catch {
    # Never print an exception, path, ciphertext or partially decrypted value.
    [Console]::Error.WriteLine('Pi credential unavailable.')
    $resultCode = 1
}
finally {
    if ($pointer -ne [IntPtr]::Zero) {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer)
    }
    if ($null -ne $secure) { $secure.Dispose() }
    $plain = $null
}
exit $resultCode
