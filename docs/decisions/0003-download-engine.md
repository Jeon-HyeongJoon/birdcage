# 0003. yt-dlp(onedir) 주 엔진 + 체크섬 검증 설치

- 상태: 초안 (2026-10-02)
- 실행 가능한 전제: P-0003-a, P-0003-b, P-0003-c (`docs/decisions/premises.json`, `make premises`)

## 결정
- 엔진은 **yt-dlp**. `-J`(포맷 선택 포함)로 info.json을 만든 뒤 `--load-info-json`으로 재추출 없이 다운로드.
- 배포판은 **압축 해제판(onedir zip)**: `yt-dlp_macos.zip`(universal2), `yt-dlp_win.zip`.
- Helper가 GitHub 릴리스에서 직접 설치: `SHA2-256SUMS` 검증 → `<data>/tools/yt-dlp/<버전>/` → `current`/`previous`(롤백).
- YouTube용 JS 런타임은 **deno**를 설치(`.sha256sum` 검증)하고 `--js-runtimes deno:<경로>`로 명시.
- 실패는 stderr로 분류(engine.Kind) → 정책(stop/retry/retry_late/fallback/ask_user). V0는 network만 1회 재시도.

## 근거 (실측)
- 단일 파일판 `yt-dlp_macos`는 실행마다 **9.19s**, onedir판은 첫 실행 후 **0.27s** (34배). 첫 실행 비용(~9s, macOS 검사)은 설치 시 스모크 테스트로 미리 치른다.
- yt-dlp EJS 위키: 공식 실행 파일에 yt-dlp-ejs 포함, deno 2.3+ 권장(기본 활성), node 22+는 명시 필요.
- PATH의 node는 fnm/nvm 셸별 임시 경로(`fnm_multishells/…`)인 경우가 많아 Premiere가 띄운 Helper에선 사라질 수 있음 → deno를 직접 설치.
- Go/Rust/Python 대체 라이브러리(kkdai/youtube, lux, rusty_ytdl, rustypipe, pytube/pytubefix)는 수정 속도·지원 사이트에서 열세(조사 2026-10).
- 2026-10 실측: yt-dlp 공식 테스트 영상 BaW_jenozKc는 "This video is unavailable" → 온라인 E2E는 jNQXAC9IVRw 사용.

## 재검토 조건
- yt-dlp가 장기간 특정 사이트에서 막힐 때(보조 엔진 L4), GPG 서명 검증 추가 시.
