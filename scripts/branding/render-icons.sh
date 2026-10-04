#!/usr/bin/env bash
# Resizes the app icon master (scripts/branding/app-icon.png, 1024px) into what
# the clients ship: winres/ (Windows exe), internal/tray/ (embedded systray) and
# assets/app-icon.png (the logo the UI draws). The .icns is built by
# build-macos.sh from the same master.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
master="${root}/scripts/branding/app-icon.png"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

if [[ ! -f "${master}" ]]; then
  echo "missing icon master: ${master}" >&2
  exit 1
fi

resize_png() {
  local src="$1" dst="$2" px="$3"
  if command -v magick >/dev/null 2>&1; then
    magick "${src}" -resize "${px}x${px}!" "${dst}"
  elif command -v convert >/dev/null 2>&1; then
    convert "${src}" -resize "${px}x${px}!" "${dst}"
  elif command -v sips >/dev/null 2>&1; then
    sips -z "${px}" "${px}" "${src}" --out "${dst}" >/dev/null
  else
    echo "need ImageMagick or macOS sips to resize the icon" >&2
    return 1
  fi
}

for px in 16 32 48 256; do
  resize_png "${master}" "${work}/i${px}.png" "${px}"
done

mkdir -p "${root}/winres" "${root}/internal/tray" "${root}/assets"
cp -f "${work}/i256.png" "${root}/winres/icon.png"
cp -f "${work}/i16.png" "${root}/winres/icon16.png"
cp -f "${work}/i256.png" "${root}/assets/app-icon.png"

go run "${root}/scripts/branding/png2ico/main.go" -o "${root}/winres/icon.ico" \
  "16:${work}/i16.png" "32:${work}/i32.png" "48:${work}/i48.png" "256:${work}/i256.png"

# The Windows build copies icon.ico over the tray icon too; the same file here
# keeps a build from rewriting a tracked file.
cp -f "${root}/winres/icon.ico" "${root}/internal/tray/tray_icon.ico"

echo "✓ Icons rendered from ${master} -> winres/, internal/tray/, assets/app-icon.png"
