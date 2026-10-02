#!/bin/sh
# 편집 옵션 결정(docs/decisions/0010)의 비용 실측을 재현한다.
#   scripts/bench/editing-options.sh            # 로컬(ffmpeg만): 프록시·ProRes·탐색 지연·WAV
#   YTDLP=... DENO=... scripts/bench/editing-options.sh --online   # + 조회 재사용(yt-dlp, 네트워크)
# 결과는 표준 출력. 같은 기계에서 반복해 비교한다(성능 주장은 3회 재현: policy.evidence).
set -eu
FFMPEG=${FFMPEG:-ffmpeg}
DUR=${DUR:-60}
work=$(mktemp -d "${TMPDIR:-/tmp}/birdcage-bench.XXXXXX")
trap 'rm -rf "$work"' EXIT
cd "$work"

now() { python3 -c 'import time; print(time.time())'; }
since() { python3 -c "import time; print(f'{time.time()-$1:.2f}s')"; }
mb() { python3 -c "import os; print(f'{os.path.getsize(\"$1\")/1048576:.1f} MB')"; }
timed() { label=$1; shift; t=$(now); "$@" >/dev/null 2>&1; echo "$label: $(since "$t")"; }

uname -m; sysctl -n machdep.cpu.brand_string 2>/dev/null || true; "$FFMPEG" -hide_banner -version | head -1
case "$(uname -s)" in Darwin) H264=h264_videotoolbox; HW="-hwaccel videotoolbox" ;; *) H264=libx264; HW="" ;; esac

# 원본: GOP 2s(30fps), 노이즈 오디오 — YouTube H.264 다운로드와 같은 형태.
"$FFMPEG" -hide_banner -loglevel error -y -f lavfi -i "testsrc2=s=1920x1080:r=30:d=$DUR" -f lavfi -i "anoisesrc=d=$DUR:r=48000:a=0.3" \
  -c:v "$H264" -b:v 8M -g 60 -pix_fmt yuv420p -c:a aac -b:a 192k s1080.mp4
"$FFMPEG" -hide_banner -loglevel error -y -f lavfi -i "testsrc2=s=3840x2160:r=30:d=$DUR" -f lavfi -i "anoisesrc=d=$DUR:r=48000:a=0.3" \
  -c:v "$H264" -b:v 40M -g 60 -pix_fmt yuv420p -c:a aac -b:a 192k s2160.mp4

echo "## 처리 비용 (${DUR}s 원본)"
timed "remux 1080p (기존 경로)" "$FFMPEG" -y -i s1080.mp4 -c copy -movflags +faststart r1080.mp4
timed "remux 2160p (기존 경로)" "$FFMPEG" -y -i s2160.mp4 -c copy -movflags +faststart r2160.mp4
# shellcheck disable=SC2086
timed "프록시 2160p→720p $H264" "$FFMPEG" -y $HW -i s2160.mp4 -vf scale=-2:720 -c:v "$H264" -b:v 5M -g 60 -c:a copy p720.mp4
if [ "$(uname -s)" = Darwin ]; then
  timed "ProRes 422 LT 1080p (videotoolbox)" "$FFMPEG" -y -hwaccel videotoolbox -i s1080.mp4 -c:v prores_videotoolbox -profile:v lt -c:a pcm_s16le plt_vt.mov
  timed "ProRes Proxy 1080p (videotoolbox)" "$FFMPEG" -y -hwaccel videotoolbox -i s1080.mp4 -c:v prores_videotoolbox -profile:v proxy -c:a pcm_s16le ppx_vt.mov
fi
timed "ProRes 422 LT 1080p (prores_ks, Windows 경로)" "$FFMPEG" -y -i s1080.mp4 -c:v prores_ks -profile:v 1 -c:a pcm_s16le plt_ks.mov
timed "AAC → WAV 48k" "$FFMPEG" -y -i s1080.mp4 -vn -c:a pcm_s16le -ar 48000 a.wav
echo "## 크기"
for f in r1080.mp4 r2160.mp4 p720.mp4 plt_vt.mov ppx_vt.mov plt_ks.mov a.wav; do [ -f "$f" ] && echo "$f: $(mb "$f")"; done

export DUR FFMPEG
echo "## 임의 위치 탐색(정확 탐색 1프레임 디코드, 중앙값 ms)"
python3 - <<'EOF'
import os, statistics, subprocess, time
ff = os.environ.get("FFMPEG", "ffmpeg")
gops = range(1, max(2, int(os.environ.get("DUR", "60")) // 2 - 1))  # 원본 길이 안의 GOP만
def seek(f, t):
    s = time.perf_counter()
    subprocess.run([ff, "-v", "error", "-ss", f"{t:.4f}", "-i", f, "-frames:v", "1", "-f", "null", "-"], check=True)
    return (time.perf_counter() - s) * 1000
for name, f in [("H.264 1080p GOP 2s", "r1080.mp4"), ("H.264 2160p GOP 2s", "r2160.mp4"), ("ProRes LT 1080p", "plt_ks.mov")]:
    seek(f, 2.0)
    key = statistics.median(seek(f, 2.0 * k) for k in gops)
    worst = statistics.median(seek(f, 2.0 * k + 59 / 30) for k in gops)
    print(f"{name}: 키프레임 {key:.0f} / GOP 끝 {worst:.0f} / 추가 {worst - key:.0f}")
EOF

if [ "${1:-}" = --online ]; then
  : "${YTDLP:?YTDLP=yt-dlp 경로}"
  JS=""; [ -n "${DENO:-}" ] && JS="--js-runtimes deno:$DENO"
  U=${URL:-https://www.youtube.com/watch?v=jNQXAC9IVRw}
  echo "## 조회 재사용 ($U)"
  # shellcheck disable=SC2086
  timed "조회(-J)" sh -c "'$YTDLP' --ignore-config -q $JS -J '$U' > info.json"
  # shellcheck disable=SC2086
  timed "다운로드(URL에서 재추출)" "$YTDLP" --ignore-config -q $JS -f "bv*[height<=360]+ba/b" --merge-output-format mp4 -o d1.mp4 "$U"
  # shellcheck disable=SC2086
  timed "다운로드(--load-info-json)" "$YTDLP" --ignore-config -q $JS --load-info-json info.json -f "bv*[height<=360]+ba/b" --merge-output-format mp4 -o d2.mp4
fi
