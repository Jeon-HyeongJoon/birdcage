#!/usr/bin/env bash
# macOS: 서명 없는 개발용 CEP 패널을 Premiere에서 열 수 있게 한다.
#  1) CSXS 10/11/12 PlayerDebugMode=1  (Premiere 15.0–15.3=CEP10, 15.4+=CEP11, 25+=CEP12)
#  2) Helper 빌드 + 패널 빌드 + ~/Library/Application Support/Adobe/CEP/extensions/com.birdcage.panel → dist/cep 링크
#     (일반 `npm run build`는 링크를 남기지 않는다 — vite.config.ts noImplicitCepLink. 링크는 여기서만 만든다)
# 되돌리기: scripts/cep-dev-install.sh --uninstall
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"

if [ "${1:-}" = "--uninstall" ]; then
  for v in 10 11 12; do defaults delete "com.adobe.CSXS.$v" PlayerDebugMode 2>/dev/null || true; done
  (cd "$root/panel/cep" && npm run delsymlink) || true
  ext="$HOME/Library/Application Support/Adobe/CEP/extensions"
  rmdir "$ext" "$(dirname "$ext")" 2>/dev/null || true # 비어 있을 때만
  echo "PlayerDebugMode 해제, 패널 링크 제거"
  exit 0
fi

for v in 10 11 12; do defaults write "com.adobe.CSXS.$v" PlayerDebugMode 1; done
echo "PlayerDebugMode=1 (CSXS 10/11/12)"

"$root/scripts/build-helper.sh" mac
(cd "$root/panel/cep" && npm install --no-audit --no-fund && npm run build && npm run symlink)
echo
echo "완료: Premiere를 재시작한 뒤 [창 > 확장 > Birdcage]"
echo "디버그: Chrome에서 http://localhost:8860 (panel/cep/dist/cep/.debug)"
