Birdcage macOS 개인용 배포판
=================================

지원 범위
- macOS 13(Ventura) 이상, Apple Silicon·Intel (Helper는 universal 실행 파일, Go 툴체인 기준 최소 13.0)
- Adobe Premiere Pro 2021(15.x) 이상의 CEP 패널
- 서명·공증되지 않은 개인용 배포판

설치
1. ZIP을 압축 해제합니다.
2. Premiere Pro를 종료합니다.
3. install.command를 실행합니다.
   - 처음에는 "확인되지 않은 개발자" 경고가 뜰 수 있습니다. Finder에서 install.command를
     Control-클릭 → [열기] → [열기]를 누르거나, 터미널에서 다음을 실행합니다.
       bash install.command
   - 관리자 권한은 필요 없습니다.
4. Premiere Pro를 시작하고 [창 > 확장 > Birdcage]를 엽니다.
   진행·완료 기록은 같은 메뉴의 [Birdcage 다운로드 목록]에서 확인합니다.

설치기가 하는 일
- SHA256SUMS.txt로 파일이 손상되지 않았는지 확인합니다(--verify-only로 확인만 할 수 있음).
- ~/Library/Application Support/Adobe/CEP/extensions/com.birdcage.panel 에 복사합니다.
  기존 Birdcage(개발용 링크 포함)는 ~/Library/Application Support/Birdcage/backups 에 보관합니다.
- 인터넷에서 받은 파일 표시(격리 속성)를 설치한 사본에서만 지웁니다.
- 서명 없는 패널을 열 수 있게 CSXS 10·11·12의 PlayerDebugMode를 켭니다.

FFmpeg
- ffmpeg·ffprobe 8.1.2(Intel 정적 빌드)를 포함하므로 별도 설치가 필요 없습니다.
- Apple Silicon에서는 Rosetta로 실행됩니다. Premiere 2021은 원래 Rosetta가 필요합니다.
  Rosetta가 없다면: softwareupdate --install-rosetta --agree-to-license
- Homebrew 등으로 설치한 ffmpeg가 있으면 그쪽(네이티브)을 먼저 씁니다.
- yt-dlp와 Deno는 첫 실행 때 인터넷에서 체크섬을 확인해 설치됩니다.

데이터 위치
- 도구·로그·미리보기 캐시·완료 기록: ~/Library/Application Support/Birdcage
- 받은 영상: ~/Movies/Birdcage

삭제
- uninstall.command를 실행합니다. 패널은 ~/Library/Application Support/Birdcage/uninstalled 로
  옮겨 되살릴 수 있고, 받은 미디어·설정은 지우지 않습니다.

이 ZIP의 출처를 신뢰할 수 있을 때만 실행하십시오.
제3자 소프트웨어 고지는 licenses 폴더와 FFMPEG_SOURCE.txt에 있습니다.
