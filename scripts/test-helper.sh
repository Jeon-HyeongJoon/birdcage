#!/usr/bin/env bash
# Helper 테스트 실행 + 성능 기록 (필수).
#
# 실행마다 helper/.perf/runs/<UTC 시각>/ 에 다음이 남는다:
#   gotest.jsonl      go test -json (테스트별 시간)
#   <패키지>.jsonl    perftest 기록 (패키지 자원 사용, 구간 측정)
#   helper-e2e.jsonl  E2E 중 Helper 자체 성능 로그 (단계별 시간, 자식 프로세스 CPU/RSS, 런타임 샘플)
#   bench.txt         --bench 지정 시 벤치마크 결과
#   cpu.pprof/mem.pprof  --profile <패키지> 지정 시
#   summary.md        perfreport 요약 (직전 실행과 비교, +20% 이상 느려지면 회귀 표시)
#
# 사용:
#   scripts/test-helper.sh                    # 단위+통합(+로컬 E2E) 테스트
#   scripts/test-helper.sh --bench            # 벤치마크 포함
#   scripts/test-helper.sh --e2e              # 실제 YouTube 다운로드 포함 (BIRDCAGE_E2E=1)
#   scripts/test-helper.sh --profile ./internal/media   # 해당 패키지 CPU/메모리 프로파일
#   scripts/test-helper.sh -- -run TestX      # go test 추가 인자
set -uo pipefail

cd "$(dirname "$0")/../helper"
bench=0
profile=""
extra=()
while [ $# -gt 0 ]; do
  case "$1" in
    --bench) bench=1 ;;
    --e2e) export BIRDCAGE_E2E=1 ;;
    --profile) profile="$2"; shift ;;
    --) shift; extra=("$@"); break ;;
    *) extra+=("$1") ;;
  esac
  shift
done

run=$(date -u +%Y%m%dT%H%M%SZ)
dir=".perf/runs/$run"
mkdir -p "$dir"
export BIRDCAGE_PERF_DIR="$PWD/$dir" BIRDCAGE_PERF_RUN="$run"
prev=$(cat .perf/LATEST 2>/dev/null || true)

echo "▶ go test (perf → helper/$dir)"
go test -json -count=1 "${extra[@]+"${extra[@]}"}" ./... > "$dir/gotest.jsonl"
status=$?

# 벤치마크/프로파일 실행의 TestMain 기록은 테스트 기록과 섞이지 않게 하위 폴더에 둔다.
if [ "$bench" = 1 ]; then
  echo "▶ benchmarks"
  BIRDCAGE_PERF_DIR="$PWD/$dir/bench-perf" go test -run '^$' -bench . -benchmem -count=1 ./... > "$dir/bench.txt" 2>&1 || status=1
fi
if [ -n "$profile" ]; then
  echo "▶ profile $profile"
  BIRDCAGE_PERF_DIR="$PWD/$dir/bench-perf" go test -run '^$' -bench . -benchmem -count=1 -cpuprofile "$dir/cpu.pprof" -memprofile "$dir/mem.pprof" \
    -o "$dir/profiled.test" "$profile" >> "$dir/bench.txt" 2>&1 || status=1
  echo "  go tool pprof -top $dir/profiled.test $dir/cpu.pprof"
fi

echo "$run" > .perf/LATEST
baseline=()
if [ -n "$prev" ] && [ -d ".perf/runs/$prev" ]; then
  baseline=(-baseline ".perf/runs/$prev")
fi
go run ./cmd/perfreport -dir "$dir" "${baseline[@]+"${baseline[@]}"}"
exit $status
