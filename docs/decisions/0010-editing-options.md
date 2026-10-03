# 0010. 편집용 가져오기 옵션: 넣을 기능과 뺄 기능

- 상태: 초안 (2026-10-03)
- 실행 가능한 전제: P-0010-a, P-0010-b, P-0010-c, P-0010-d (`docs/decisions/premises.json`, `make premises`)
- 재현: `scripts/bench/editing-options.sh` (로컬), `--online` (조회 재사용)

## 기준
사용자 지시(2026-10-03): 체감되는 이득에 비해 처리 비용(시간·용량·복잡도)이 크면 넣지 않는다. 크다/작다는 판단도 실측과 출처로 한다.
측정 환경: Apple M1 Pro, ffmpeg 8.1.1, yt-dlp 2026.08.19, 1분 원본(testsrc2 + 노이즈, GOP 2s), 실제 YouTube 영상 jNQXAC9IVRw.

## 결정
| 기능 | 결정 | 비용(실측) | 이득(실측·출처) |
|---|---|---|---|
| 구간 추출 | **넣음**: 핸들(앞뒤 1s) 포함 구간만 받고 프로젝트 항목 In/Out으로 정확한 구간 지정 | 전체보다 적게 받음(감소) | In 프레임 md5 일치, 오디오 오프셋 −0.62ms(H.264 복사)·0.00ms(VP9→H.264) |
| 조회 결과 재사용 | **넣음**: 링크 조회(-J)의 info.json을 다운로드에 그대로 사용 | 0 | 다운로드 2.23s → 0.86s(재추출 생략, −1.37s/건) |
| 화질 선택 | **넣음**: 높이별 크기·"변환 필요"만 표시 | 0(-J 포맷 목록 24개) | 사용자가 용량·변환을 보고 고름 |
| 변환 시간 예측 | **보류** | 성능 기록 저장·모델 | 첫 사용 시 기록이 없어 값이 없다 |
| 오디오만 | **넣음**: WAV(PCM 16bit 48k) | 디코드 0.29s/분 | Adobe: 무압축·시퀀스와 같은 샘플레이트면 conform 없음, 압축 오디오는 가져올 때 항상 conform |
| 자막 | **넣음(옵트인)**: srt 사이드카, 언어 1개, 영상과 별도 호출 | 요청 1회 | YouTube 자막 요청은 429가 알려져 있고(yt-dlp #13831·#11059·#2706) yt-dlp는 자막 실패 시 기본으로 다운로드 전체를 실패시킨다 → 별도 호출로 격리 |
| 캡션 트랙 생성 | **기능 검사 후에만**(`createCaptionTrack` 존재 시) | API 1회 | 스크립팅 가이드에 도입 버전이 없다 → 15.x 동작은 실기 확인(T-023) |
| 타임라인 배치 | **넣음**: 빈에만 / 빈 트랙에 안전 배치 / 삽입 / 덮어쓰기, 트랙은 Premiere 트랙 타겟팅을 따름 | API 1회 | — |
| 프록시 자동 연결 | **뺌** | 4K 1분 14.8s(기존 remux 0.44s의 ×34), 그동안 가져오기 대기 | 4K 탐색 최악 −193ms. Premiere 내장 Create Proxies가 AME에서 백그라운드로 같은 일을 한다 |
| 편집용 코덱(ProRes) | **뺌** | 1080p 1분 6.5s(Mac HW, ×22) / 21.3s(prores_ks, Windows에 HW 인코더 없음, ×73), 용량 ×3.1–7 | 1080p 탐색 최악 −73ms(GOP 2s 디코드 생략) |
| 시퀀스 fps 변환 | **뺌**(불일치 경고만) | 전체 재인코딩 | 프레임 보간 품질 문제, Premiere가 시퀀스에서 처리 |

### 구간 추출 방식(검증됨, 2026-10-03 갱신)
- **먼저 범위 요청**: DASH mp4(YouTube·Instagram, 스트림별 단일 트랙 조각 파일)는 앞쪽 sidx(시간→바이트 색인)로 필요한 조각만
  8MB 이하 범위 요청 4개 병렬로 받아 초기화 부분(ftyp+moov) 뒤에 붙인다(`internal/rangefetch`). 조각의 tfdt가 원래 시각을 담는다.
  사용자 QA 영상(34분 52초 1080p, 1000–1030s): 첫 진행률 0.3s, 받기 0.89s, 전체 2.1s, In 프레임이 서버 원본과 md5 일치, 오디오 −0.04ms.
  이전 방식(ffmpeg 구간)은 연속 스트림이라 재생 속도 약 1.8배로 제한되고(32s 구간 19.1s) 진행률이 없어 멈춘 것처럼 보였다.
- 여러 트랙이 섞인 조각 파일은 조각 시각과 처음부터 읽은 시각이 어긋나(실측 21ms) 이 경로에서 뺀다. 색인 없음·URL 만료·webm은
  아래 ffmpeg 구간 받기로 넘어가며, 이때 ffmpeg의 time=으로 진행률을 낸다.
- 대체 경로: `--download-sections "*S-E" --downloader-args "ffmpeg_o:-copyts" --merge-output-format mp4`.
  DASH는 비디오·오디오가 따로 탐색된다: 비디오는 직전 키프레임(5.400s), 오디오는 요청 지점(7.244s) 또는 처음(Opus/webm, 0.014s)부터.
  `-copyts` + mp4만 원래 타임라인을 보존한다(mkv는 0.011/0으로 잃음).
- 정규화: `-copyts -itsoffset -<비디오 시작> -i … -af atrim=start=0,aresample=async=1:first_pts=0` → 두 스트림 시작 0, 오디오는 앞을 자르거나 무음으로 채움(오디오만 재인코딩, 비용 미미).
- In = 요청 시작 − 비디오 시작, Out = 요청 끝 − 비디오 시작. 프로젝트 항목에 설정하고 다시 읽어 확인한다(단위가 문서마다 달라서).
- 왜 정확한 재인코딩 컷이 아닌가: 복사 경로를 지키면 비디오 화질 손실·인코딩 시간이 0이고, 편집자는 핸들을 원한다.

### 오디오만·자막(검증됨)
- 오디오만은 WAV(PCM 16bit 48k). 구간은 최고 음질 오디오가 m4a일 때만 구간 다운로드(`-copyts` 시작 7.244s 보존)하고,
  webm/Opus는 구간의 원래 시작 시각을 잃으므로(실측 0.021s, 길이 N/A) 전체 오디오(약 1MB/분)를 받아 변환하면서 샘플 단위로 자른다.
  합성 노이즈 비교: 자르기 0.00ms, m4a 정렬 −0.67ms. 실제 YouTube(140 m4a): In 1.004s.
- 자막은 영상 다운로드 뒤 `--load-info-json --skip-download --write-(auto-)subs --convert-subs srt`로 따로 받고, 파일 시작 위치만큼 옮겨
  `<파일>.<언어>.srt`로 둔다. 실패는 경고만(작업 성공). 타임라인에 놓았고 `createCaptionTrack`이 있으면 캡션 트랙을 (놓은 위치 − In)에 만든다.

### UI
- 한 열 구성. 카드는 영상 정보 하나만, 반경 2px(사용자 지시: 라운드가 매우 적은 네모). 나머지는 행·구분선·세그먼트.
- 토큰은 `panel/cep/src/js/main/_tokens.scss` 하나. `uilint`(G-uilint)가 반경 > 2px과 CEP 10에서 깨지는 CSS를 막는다.
  CEP 10 = Chromium 74.0.3729.157(Adobe CEP 10 Cookbook). 최소 Chrome(MDN browser-compat-data): flex `gap` 84, `:focus-visible` 86, `inset` 87,
  `aspect-ratio`·`:is()`·`:where()` 88, `:has()` 105, `clamp()` 79. UXP는 CSS Grid 없음.

## 근거(출처)
- Adobe, Audio conforming specifications: https://helpx.adobe.com/premiere/desktop/organize-media/import-files/audio-conforming-specifications.html
- Adobe, Ingest and Proxy workflow: https://helpx.adobe.com/th_en/premiere-pro/using/ingest-proxy-workflow.html
- yt-dlp 자막 429: https://github.com/yt-dlp/yt-dlp/issues/13831 , https://github.com/yt-dlp/yt-dlp/issues/11059 , https://github.com/yt-dlp/yt-dlp/issues/2706
- Premiere 스크립팅 Sequence(createCaptionTrack·insertClip·overwriteClip·getPlayerPosition): https://ppro-scripting.docsforadobe.dev/sequence/sequence/
- CEP 10 Cookbook(CEF/Chromium 버전): https://github.com/Adobe-CEP/CEP-Resources/blob/master/CEP_10.x/Documentation/CEP%2010.0%20HTML%20Extension%20Cookbook.md
- MDN browser-compat-data: https://github.com/mdn/browser-compat-data , caniuse flexbox-gap: https://caniuse.com/flexbox-gap
- EBU R37(오디오·비디오 상대 타이밍 허용: 오디오 앞섬 40ms, 늦음 60ms)

## 재검토 조건
- Premiere가 편집 목록/시작 오프셋을 정확히 처리함이 실기로 확인되면 오디오 재인코딩을 생략할 수 있다.
- 사용자 기기에서 프록시·ProRes 이득이 실측으로 커지면(예: 저사양 Windows 4K 탐색 지연이 체감) 다시 측정해 넣는다.
- 성능 기록이 쌓이면 변환 시간 예측을 다시 검토한다.
