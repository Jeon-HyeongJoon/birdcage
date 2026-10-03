$ErrorActionPreference = "Stop"

if (Get-Process -Name "Adobe Premiere Pro" -ErrorAction SilentlyContinue) {
    throw "Close Adobe Premiere Pro before removing Birdcage."
}

$target = Join-Path $env:APPDATA "Adobe\CEP\extensions\com.birdcage.panel"
if (-not (Test-Path $target)) {
    Write-Host "Birdcage is not installed."
    exit 0
}

$archiveRoot = Join-Path $env:LOCALAPPDATA "Birdcage\uninstalled"
New-Item -ItemType Directory -Path $archiveRoot -Force | Out-Null
$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$archived = Join-Path $archiveRoot ("com.birdcage.panel-" + $stamp)
Move-Item -LiteralPath $target -Destination $archived

Write-Host "Birdcage was removed from the CEP extensions directory."
Write-Host "A recoverable copy was moved to:"
Write-Host $archived
Write-Host "PlayerDebugMode and downloaded media were left unchanged."
