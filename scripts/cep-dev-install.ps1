# Windows: 서명 없는 개발용 CEP 패널을 Premiere에서 열 수 있게 한다.
#  1) HKCU\Software\Adobe\CSXS.10/11/12 PlayerDebugMode=1
#  2) Helper(win x64) 빌드 + 패널 빌드 + %APPDATA%\Adobe\CEP\extensions\com.birdcage.panel → dist\cep 링크(junction)
#     (일반 `npm run build`는 링크를 남기지 않는다 — 링크는 여기서만 만든다)
# 되돌리기: .\scripts\cep-dev-install.ps1 -Uninstall
param([switch]$Uninstall)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

if ($Uninstall) {
  foreach ($v in 10, 11, 12) { Remove-ItemProperty -Path "HKCU:\Software\Adobe\CSXS.$v" -Name PlayerDebugMode -ErrorAction SilentlyContinue }
  Push-Location "$root\panel\cep"; npm run delsymlink; Pop-Location
  Write-Host "PlayerDebugMode 해제, 패널 링크 제거"
  exit 0
}

foreach ($v in 10, 11, 12) {
  New-Item -Path "HKCU:\Software\Adobe\CSXS.$v" -Force | Out-Null
  Set-ItemProperty -Path "HKCU:\Software\Adobe\CSXS.$v" -Name PlayerDebugMode -Value "1"
}
Write-Host "PlayerDebugMode=1 (CSXS 10/11/12)"

$out = "$root\panel\cep\src\bin\win32-x64"
New-Item -ItemType Directory -Force -Path $out | Out-Null
Push-Location "$root\helper"
$env:GOOS = "windows"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
$version = (Get-Content "$root\panel\cep\package.json" -Raw | ConvertFrom-Json).version
go build -trimpath -ldflags "-s -w -X main.version=${version}-dev" -o "$out\birdcage-helper.exe" .\cmd\birdcage-helper
Pop-Location

Push-Location "$root\panel\cep"
npm install --no-audit --no-fund
npm run build
npm run symlink
Pop-Location
Write-Host "완료: Premiere를 재시작한 뒤 [창 > 확장 > Birdcage]"
