# Birdcage

YouTube · TikTok 링크로 영상을 받아 **Premiere에서 바로 편집 가능한 형태로 정규화해 프로젝트에 넣는** 패널.
개인·비상업 용도. Windows + macOS, Premiere 2021(15.x)–26.

```
Premiere ─ CEP 메인 패널 (미리보기·옵션·현재 요청) ── ws://127.0.0.1 (토큰·Origin) ──▶ Go Helper
           ├ 다운로드 목록 패널 (진행·실패·완료 기록)                                  ├ yt-dlp(onedir) + deno
           └ ExtendScript: importFiles → Birdcage/<플랫폼> 빈                          ├ ffprobe 분석(VFR 감지) → ffmpeg 정규화
                                                                                     └ 성능 로그(JSONL) + pprof
```

설계 결정과 근거: [docs/decisions](docs/decisions/README.md) (모두 초안, 각 결정은 실행 가능한 전제를 가짐)

**에이전트·루프 작업 규칙: [AGENTS.md](AGENTS.md)** (생성물 — SSOT는 `specs/principles.json` + `loop/policy.json`)

**설치 파일: [Birdcage 0.2.0 Release](https://github.com/Jeon-HyeongJoon/birdcage/releases/tag/v0.2.0)**

## 구조

| 경로 | 내용 |
|---|---|
| `helper/` | Go Helper. `cmd/birdcage-helper`(실행 파일), `cmd/perfreport`(성능 요약), `internal/{engine,media,jobs,server,tools,proc,perf,library,protocol}` |
| `protocol/fixtures/` | 패널↔Helper 메시지 계약(Go가 생성, TS가 검증) |
| `panel/core/` | 호스트 무관 TS 코어: 프로토콜 타입, HelperClient, Controller(재연결·자동 임포트), HostAdapter |
| `panel/cep/` | 메인·다운로드 목록 CEP 패널(bolt-cep). `src/js/host/cep-host.ts`(Helper 실행·임포트), `src/jsx/ppro/ppro.ts`(ExtendScript) |
| `panel/cep/test/golden/` | [UI 골든 화면 카탈로그](panel/cep/test/golden/README.md): 픽셀 화면 24개·DOM 구조 22개와 갱신 규칙 |
| `scripts/` | `test-helper.sh`(테스트+성능 기록), `build-helper.sh`, `cep-dev-install.sh/.ps1` |
| `specs/` | SSOT 명세: 원칙(`principles.json`), 실패 분류표(`failures.json`) → `make gen`으로 Go·TS·AGENTS.md 생성 |
| `loop/` | 루프 정책·스키마·백로그·반복 기록·증적 원장(커밋), `loop/.runs/`는 원본 산출물(로컬, gitignore) |
| `tools/` | `loopctl`(루프 운영·검사·게이트·전제), `specgen`(SSOT 생성기) |

## 현재 상태 (0.2.0)

**현재 흐름 = 유튜브 검색 또는 링크 → 단계적 미리보기 → 구간·화질·오디오·자막 선택 → 백그라운드 다운로드 → Premiere 배치. 미리보기 옆 탐색 목록(추천·같은 채널·비슷한 제목·검색 결과)에서 다음 레퍼런스로 이어 간다.**

| 항목 | 상태 | 근거 |
|---|---|---|
| YouTube: 조회→다운로드→분석→정규화 | ✅ 검증 | 온라인 E2E(jNQXAC9IVRw), 만료된 미디어 URL 403은 메타데이터를 새로 조회해 1회 재시도 |
| 미리보기·편집 옵션 | ✅ 검증 | 스토리보드→파형→360p 재생, In/Out·화질·오디오 전용·자막, 구간 프레임·오디오 정렬 테스트 |
| Premiere 작업 흐름 | ✅ 부분 검증 | 15.2에서 패널 로드·Helper 연결·다운로드→`Birdcage/Youtube` 빈 자동 임포트(T-005, 2026-10-03 초기 빌드)와 H.264 미리보기 재생 확인. 구간 In/Out·새 빈 트랙 배치·캡션·단축키는 사람 확인 대기(T-023) |
| 탐색 목록·유튜브 검색 (0.2.0) | ✅ 검증 | 시청 페이지 '다음 동영상'(9/10 시드, 중앙값 0.89s)→같은 채널→제목 검색 대체, 검색 첫 페이지 0.9–1.4s·더 보기, 넓으면 사이드바·좁으면 아래, [←] 복원. Premiere 15.2 실기 사용 후 병합(ADR 0012·0013). TikTok 검색은 로그인 없이 결과가 없어 제외 |
| 패널 반응성 (0.2.0) | ✅ 실기 검증 | CEP 10의 시스템 글꼴 경로가 새 글자·단어마다 느려 검색 뒤 0.8–3.5s 멈춤 → 글꼴 이름 지정으로 멈춤 없음(T-037, uilint `no-system-font`) |
| 백그라운드 다운로드 관리 | ✅ 검증 | 메인은 현재 링크에 집중, 별도 `Birdcage 다운로드 목록` 패널에서 진행·실패·완료 기록 관리, 중복 요청 방지 |
| 프로젝트 로컬 소스 관리 | ✅ 자동 검증 | 저장된 현재 `.prproj` 폴더의 `Birdcage/<플랫폼>`을 요청마다 기본 위치로 사용. 미저장·클라우드 프로젝트나 쓰기 불가 경로는 사용자 Movies/Videos의 Birdcage 폴더로 폴백 |
| 완료 기록 공유 | ✅ 자동 검증 / Windows 재확인 대기 | 두 CEP 패널이 Helper 데이터 폴더의 `download-history.json`을 공유하고 기존 0.1.1 기록을 이관 |
| 도구 설치: yt-dlp·Deno | ✅ 검증 | 체크섬 검증·롤백·zip-slip 거부. Windows Get-FileHash 형식 파싱 수정 후 사용자 실기에서 초기 설치 확인 |
| 정규화: VP9/Opus→H.264/AAC 48k, VFR→CFR, passthrough | ✅ 검증 | ffmpeg 픽스처 테스트(VideoToolbox), 결과 재분석 |
| 보안: 루프백·토큰·Origin | ✅ 검증 | server 테스트(잘못된 토큰 401, 웹 Origin 403, file:// 허용) |
| 성능 기록: pprof + perf JSONL + 테스트 리포트/회귀 비교 | ✅ 검증 | `scripts/test-helper.sh`, E2E에서 pprof 엔드포인트 확인 |
| 패널 코어 ↔ 실제 Helper (Node) | ✅ 검증 | `panel/core/test/integration.test.ts` (실행→연결→작업→자동 임포트 호출) |
| CEP 패널·Windows x64 개인 배포 | ✅ 빌드 / 부분 실기 | CSXS 10, Chromium 74, ES3. Windows에서 확장 인식·도구 설치 확인(0.1.2), 0.2.0 ZIP에 Helper·FFmpeg·ffprobe·설치기 포함 |
| macOS 개인 배포 (0.2.0) | ✅ 빌드 / 설치기 시험 | universal Helper + FFmpeg 8.1.2(evermeet, Intel·Apple Silicon은 Rosetta, 네이티브 ffmpeg 우선) + 체크섬·격리 속성 제거·PlayerDebugMode 설치기 |
| TikTok 실링크 | ⏳ 미검증 | `BIRDCAGE_E2E_URLS`로 지정해 검증 예정 |
| Instagram | 범위 제외 | 공개 Reel도 로그인 쿠키를 요구하는 사례를 재현해 지원 기능 명시에서 제외 |

현재 범위에서 뺀 것: 쿠키/로그인, Instagram, TikTok 검색, 재생목록, 라이브, HDR 톤매핑, 프록시·메타데이터(원본 URL) 기록, L1–L5 폴백(실패 분류·정책만 구현), UXP 패널.

## 빠른 시작 (macOS)

```bash
# 요구: Go 1.26+, Node 22+, ffmpeg/ffprobe(brew install ffmpeg)
cd helper && go run ./cmd/birdcage-helper --ensure-tools   # yt-dlp·deno 설치(체크섬 검증) → ~/Library/Application Support/Birdcage/tools
cd .. && make test                                      # Helper 테스트(성능 기록) + 패널 코어 테스트
scripts/cep-dev-install.sh                              # PlayerDebugMode + Helper·패널 빌드 + CEP 확장 폴더 링크(되돌리기: --uninstall)
# Premiere 재시작 → 창 > 확장 > Birdcage / Birdcage 다운로드 목록
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

## 개인용 배포 (Windows x64 · macOS)

- 다운로드: [Windows x64 ZIP](https://github.com/Jeon-HyeongJoon/birdcage/releases/download/v0.2.0/Birdcage-Windows-x64-0.2.0.zip) · [macOS ZIP](https://github.com/Jeon-HyeongJoon/birdcage/releases/download/v0.2.0/Birdcage-macOS-0.2.0.zip) · [릴리스 노트](docs/releases/0.2.0.md)
- 현재 배포 버전은 `panel/cep/package.json`의 **0.2.0**.
  `make package-windows` → `artifacts/windows/Birdcage-Windows-x64-<version>.zip`, `make package-macos` → `artifacts/macos/Birdcage-macOS-<version>.zip`.
- 서명·공증 없는 개인용 배포판이다. 두 설치기 모두 SHA256SUMS로 파일을 확인하고, 기존 확장은 확장 폴더 밖(Birdcage 데이터 폴더의 `backups`)에 보관한다
  (확장 폴더 안에 같은 ID의 사본이 남으면 CEP가 어느 쪽을 열지 보장되지 않는다). 서명 없는 패널을 열 수 있게 CSXS 10·11·12 PlayerDebugMode를 켠다.
- macOS: ZIP을 풀고 터미널에서 `bash install.command`(더블클릭은 Gatekeeper가 서명 없는 스크립트로 거부한다 — `spctl` 실측 rejected).
  설치기는 설치한 사본의 격리 속성을 파일마다 지우고(macOS 27의 `xattr`에는 `-r`이 없음) Helper·FFmpeg 실행을 확인한다. 격리 속성이 남으면 Helper가 강제 종료된다(exit 137 실측).
  FFmpeg 8.1.2는 ffmpeg.org가 안내하는 evermeet.cx 정적 빌드(Intel)라 Apple Silicon에서는 Rosetta로 돌고, Homebrew 등 네이티브 ffmpeg가 있으면 그쪽을 먼저 쓴다.
  Helper 최소 macOS 13(Go 툴체인). 이 기기의 개발용 링크(`cep-dev-install.sh`)도 설치 시 백업으로 옮겨지므로 개발로 돌아가려면 스크립트를 다시 실행한다.

### Windows x64

- ZIP을 풀고 `install.cmd`를 실행한다. 관리자 권한은 필요 없다.
- ZIP은 checksum으로 고정한 FFmpeg/ffprobe GPLv3 정적 빌드와 Windows x64 Helper를 포함한다. yt-dlp·Deno는 첫 실행 때 체크섬을 검증해 설치한다.
- Premiere 메뉴는 `창 > 확장 > Birdcage`와 `Birdcage 다운로드 목록` 두 개다. 완료 기록은 `%LOCALAPPDATA%\Birdcage\download-history.json`에 저장된다.
- 사용자 실기에서 Windows 확장 인식과 초기 도구 설치까지 확인했다. 남은 확인은 0.1.2 완료 기록 유지, 링크 1건의 Premiere 임포트, 한글 사용자명 경로, Defender·Job Object 동작이다.

## 라이선스 메모

- `panel/cep`는 bolt-cep(MIT) 템플릿 기반: `panel/cep/THIRD_PARTY_LICENSE_bolt-cep.txt`
- yt-dlp(Unlicense)·Deno(MIT)는 Helper가 최초 실행 시 설치한다. Windows·macOS 개인용 ZIP은 FFmpeg/ffprobe(GPLv3)를 번들하고 출처·라이선스 고지를 포함한다.
