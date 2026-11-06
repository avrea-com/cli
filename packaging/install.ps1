# Install the native AVR executable. Download and inspect this script before use.
[CmdletBinding()]
param(
    [string]$Version = '',
    [string]$BinDir = (Join-Path $env:LOCALAPPDATA 'Avrea\bin')
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -ne 'X64' -or $env:OS -ne 'Windows_NT') {
    throw 'This installer supports Windows AMD64 only.'
}
if (-not [IO.Path]::IsPathRooted($BinDir)) { throw 'BinDir must be absolute.' }
$base = if ($env:AVR_RELEASE_BASE_URL) { $env:AVR_RELEASE_BASE_URL.TrimEnd('/') } else { 'https://github.com/avrea-com/cli/releases' }
if ($base -notmatch '^https://' -and $base -notmatch '^http://127\.0\.0\.1:\d+(/|$)') { throw 'Release URL must use HTTPS.' }
$temp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
[IO.Directory]::CreateDirectory($temp) | Out-Null
$staged = $null
try {
    if (-not $Version) {
        Invoke-WebRequest "$base/latest/download/avr-version.txt" -OutFile "$temp\version" -TimeoutSec 120
        $Version = [IO.File]::ReadAllText("$temp\version").Trim()
    }
    if ($Version -notmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?$') { throw 'Invalid version.' }
    $name = "avr_${Version}_windows_amd64"
    $archive = "$name.zip"
    Invoke-WebRequest "$base/download/v$Version/$archive" -OutFile "$temp\$archive" -TimeoutSec 120
    Invoke-WebRequest "$base/download/v$Version/$archive.sha256" -OutFile "$temp\checksum" -TimeoutSec 120
    $digest = (Get-FileHash "$temp\$archive" -Algorithm SHA256).Hash.ToLowerInvariant()
    if ([IO.File]::ReadAllText("$temp\checksum").Trim() -cne "$digest  $archive") { throw 'Checksum mismatch.' }
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead("$temp\$archive")
    try {
        $entries = @($zip.Entries | Where-Object { $_.FullName -ceq "$name/avr.exe" })
        if ($entries.Count -ne 1 -or $entries[0].Length -eq 0) { throw 'Missing, duplicate or empty executable.' }
        # Extract a single entry to a chosen path; archive paths are never used.
        [IO.Compression.ZipFileExtensions]::ExtractToFile($entries[0], "$temp\avr.exe")
    } finally { $zip.Dispose() }
    [IO.Directory]::CreateDirectory($BinDir) | Out-Null
    $dest = Join-Path $BinDir 'avr.exe'
    $marker = Join-Path $BinDir 'avr-install-method'
    foreach ($path in @($dest, $marker)) {
        if ((Test-Path -LiteralPath $path) -and ((Get-Item -Force -LiteralPath $path).Attributes -band [IO.FileAttributes]::ReparsePoint)) {
            throw 'Destination is a link. Use its package manager or choose another directory.'
        }
    }
    if (Test-Path -LiteralPath $dest) {
        $oldDigest = (Get-FileHash -LiteralPath $dest -Algorithm SHA256).Hash.ToLowerInvariant()
        if (-not (Test-Path -LiteralPath $marker) -or [IO.File]::ReadAllText($marker).Trim() -cne "native $oldDigest") {
            throw 'Destination contains an unmanaged or changed avr. Choose another directory.'
        }
    }
    $staged = Join-Path $BinDir ('.avr-' + [Guid]::NewGuid().ToString())
    Copy-Item -LiteralPath "$temp\avr.exe" -Destination $staged
    $newDigest = (Get-FileHash -LiteralPath $staged -Algorithm SHA256).Hash.ToLowerInvariant()
    if (Test-Path -LiteralPath $dest) { [IO.File]::Replace($staged, $dest, $null) } else { [IO.File]::Move($staged, $dest) }
    [IO.File]::WriteAllText($marker, "native $newDigest`n")
    Write-Output "Installed avr $Version to $dest"
    Write-Output "Add this directory to your user PATH if needed: $BinDir"
    $existing = Get-Command avr -ErrorAction SilentlyContinue
    if ($existing -and $existing.Source -ne $dest) { Write-Output "Another avr currently wins on PATH: $($existing.Source)" }
} finally {
    if ($staged -and (Test-Path -LiteralPath $staged)) { Remove-Item -LiteralPath $staged }
    Remove-Item -LiteralPath $temp -Recurse
}
