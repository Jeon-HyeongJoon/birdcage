# 0004. Premiere용 정규화 정책

- 상태: 초안 (2026-10-02)
- 실행 가능한 전제: P-0004-a, P-0004-b (`docs/decisions/premises.json`, `make premises`)

## 결정
- 포맷 선택: `-f bv*+ba/b -S hdr:sdr,res:<max>,vcodec:h264,acodec:aac` (SDR > 해상도 > H.264 > AAC).
- ffprobe로 분석 후 스트림별 copy/transcode 결정(media.MakePlan):
  - 영상 OK: H.264 8bit 4:2:0 (+26 이상은 HEVC 8bit) 그리고 CFR.
  - VFR 판정: 앞 30초 패킷 간격 분포에서 중앙값 ±10% 밖 비율 > 2% 또는 r/avg frame rate 1% 이상 차이.
  - 오디오 OK: AAC 44.1/48kHz → 그대로(AAC→AAC 재인코딩은 손실뿐, Premiere가 임포트 시 conform). 그 외 → AAC 256k 48kHz.
  - 둘 다 OK + MP4면 **그대로 이동(passthrough)**, 컨테이너만 다르면 remux.
- 변환 시: `-fps_mode cfr -r <표준 fps로 스냅>`, GOP = 2초, `yuv420p`, `+faststart`, 하드웨어 인코더 우선
  (mac `h264_videotoolbox`, win `nvenc→qsv→amf`, 실패 시 `libx264 -crf 18`). 비트레이트 0.14 bpp·fps, 4–80Mbps.
- 저장: `<라이브러리>/<플랫폼>/<제목> [<id>].mp4` (NFC, Windows 금지문자·예약어, 80자). 동시 작업 경로 충돌은 예약으로 방지.

## 근거
- 실측(V0): YouTube 실영상은 H.264/AAC가 선택돼 passthrough(정규화 0.1ms). VP9/Opus 4초 → VideoToolbox 변환 ~0.3–0.4s. 인코더 감지 185–191ms(1회 캐시).
- VFR→CFR 변환 결과를 같은 분석기로 재검증(테스트).
- 동시 다운로드 시 같은 경로를 받아 rename 실패하던 버그를 jobs 테스트에서 발견 → library.Reserve.

## 재검토 조건
- HDR 톤매핑(V1), ProRes/DNxHR 편집용 프리셋, 프록시 자동 연결(attachProxy) 추가 시.
