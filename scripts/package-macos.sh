#!/usr/bin/env bash
# macOS 개인용 CEP 배포 ZIP을 만든다(Apple Silicon·Intel).
# - Helper(universal)·패널을 빌드하고 디버그 파일·Windows 실행 파일을 뺀다.
# - ffmpeg.org가 안내하는 evermeet.cx FFmpeg 8.1.2 정적 빌드(Intel)를 SHA-256 검증 후 ffmpeg/ffprobe만 번들한다.
#   Apple Silicon에서는 Rosetta로 돌고, 기기에 네이티브 ffmpeg가 있으면 Helper가 그쪽을 먼저 쓴다(tools.Locator.BundledLast).
# - 설치 전 checksum을 확인하고 격리 속성을 지우는 install.command를 넣는다.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
panel="$root/panel/cep"
packaging="$root/packaging/macos"
artifact_dir="$root/artifacts/macos"
cache_dir="$panel/.package-cache"

ffmpeg_version="8.1.2"
declare -a tools=(ffmpeg ffprobe)
sha_ffmpeg="e91df72a1ee7c26606f90dd2dd4dcccc6a75140ff9ea6fdd50faae828b82ba69"
sha_ffprobe="399b93f0b9862f69767afa343e90c2f48d7e7958cadbb6deb76a012d0e3b7ce3"

for command_name in curl go npm node shasum unzip zip lipo ditto; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "required command not found: $command_name" >&2
    exit 1
  fi
done

version="$(node -p "require('$panel/package.json').version")"
work="$(mktemp -d "${TMPDIR:-/tmp}/birdcage-macos-package.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir -p "$cache_dir" "$artifact_dir"

for tool in "${tools[@]}"; do
  sha_var="sha_$tool"
  want="${!sha_var}"
  archive="$cache_dir/$tool-$ffmpeg_version-macos-x86_64.zip"
  if [ ! -f "$archive" ] || [ "$(shasum -a 256 "$archive" | awk '{print $1}')" != "$want" ]; then
    echo "downloading $tool $ffmpeg_version (evermeet.cx)"
    curl -fL --retry 3 --progress-bar "https://evermeet.cx/ffmpeg/$tool-$ffmpeg_version.zip" -o "$archive.partial"
    got="$(shasum -a 256 "$archive.partial" | awk '{print $1}')"
    if [ "$got" != "$want" ]; then
      rm -f "$archive.partial"
      echo "$tool checksum mismatch: expected $want, got $got" >&2
      exit 1
    fi
    mv "$archive.partial" "$archive"
  fi
done

BIRDCAGE_VERSION="$version" "$root/scripts/build-helper.sh" mac
npm --prefix "$panel" ci --no-audit --no-fund
npm --prefix "$panel" run -s build

release_name="Birdcage-macOS-$version"
release_root="$work/$release_name"
extension_root="$release_root/com.birdcage.panel"
mkdir -p "$extension_root"
ditto "$panel/dist/cep" "$extension_root"
rm -f "$extension_root/.debug"
find "$extension_root" -type f -name '*.map' -delete
rm -rf "$extension_root/bin/win32-x64"

helper="$extension_root/bin/darwin/birdcage-helper"
archs="$(lipo -archs "$helper")"
case "$archs" in *arm64*x86_64*|*x86_64*arm64*) ;; *) echo "helper is not universal: $archs" >&2; exit 1 ;; esac

for tool in "${tools[@]}"; do
  dir="$extension_root/bin/darwin/tools/$tool"
  mkdir -p "$dir"
  unzip -q -o "$cache_dir/$tool-$ffmpeg_version-macos-x86_64.zip" "$tool" -d "$dir"
  chmod 755 "$dir/$tool"
  "$dir/$tool" -hide_banner -version | head -1 | grep -q "version $ffmpeg_version" || { echo "$tool does not report $ffmpeg_version" >&2; exit 1; }
done

mkdir -p "$release_root/licenses/ffmpeg"
cp "$packaging/GPL-3.0.txt" "$release_root/licenses/ffmpeg/GPL-3.0.txt"
cp "$panel/THIRD_PARTY_LICENSE_bolt-cep.txt" "$release_root/licenses/THIRD_PARTY_LICENSE_bolt-cep.txt"
cp "$packaging/install.command" "$packaging/uninstall.command" "$packaging/README-ko.txt" "$packaging/FFMPEG_SOURCE.txt" "$release_root/"
chmod 755 "$release_root/install.command" "$release_root/uninstall.command"

(
  cd "$release_root"
  find com.birdcage.panel -type f | LC_ALL=C sort | while IFS= read -r file; do
    printf '%s  %s\n' "$(shasum -a 256 "$file" | awk '{print $1}')" "$file"
  done > SHA256SUMS.txt
)

zip_path="$artifact_dir/$release_name.zip"
rm -f "$zip_path"
(cd "$work" && zip -q -r -9 -y "$zip_path" "$release_name")
unzip -tq "$zip_path" >/dev/null

echo "created: $zip_path"
echo "size:    $(du -h "$zip_path" | awk '{print $1}')"
echo "sha256:  $(shasum -a 256 "$zip_path" | awk '{print $1}')"
