#!/bin/bash
# Birdcage macOS 제거기: CEP 확장 폴더에서 패널을 빼서 Birdcage 데이터 폴더에 보관한다(되살릴 수 있게).
# 받은 미디어·설정·PlayerDebugMode는 그대로 둔다.
set -euo pipefail
if pgrep -f "/Contents/MacOS/Adobe Premiere Pro" >/dev/null 2>&1; then
  echo "Adobe Premiere Pro를 종료한 뒤 다시 실행하세요." >&2
  exit 1
fi
ext="${BIRDCAGE_CEP_DIR:-$HOME/Library/Application Support/Adobe/CEP/extensions}"
data="${BIRDCAGE_DATA_DIR:-$HOME/Library/Application Support/Birdcage}"
target="$ext/com.birdcage.panel"
if [ ! -e "$target" ] && [ ! -L "$target" ]; then
  echo "Birdcage가 설치되어 있지 않습니다."
  exit 0
fi
mkdir -p "$data/uninstalled"
archived="$data/uninstalled/com.birdcage.panel-$(date +%Y%m%d-%H%M%S)"
mv "$target" "$archived"
echo "Birdcage를 CEP 확장 폴더에서 제거했습니다. 되살릴 수 있는 사본:"
echo "  $archived"
echo "PlayerDebugMode와 받은 미디어는 그대로 두었습니다."
