param([switch]$VerifyOnly)

$ErrorActionPreference = "Stop"

$packageRoot = $PSScriptRoot
$source = Join-Path $packageRoot "com.birdcage.panel"
$manifest = Join-Path $packageRoot "SHA256SUMS.txt"

if (-not (Test-Path (Join-Path $source "CSXS\manifest.xml") -PathType Leaf)) {
    throw "Invalid package: CEP manifest is missing."
}
if (-not (Test-Path $manifest -PathType Leaf)) {
    throw "Invalid package: SHA256SUMS.txt is missing."
}

Write-Host "Verifying package files..."
foreach ($line in Get-Content -LiteralPath $manifest) {
    if ($line -notmatch '^([0-9a-fA-F]{64})  (.+)$') {
        throw "Invalid checksum line: $line"
    }
    $expected = $Matches[1].ToLowerInvariant()
    $relative = $Matches[2].Replace('/', '\')
    $file = Join-Path $packageRoot $relative
    if (-not (Test-Path $file -PathType Leaf)) {
        throw "Package file is missing: $relative"
    }
    $actual = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "Checksum mismatch: $relative"
    }
}

if ($VerifyOnly) {
    Write-Host "Package checksum verification succeeded."
    exit 0
}

$extensionRoot = Join-Path $env:APPDATA "Adobe\CEP\extensions"
$target = Join-Path $extensionRoot "com.birdcage.panel"
if (Get-Process -Name "Adobe Premiere Pro" -ErrorAction SilentlyContinue) {
    throw "Close Adobe Premiere Pro before installing Birdcage."
}

New-Item -ItemType Directory -Path $extensionRoot -Force | Out-Null
$staging = Join-Path $extensionRoot ("com.birdcage.panel.installing-" + $PID)
if (Test-Path $staging) {
    Remove-Item -LiteralPath $staging -Recurse -Force
}

$backup = $null
try {
    Copy-Item -LiteralPath $source -Destination $staging -Recurse -Force
    Get-ChildItem -LiteralPath $staging -Recurse -File | Unblock-File -ErrorAction SilentlyContinue

    if (Test-Path $target) {
        $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
        $backup = Join-Path $extensionRoot ("com.birdcage.panel.backup-" + $stamp)
        Move-Item -LiteralPath $target -Destination $backup
    }
    Move-Item -LiteralPath $staging -Destination $target
} catch {
    if ((-not (Test-Path $target)) -and $backup -and (Test-Path $backup)) {
        Move-Item -LiteralPath $backup -Destination $target
    }
    throw
}

foreach ($version in 10, 11, 12) {
    $key = "HKCU:\Software\Adobe\CSXS.$version"
    New-Item -Path $key -Force | Out-Null
    New-ItemProperty -Path $key -Name PlayerDebugMode -Value "1" -PropertyType String -Force | Out-Null
}

Write-Host ""
Write-Host "Birdcage was installed to:"
Write-Host $target
if ($backup) {
    Write-Host "Previous installation was preserved at:"
    Write-Host $backup
}
Write-Host "Start Premiere Pro, then open Window > Extensions > Birdcage."
