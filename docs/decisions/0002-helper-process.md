# 0002. Go Helper + 루프백 WebSocket

- 상태: 초안 (2026-10-02)

## 결정
- 다운로드·변환은 별도 **Go Helper 프로세스**가 한다. 패널은 UI와 Premiere API 호출만.
- 패널이 Helper를 실행하고 표준출력 첫 줄 JSON으로 포트·토큰을 받는다. 패널이 죽으면 stdin EOF로 Helper도 종료.
- 통신: `ws://127.0.0.1:<랜덤포트>/ws?token=<실행별 32바이트>`. Origin 허용: 없음/`null`/`file://`(CEP). 일반 웹페이지 Origin은 403.
- 자식 프로세스는 트리 단위로 종료(Unix 프로세스 그룹, Windows Job Object).

## 근거
- UXP엔 child_process가 없어 어차피 Helper가 필요 → CEP·UXP가 같은 Helper를 공유.
- 오래 걸리는 작업이 죽어도 Premiere는 안전(크래시 격리).
- 오픈소스 조사(2026-10): 다수가 `0.0.0.0` 바인딩·`CORS *`·인증 없음 → 브라우저 웹페이지가 로컬 서버를 조작할 수 있음. YT2Premiere의 토큰·Origin 모델을 참고.
- 실측: Helper 시작→핸드셰이크 7–12ms, 패널 연결(hello.ok) ~305ms(도구 버전 확인 병렬화 후, 순차 1265ms).

## 재검토 조건
- UXP에서 루프백 연결 제약이 확인될 때, 또는 Hybrid 애드온이 더 단순해질 때.
