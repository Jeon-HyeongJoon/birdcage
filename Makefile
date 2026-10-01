# Birdcage 개발 명령. Windows에서는 scripts/*.ps1 또는 각 명령을 직접 실행한다.
.PHONY: test helper-test helper-bench helper-e2e helper-build panel-test panel-build perf-report cep-dev-install tools

test: helper-test panel-test

helper-test:        ## Helper 테스트 + 성능 기록(helper/.perf/runs/…, 직전 실행과 비교)
	scripts/test-helper.sh

helper-bench:       ## 벤치마크 포함
	scripts/test-helper.sh --bench

helper-e2e:         ## 실제 YouTube 다운로드 포함
	scripts/test-helper.sh --e2e

helper-build:       ## 패널에 넣을 Helper 바이너리(mac universal + win x64)
	scripts/build-helper.sh

panel-test:         ## 패널 코어 테스트(프로토콜 계약 + 컨트롤러 + 실제 Helper 통합)
	cd panel/core && npm test

panel-build: helper-build   ## CEP 패널 빌드 → panel/cep/dist/cep
	cd panel/cep && npm run build

perf-report:        ## 마지막 테스트 실행의 성능 요약
	cd helper && go run ./cmd/perfreport

tools:              ## yt-dlp·deno를 Helper 데이터 폴더에 설치(체크섬 검증)
	cd helper && go run ./cmd/birdcage-helper --ensure-tools

cep-dev-install:    ## 서명 없는 개발용 패널을 Premiere에서 열 수 있게 설정
	scripts/cep-dev-install.sh
