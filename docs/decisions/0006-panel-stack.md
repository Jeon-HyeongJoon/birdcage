# 0006. bolt-cep + React 18 + 호스트 무관 코어

- 상태: 초안 (2026-10-02)
- 실행 가능한 전제: P-0006-a, P-0006-b, P-0006-c (`docs/decisions/premises.json`, `make premises`)

## 결정
- CEP 패널 골격: **hyperbrew/bolt-cep 2.2.3**(MIT) — Vite + TS, ExtendScript TS→ES3 빌드, 매니페스트 생성, `evalTS`.
- UI: **React 18.3**(bolt 기본 React 19에서 내림). 공통 로직은 `panel/core`(프레임워크 무관 Controller)에 두고 UI는 state만 그린다.
- 스타일은 flexbox·단순 선택자만(UXP 재사용 대비).
- ExtendScript는 **ASCII 전용**(문자열·주석): BOM 없는 파일을 시스템 코드페이지(예: CP949)로 읽어 한글이 깨질 수 있음 → 오류는 코드로 반환, 한국어 메시지는 패널이 생성.
- 빌드는 시스템에 부수효과를 남기지 않는다: bolt-cep 플러그인이 빌드마다 CEP 확장 폴더에 링크를 만드는 동작(끄는 옵션 없음)을
  `noImplicitCepLink`(vite.config.ts)가 되돌린다. Premiere 연결 링크는 `scripts/cep-dev-install.*` / `npm run symlink`로만 만든다(사용자 결정 2026-10-02).
- 프로토콜 계약: Go 테스트가 `/protocol/fixtures/*.json`을 생성(`-update`), TS 테스트가 같은 파일을 검증.

## 근거
- 오픈소스 조사(2026-10): 동일 기능 저장소 중 성숙한 기반 없음 → 검증된 골격(bolt) 위에 직접 구현.
- React 18은 Chromium 74(CEP 10)와 UXP 양쪽에서 실적이 많고 19의 신규 API가 필요 없음.
- bolt-cep 공식 지원은 CC 2024+ → **Premiere 15.x에서 실제 로드 검증이 남은 과제**.

## 재검토 조건
- 15.x에서 bolt 산출물이 로드되지 않거나, UXP 공유 시 React가 걸림돌이 될 때.
