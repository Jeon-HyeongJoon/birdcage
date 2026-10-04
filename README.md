# Birdcage

YouTube · TikTok 링크로 영상을 받아 **Premiere에서 바로 편집 가능한 형태로 정규화해 프로젝트에 넣는** 패널.
개인·비상업 용도. Windows + macOS, Premiere 2021(15.x)–26.

```
Premiere ─ CEP 패널 (TS + React 18, bolt-cep) ── ws://127.0.0.1 (토큰·Origin) ──▶ Go Helper
            └ ExtendScript: importFiles → Birdcage/<플랫폼> 빈                          ├ yt-dlp(onedir) + deno
                                                                                    ├ ffprobe 분석(VFR 감지) → ffmpeg 정규화
                                                                                    └ 성능 로그(JSONL) + pprof
```

설계 결정과 근거: [docs/decisions](docs/decisions/README.md) (모두 초안, 각 결정은 실행 가능한 전제를 가짐)

**에이전트·루프 작업 규칙: [AGENTS.md](AGENTS.md)** (생성물 — SSOT는 `specs/principles.json` + `loop/policy.json`)

## 구조

| 경로 | 내용 |
|---|---|
| `helper/` | Go Helper. `cmd/birdcage-helper`(실행 파일), `cmd/perfreport`(성능 요약), `internal/{engine,media,jobs,server,tools,proc,perf,library,protocol}` |
| `protocol/fixtures/` | 패널↔Helper 메시지 계약(Go가 생성, TS가 검증) |
| `panel/core/` | 호스트 무관 TS 코어: 프로토콜 타입, HelperClient, Controller(재연결·자동 임포트), HostAdapter |
| `panel/cep/` | CEP 패널(bolt-cep). `src/js/host/cep-host.ts`(Helper 실행·임포트), `src/jsx/ppro/ppro.ts`(ExtendScript) |
| `scripts/` | `test-helper.sh`(테스트+성능 기록), `build-helper.sh`, `cep-dev-install.sh/.ps1` |
| `specs/` | SSOT 명세: 원칙(`principles.json`), 실패 분류표(`failures.json`) → `make gen`으로 Go·TS·AGENTS.md 생성 |
| `loop/` | 루프 정책·스키마·백로그·반복 기록·증적 원장(커밋), `loop/.runs/`는 원본 산출물(로컬, gitignore) |
| `tools/` | `loopctl`(루프 운영·검사·게이트·전제), `specgen`(SSOT 생성기) |

## V0 상태

**V0 = 링크 하나가 Helper를 거쳐 Premiere-ready 파일이 되고 임포트 호출까지 가는 한 사이클.**

| 항목 | 상태 | 근거 |
|---|---|---|
| Helper: 탐색→다운로드→분석→정규화→검증 | ✅ 검증 | 로컬 E2E(실제 yt-dlp generic), 온라인 E2E(YouTube jNQXAC9IVRw) |
| 도구 설치: yt-dlp/deno 체크섬 검증·롤백 | ✅ 검증 | 실제 GitHub 릴리스로 설치(yt-dlp 53.9MB, deno 38.5MB) + 가짜 서버 테스트(불일치·zip-slip 거부) |
| 정규화: VP9/Opus→H.264/AAC 48k, VFR→CFR, passthrough | ✅ 검증 | ffmpeg 픽스처 테스트(VideoToolbox), 결과 재분석 |
| 보안: 루프백·토큰·Origin | ✅ 검증 | server 테스트(잘못된 토큰 401, 웹 Origin 403, file:// 허용) |
| 성능 기록: pprof + perf JSONL + 테스트 리포트/회귀 비교 | ✅ 검증 | `scripts/test-helper.sh`, E2E에서 pprof 엔드포인트 확인 |
| 패널 코어 ↔ 실제 Helper (Node) | ✅ 검증 | `panel/core/test/integration.test.ts` (실행→연결→작업→자동 임포트 호출) |
| CEP 패널 빌드(CSXS 10, chrome74, ES3 ExtendScript) | ✅ 빌드 | `npm run build` 산출물·매니페스트 확인 |
| **Premiere 안에서 패널 로드·importFiles** | ⏳ 미검증 | 이 Mac의 Premiere 2021은 x86_64 전용이고 Rosetta 미설치 |
| **Windows** | ⏳ 미검증 | 크로스 컴파일·vet만 통과(Job Object, 경로 처리 코드 포함) |
| TikTok 실링크 | ⏳ 미검증 | `BIRDCAGE_E2E_URLS`로 지정해 검증 예정 |

V0에서 일부러 뺀 것: 쿠키/로그인, 재생목록, 라이브, HDR 톤매핑, 프록시·메타데이터(원본 URL) 기록, L1–L5 폴백(실패 분류·정책만 구현), UXP 패널, ffmpeg 자동 설치.

## 빠른 시작 (macOS)

```bash
# 요구: Go 1.26+, Node 22+, ffmpeg/ffprobe(brew install ffmpeg)
cd helper && go run ./cmd/birdcage-helper --ensure-tools   # yt-dlp·deno 설치(체크섬 검증) → ~/Library/Application Support/Birdcage/tools
cd .. && make test                                      # Helper 테스트(성능 기록) + 패널 코어 테스트
scripts/cep-dev-install.sh                              # PlayerDebugMode + Helper·패널 빌드 + CEP 확장 폴더 링크(되돌리기: --uninstall)
# Premiere 재시작 → 창 > 확장 > Birdcage
```

`npm run build`(panel/cep)만으로는 시스템에 아무것도 남지 않는다. bolt-cep는 빌드마다 CEP 확장 폴더에 링크를 만들지만
`vite.config.ts`의 `noImplicitCepLink`가 그 빌드에서 새로 생긴 링크를 지운다. Premiere 연결 링크는 `cep-dev-install.sh`(또는 `npm run symlink`)로만 만든다.

Premiere 2021(Intel 빌드)을 Apple Silicon에서 쓰려면 Rosetta가 필요하다: `softwareupdate --install-rosetta --agree-to-license`.
이때 패널은 Helper를 `arch -arm64`로 띄워 네이티브로 실행한다.

## 루프 (V0 이후 작업 방식)

```bash
make orient                     # 새 세션 시작: 상태·인계·다음 작업
make verify                     # 0토큰 무료 게이트(fmt·vet·gen·loop·tools·tsc·core) ~7s
cd tools && go run ./cmd/loopctl begin --task T-009 --kind worker --hypothesis "…"
make gates                      # 게이트 사다리(싼 것부터, 실패 시 중단), 증적은 loop/ledger에 기록
cd tools && go run ./cmd/loopctl brief          # 실패 시 모델에 넘길 압축 피드백
cd tools && go run ./cmd/loopctl end --state success --summary "…" --next "…" --lesson "…" --pass-task
make premises                   # ADR 전제 검사(만료된 외부 사실 포함)
```

## 성능 모니터링

- 실행 중 기록: `<data>/logs/perf-YYYYMMDD.jsonl` (단계별 시간, 자식 프로세스 CPU/RSS, 10초 주기 런타임 샘플), 로그는 `helper.log`
- pprof: `birdcage-helper --pprof 127.0.0.1:6060`
  - `go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=20`
  - `go tool pprof http://127.0.0.1:6060/debug/pprof/heap`
  - `curl http://127.0.0.1:6060/debug/perf/snapshot` (런타임 + 작업 수)
- 테스트: `scripts/test-helper.sh [--bench] [--e2e] [--profile <pkg>]` → `helper/.perf/runs/<UTC>/summary.md`
  (느린 테스트, 패키지 자원, 구간 p50/p95, 벤치마크, **직전 실행 대비 +20% 회귀**)

V0 실측(Apple Silicon, 이 Mac):

| 항목 | 값 |
|---|---|
| yt-dlp 실행 시작 | 단일 파일판 9.19s → onedir판 0.27s |
| Helper 시작→핸드셰이크 | 7–12ms |
| 패널 연결(hello.ok) | 1265ms → 305ms (도구 버전 확인 병렬화) |
| YouTube 19초 영상(360p) | 분석 2.1s · 다운로드 1.8s · 정규화 0.1ms(passthrough) |
| VP9/Opus 4초 → H.264/AAC | 정규화 ~0.3–0.4s (h264_videotoolbox) |
| H.264 인코더 감지 | 185–191ms (1회, 캐시) |

## Windows x64 (실기 테스트 전)

- Helper: `GOOS=windows go build ./cmd/birdcage-helper` (크로스 컴파일 확인됨). 자식 프로세스는 Job Object로 묶어 종료.
- 개발 설치: `scripts/cep-dev-install.ps1` (PlayerDebugMode 레지스트리 + 빌드).
- 개인용 배포 ZIP: `make package-windows` → `artifacts/windows/Birdcage-Windows-x64-<version>.zip`.
- 배포 ZIP은 checksum으로 고정한 FFmpeg/ffprobe GPLv3 정적 빌드를 Helper 옆 `tools/`에 포함한다. PATH·별도 설치는 필요 없다.
- 미검증 항목: 패널의 `bin/win32-x64` 경로, 한글 사용자명 경로, Defender 오탐, 콘솔 창 숨김.

## 라이선스 메모

- `panel/cep`는 bolt-cep(MIT) 템플릿 기반: `panel/cep/THIRD_PARTY_LICENSE_bolt-cep.txt`
- yt-dlp(Unlicense)·Deno(MIT)는 Helper가 최초 실행 시 설치한다. Windows 개인용 ZIP은 FFmpeg/ffprobe(GPLv3)를 번들하고 출처·라이선스 고지를 포함한다.
