#!/usr/bin/env bash
# Windows x64 개인용 CEP 배포 ZIP을 만든다.
# - Helper/panel을 빌드하고 디버그 파일을 제외한다.
# - 고정한 FFmpeg GPLv3 정적 빌드를 SHA-256 검증 후 ffmpeg/ffprobe만 번들한다.
# - 설치 전 파일 checksum을 확인하는 PowerShell 설치기를 포함한다.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
panel="$root/panel/cep"
packaging="$root/packaging/windows"
artifact_dir="$root/artifacts/windows"
cache_dir="$panel/.package-cache"

ffmpeg_version="8.1.2"
ffmpeg_archive="ffmpeg-${ffmpeg_version}-essentials_build.zip"
ffmpeg_url="https://www.gyan.dev/ffmpeg/builds/packages/$ffmpeg_archive"
ffmpeg_sha256="db580001caa24ac104c8cb856cd113a87b0a443f7bdf47d8c12b1d740584a2ec"
cached_archive="$cache_dir/$ffmpeg_archive"

for command_name in curl go npm node shasum unzip zip; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "required command not found: $command_name" >&2
    exit 1
  fi
done

version="$(node -p "require('$panel/package.json').version")"
work="$(mktemp -d "${TMPDIR:-/tmp}/birdcage-windows-package.XXXXXX")"
cleanup() {
  rm -rf "$work"
}
trap cleanup EXIT

mkdir -p "$cache_dir" "$artifact_dir"
if [ ! -f "$cached_archive" ] || [ "$(shasum -a 256 "$cached_archive" | awk '{print $1}')" != "$ffmpeg_sha256" ]; then
  partial="$cached_archive.partial"
  rm -f "$partial"
  echo "downloading $ffmpeg_archive"
  curl -fL --retry 3 --progress-bar "$ffmpeg_url" -o "$partial"
  actual="$(shasum -a 256 "$partial" | awk '{print $1}')"
  if [ "$actual" != "$ffmpeg_sha256" ]; then
    rm -f "$partial"
    echo "FFmpeg checksum mismatch: expected $ffmpeg_sha256, got $actual" >&2
    exit 1
  fi
  mv "$partial" "$cached_archive"
fi
(
  cd "$cache_dir"
  echo "$ffmpeg_sha256  $ffmpeg_archive" | shasum -a 256 -c -
)

BIRDCAGE_VERSION="$version" "$root/scripts/build-helper.sh" win
npm --prefix "$panel" ci --no-audit --no-fund
npm --prefix "$panel" run -s build

release_name="Birdcage-Windows-x64-$version"
release_root="$work/$release_name"
extension_root="$release_root/com.birdcage.panel"
mkdir -p "$release_root" "$extension_root"
cp -R "$panel/dist/cep/." "$extension_root/"
rm -f "$extension_root/.debug"
find "$extension_root" -type f -name '*.map' -delete
rm -rf "$extension_root/bin/darwin"

extract_root="$work/ffmpeg"
mkdir -p "$extract_root"
unzip -q "$cached_archive" -d "$extract_root"
ffmpeg_root="$extract_root/ffmpeg-${ffmpeg_version}-essentials_build"
for executable in ffmpeg ffprobe; do
  source_exe="$ffmpeg_root/bin/$executable.exe"
  if [ ! -f "$source_exe" ]; then
    echo "FFmpeg archive is missing bin/$executable.exe" >&2
    exit 1
  fi
  bundled_dir="$extension_root/bin/win32-x64/tools/$executable"
  mkdir -p "$bundled_dir"
  cp "$source_exe" "$bundled_dir/$executable.exe"
done

mkdir -p "$release_root/licenses/ffmpeg"
if [ -f "$ffmpeg_root/LICENSE" ]; then
  cp "$ffmpeg_root/LICENSE" "$release_root/licenses/ffmpeg/LICENSE.txt"
fi
if [ -f "$ffmpeg_root/README.txt" ]; then
  cp "$ffmpeg_root/README.txt" "$release_root/licenses/ffmpeg/BUILD-README.txt"
fi
cp "$panel/THIRD_PARTY_LICENSE_bolt-cep.txt" "$release_root/licenses/THIRD_PARTY_LICENSE_bolt-cep.txt"
cp "$packaging/install.ps1" "$packaging/uninstall.ps1" "$packaging/install.cmd" "$packaging/uninstall.cmd" \
  "$packaging/README-ko.txt" "$packaging/FFMPEG_SOURCE.txt" "$release_root/"

(
  cd "$release_root"
  find com.birdcage.panel -type f | LC_ALL=C sort | while IFS= read -r file; do
    hash="$(shasum -a 256 "$file" | awk '{print $1}')"
    printf '%s  %s\n' "$hash" "$file"
  done > SHA256SUMS.txt
)

zip_path="$artifact_dir/$release_name.zip"
rm -f "$zip_path"
(
  cd "$work"
  zip -q -r -9 "$zip_path" "$release_name"
)
unzip -tq "$zip_path" >/dev/null

zip_hash="$(shasum -a 256 "$zip_path" | awk '{print $1}')"
zip_size="$(du -h "$zip_path" | awk '{print $1}')"
echo "created: $zip_path"
echo "size:    $zip_size"
echo "sha256:  $zip_hash"
