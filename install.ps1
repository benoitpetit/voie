[CmdletBinding()]
param(
    [string]$Version = 'latest',
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\voie')
)

$ErrorActionPreference = 'Stop'
$Repository = 'benoitpetit/voie'
$ReleaseBase = "https://github.com/$Repository/releases"
$TempDir = $null

function Get-ReleaseAssetName([string]$Checksums, [string]$RequestedVersion) {
    if ($RequestedVersion -eq 'latest') {
        $matches = [regex]::Matches($Checksums, '(?m)^([0-9a-fA-F]{64})\s+\*?(voie_(\d+\.\d+\.\d+)_windows_amd64\.zip)\s*$')
        if ($matches.Count -ne 1) {
            throw "Expected exactly one Windows amd64 archive in checksums.txt; found $($matches.Count)."
        }
        return @{ Name = $matches[0].Groups[2].Value; Version = $matches[0].Groups[3].Value; Hash = $matches[0].Groups[1].Value }
    }

    $normalized = $RequestedVersion -replace '^v', ''
    if ($normalized -notmatch '^\d+\.\d+\.\d+$') {
        throw "Invalid version '$RequestedVersion'; expected x.y.z."
    }
    $name = "voie_${normalized}_windows_amd64.zip"
    $matches = [regex]::Matches($Checksums, "(?m)^([0-9a-fA-F]{64})\s+\*?$([regex]::Escape($name))\s*$")
    if ($matches.Count -ne 1) {
        throw "Expected exactly one checksum for $name; found $($matches.Count)."
    }
    return @{ Name = $name; Version = $normalized; Hash = $matches[0].Groups[1].Value }
}

try {
    if ($env:OS -ne 'Windows_NT') {
        throw 'This installer supports Windows only.'
    }
    if (-not [Environment]::Is64BitOperatingSystem -or $env:PROCESSOR_ARCHITECTURE -notin @('AMD64', 'ARM64')) {
        throw "Unsupported architecture '$env:PROCESSOR_ARCHITECTURE'; supported: Windows amd64."
    }
    if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') {
        throw 'Windows ARM64 releases are not available yet; supported: Windows amd64.'
    }

    $TempDir = Join-Path ([IO.Path]::GetTempPath()) ("voie-install-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $TempDir | Out-Null

    if ($Version -eq 'latest') {
        $checksumUri = "$ReleaseBase/latest/download/checksums.txt"
    }
    else {
        $normalized = $Version -replace '^v', ''
        if ($normalized -notmatch '^\d+\.\d+\.\d+$') {
            throw "Invalid version '$Version'; expected x.y.z."
        }
        $checksumUri = "$ReleaseBase/download/v$normalized/checksums.txt"
    }
    Invoke-WebRequest -Uri $checksumUri -OutFile (Join-Path $TempDir 'checksums.txt')
    $checksums = [IO.File]::ReadAllText((Join-Path $TempDir 'checksums.txt'))
    $asset = Get-ReleaseAssetName $checksums $Version
    $archivePath = Join-Path $TempDir $asset.Name
    Invoke-WebRequest -Uri "$ReleaseBase/download/v$($asset.Version)/$($asset.Name)" -OutFile $archivePath

    $actualHash = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash
    if ($actualHash -ine $asset.Hash) {
        throw "SHA-256 checksum mismatch for $($asset.Name)."
    }

    $extractDir = Join-Path $TempDir 'extracted'
    Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir
    $binary = Join-Path $extractDir 'voie.exe'
    if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) {
        throw 'The release archive does not contain voie.exe.'
    }
    $item = Get-Item -LiteralPath $binary -Force
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'The release archive contains an invalid voie.exe entry.'
    }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $destination = Join-Path $InstallDir 'voie.exe'
    $staged = Join-Path $InstallDir ('.voie-' + [guid]::NewGuid().ToString('N') + '.exe')
    Copy-Item -LiteralPath $binary -Destination $staged
    try {
        if (Test-Path -LiteralPath $destination) {
            [IO.File]::Replace($staged, $destination, $null, $true)
        }
        else {
            [IO.File]::Move($staged, $destination)
        }
    }
    finally {
        if (Test-Path -LiteralPath $staged) { Remove-Item -LiteralPath $staged -Force }
    }
    Write-Output "Installed voie $($asset.Version) at $destination"
}
catch {
    [Console]::Error.WriteLine("install.ps1: $($_.Exception.Message)")
    exit 1
}
finally {
    if ($TempDir -and (Test-Path -LiteralPath $TempDir)) {
        Remove-Item -LiteralPath $TempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
