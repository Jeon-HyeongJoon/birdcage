#!/bin/bash
# Birdcage macOS 설치기: 체크섬 확인 → Premiere 종료 확인 → CEP 확장 폴더에 복사(기존 것은 Birdcage 데이터 폴더에 백업)
# → 인터넷에서 받은 파일 표시(격리 속성) 제거 → 서명 없는 패널을 열 수 있게 PlayerDebugMode 켜기.
#   ./install.command                # 설치
#   ./install.command --verify-only  # 파일 확인만
# 시험용 환경 변수: BIRDCAGE_CEP_DIR(설치 위치), BIRDCAGE_DATA_DIR(백업 위치), BIRDCAGE_NO_DEBUG_MODE=1(설정 안 바꿈)
set -euo pipefail

pkg="$(cd "$(dirname "$0")" && pwd)"
src="$pkg/com.birdcage.panel"
fail() { echo "설치 실패: $*" >&2; exit 1; }

[ -f "$src/CSXS/manifest.xml" ] || fail "패키지가 올바르지 않습니다(CEP manifest 없음)."
[ -f "$pkg/SHA256SUMS.txt" ] || fail "패키지가 올바르지 않습니다(SHA256SUMS.txt 없음)."

echo "패키지 파일 확인 중…"
(cd "$pkg" && shasum -a 256 -c --quiet SHA256SUMS.txt) || fail "파일이 손상되었거나 바뀌었습니다. ZIP을 다시 받으세요."

if [ "${1:-}" = "--verify-only" ]; then
  echo "패키지 확인 완료."
  exit 0
fi

if pgrep -f "/Contents/MacOS/Adobe Premiere Pro" >/dev/null 2>&1; then
  fail "Adobe Premiere Pro를 종료한 뒤 다시 실행하세요."
fi

ext="${BIRDCAGE_CEP_DIR:-$HOME/Library/Application Support/Adobe/CEP/extensions}"
data="${BIRDCAGE_DATA_DIR:-$HOME/Library/Application Support/Birdcage}"
target="$ext/com.birdcage.panel"
staging="$ext/.com.birdcage.panel.installing-$$"
backup=""

mkdir -p "$ext"
rm -rf "$staging"
restore() {
  rm -rf "$staging"
  if [ -n "$backup" ] && [ ! -e "$target" ] && [ ! -L "$target" ]; then mv "$backup" "$target"; fi
}
trap restore ERR

ditto "$src" "$staging"
xattr -dr com.apple.quarantine "$staging" 2>/dev/null || true
chmod +x "$staging/bin/darwin/birdcage-helper" "$staging/bin/darwin/tools/ffmpeg/ffmpeg" "$staging/bin/darwin/tools/ffprobe/ffprobe"

if [ -e "$target" ] || [ -L "$target" ]; then
  # 백업을 확장 폴더 밖에 둔다: 같은 확장 ID가 두 개면 CEP가 어느 쪽을 열지 보장되지 않는다.
  mkdir -p "$data/backups"
  backup="$data/backups/com.birdcage.panel-$(date +%Y%m%d-%H%M%S)"
  mv "$target" "$backup"
fi
mv "$staging" "$target"
trap - ERR

if [ -z "${BIRDCAGE_NO_DEBUG_MODE:-}" ]; then
  for v in 10 11 12; do defaults write "com.adobe.CSXS.$v" PlayerDebugMode 1; done
fi

echo
echo "Birdcage를 설치했습니다:"
echo "  $target"
[ -n "$backup" ] && echo "이전 설치는 여기에 보관했습니다:" && echo "  $backup"

# 번들 FFmpeg는 Intel 빌드다. Apple Silicon에서 Rosetta도 네이티브 ffmpeg도 없으면 알린다.
if [ "$(uname -m)" = "arm64" ] && ! /usr/bin/arch -x86_64 /usr/bin/true 2>/dev/null; then
  if ! command -v ffmpeg >/dev/null 2>&1 && [ ! -x /opt/homebrew/bin/ffmpeg ]; then
    echo
    echo "주의: 포함된 FFmpeg는 Intel 빌드라 Rosetta가 필요합니다."
    echo "  softwareupdate --install-rosetta --agree-to-license   (또는 brew install ffmpeg)"
  fi
fi
echo
echo "Premiere Pro를 시작하고 [창 > 확장 > Birdcage]를 여세요. 이 창은 닫아도 됩니다."
