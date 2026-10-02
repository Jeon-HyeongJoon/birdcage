# 0008. 실패 분류표 SSOT

- 상태: 초안 (2026-10-02)
- SSOT: `specs/failures.json` → `helper/internal/engine/failures_gen.go`, `panel/core/src/failures.gen.ts`
- 실행 가능한 전제: P-0008-a

## 결정
- 실패 종류(kind) → 실행 정책(policy) → 사용자 메시지를 명세 하나에 둔다. Go·TS 코드는 `specgen`이 생성(DO NOT EDIT).
- 생성물 머리에 의미 해시(설명 필드 제외 정규형 sha256)를 넣는다. 설명만 바꾸면 같고, 정책·메시지를 바꾸면 바뀐다.
- 패널 검증기(validators.ts)는 수신한 실패의 kind·policy가 명세와 일치하는지 확인한다.

## 근거
- 같은 표가 Go(engine.go)와 TS(protocol.ts)에 따로 있었다 → 해석 차이의 원인(원칙 W2).
- `specgen -check`가 L1 게이트(G-gen)로 생성물 최신 여부를 막는다.

## 재검토 조건
- 분류 정규식(classify.go)까지 명세로 올릴 필요가 생길 때(감시 루프가 패턴을 갱신하게 되면).
