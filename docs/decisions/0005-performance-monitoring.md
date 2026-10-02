# 0005. 성능 기록 필수 (pprof + JSONL perf 로그)

- 상태: 초안 (2026-10-02) — 사용자 요구: "Go Helper는 pprof 모니터링 및 테스트 시 로그로 성능 기록 필수"
- 실행 가능한 전제: P-0005-a, P-0005-b (`docs/decisions/premises.json`, `make premises`)

## 결정
- **실행 중**: Helper는 기본으로 `<data>/logs/perf-YYYYMMDD.jsonl`에 기록한다.
  - `span`: 단계별(probe/download/analyze/normalize, 도구 설치, WS 요청) 시간, 자식 프로세스 CPU·최대 RSS, 바이트·처리량, Go 런타임 스냅샷.
  - `sample`: `--perf-sample`(기본 10s)마다 힙·고루틴·GC·프로세스 CPU%·할당률·작업 수.
- **pprof**: `--pprof 127.0.0.1:6060`(루프백만 허용) → `/debug/pprof/*` + `/debug/perf/snapshot`. 켜면 block/mutex 프로파일도 저비용 샘플링.
- **테스트**: 모든 패키지 `TestMain`에서 `perftest.Main` → 패키지별 시간·CPU·할당·GC·최대 힙/고루틴 기록.
  `scripts/test-helper.sh`가 실행마다 `helper/.perf/runs/<UTC>/`를 만들고 `go test -json`, perf 로그, (선택) 벤치·프로파일을 모아
  `perfreport`로 요약(`summary.md`)하며 **직전 실행 대비 +20% 이상 느려진 항목을 회귀로 표시**한다.
- CPU는 OS 값(getrusage / GetProcessTimes)을 쓴다. runtime/metrics의 `/cpu/classes`는 GC 주기에만 갱신돼 짧은 구간에서 0이 나왔다(실측).

## 사용
```
scripts/test-helper.sh [--bench] [--e2e] [--profile ./internal/media]
go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=20
curl http://127.0.0.1:6060/debug/perf/snapshot
```
