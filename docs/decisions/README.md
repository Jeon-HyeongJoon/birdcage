# 설계 결정 기록 (ADR)

모든 결정은 **초안**이다. 각 결정은 실행 가능한 전제(`premises.json`)를 가지며 `make premises`가 검사한다. 더 나은 설계가 추론되거나 근거가 발견되면 교체한다.
교체할 때는 해당 ADR의 상태를 `대체됨(→ NNNN)`으로 바꾸고 새 ADR에 근거·바뀌는 범위·이전 비용을 적는다.

| # | 결정 | 상태 |
|---|---|---|
| [0001](0001-host-strategy.md) | CEP 패널 하나로 Premiere 15.0–26 커버, UXP는 v2 | 초안 |
| [0002](0002-helper-process.md) | Go Helper + 루프백 WebSocket(토큰·Origin) | 초안 |
| [0003](0003-download-engine.md) | yt-dlp(onedir) 주 엔진 + 체크섬 검증 설치 | 초안 |
| [0004](0004-normalization.md) | Premiere용 정규화 정책 | 초안 |
| [0005](0005-performance-monitoring.md) | 성능 기록 필수(pprof + JSONL perf 로그) | 초안 |
| [0006](0006-panel-stack.md) | bolt-cep + React 18 + 호스트 무관 코어 | 초안 |
| [0007](0007-loop-sessions-storage.md) | 루프·세션·저장 정책 | 초안 |
| [0008](0008-failure-taxonomy-ssot.md) | 실패 분류표 SSOT | 초안 |
| [0009](0009-principles-ssot.md) | 원칙 SSOT와 생성된 AGENTS.md | 초안 |
