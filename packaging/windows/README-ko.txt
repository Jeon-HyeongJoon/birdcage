Birdcage Windows x64 개인용 배포판
=========================================

지원 범위
- Windows 10/11 x64
- Adobe Premiere Pro CEP 패널
- 서명되지 않은 개인용 배포판
- Windows ARM64는 아직 지원하지 않습니다.

설치
1. ZIP을 완전히 압축 해제합니다.
2. Premiere Pro를 종료합니다.
3. install.cmd를 실행합니다. 관리자 권한은 필요 없습니다.
4. Premiere Pro를 시작하고 [창 > 확장 > Birdcage]를 엽니다.
   진행·완료 기록은 같은 메뉴의 [Birdcage 다운로드 목록]에서 확인합니다.

설치 경로
%APPDATA%\Adobe\CEP\extensions\com.birdcage.panel

이 배포판은 ffmpeg.exe와 ffprobe.exe를 포함하므로 PATH 설정이나
FFmpeg 별도 설치가 필요 없습니다. yt-dlp와 Deno는 첫 실행 시 인터넷에서
다운로드됩니다.

완료 기록
%LOCALAPPDATA%\Birdcage\download-history.json

메인 패널을 닫아도 다운로드 목록 패널이 이 파일을 직접 읽습니다.

기존 Birdcage가 있으면 설치기가 같은 CEP 폴더에 backup-날짜 이름으로 보존합니다.
삭제하려면 uninstall.cmd를 실행합니다. 다운로드한 미디어와 설정은 제거하지 않습니다.

무서명 실행 파일이므로 Windows Defender가 경고할 수 있습니다. 이 ZIP의
출처를 신뢰할 수 있을 때만 실행하십시오.

제3자 소프트웨어 고지는 licenses 폴더와 FFMPEG_SOURCE.txt에 있습니다.
