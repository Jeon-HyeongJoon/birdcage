#!/usr/bin/env bash
# Helper를 패널에 넣을 실행 파일로 빌드한다 → panel/cep/src/bin/ (bolt copyAssets로 dist/cep/bin에 복사됨)
#   macOS:   bin/darwin/birdcage-helper          (arm64 + x86_64 universal, lipo)
#   Windows: bin/win32-x64/birdcage-helper.exe
# 사용: scripts/build-helper.sh [mac|win|all]   (기본 all)
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/panel/cep/src/bin"
target="${1:-all}"
cd "$root/helper"

package_ver="$(node -p "require('$root/panel/cep/package.json').version")"
ver="${BIRDCAGE_VERSION:-${package_ver}-dev}"
if [ -z "${BIRDCAGE_VERSION:-}" ] && sha=$(git -C "$root" rev-parse --short HEAD 2>/dev/null); then
  ver="$ver+$sha"
  git -C "$root" diff --quiet 2>/dev/null || ver="$ver.dirty"
fi
ldflags="-s -w -X main.version=$ver"

if [ "$target" = mac ] || [ "$target" = all ]; then
  mkdir -p "$out/darwin"
  tmp=$(mktemp -d)
  GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$tmp/arm64" ./cmd/birdcage-helper
  GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$tmp/amd64" ./cmd/birdcage-helper
  if command -v lipo >/dev/null; then
    lipo -create -output "$out/darwin/birdcage-helper" "$tmp/arm64" "$tmp/amd64"
  else
    echo "lipo 없음(macOS 전용 도구) → arm64만 넣습니다" >&2
    cp "$tmp/arm64" "$out/darwin/birdcage-helper"
  fi
  chmod +x "$out/darwin/birdcage-helper"
  rm -rf "$tmp"
  echo "built $out/darwin/birdcage-helper ($ver)"
fi

if [ "$target" = win ] || [ "$target" = all ]; then
  mkdir -p "$out/win32-x64"
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$out/win32-x64/birdcage-helper.exe" ./cmd/birdcage-helper
  echo "built $out/win32-x64/birdcage-helper.exe ($ver)"
fi
