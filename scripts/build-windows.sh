#!/usr/bin/env bash
# Windows build for Unix-like environments (Linux, macOS).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"

app_version="$(bash "${root}/scripts/app-version.sh")"
echo "==> Jorbique VPN Windows build (version: ${app_version})"

if [ "${SKIP_FRONTEND:-0}" != "1" ]; then
  echo "==> frontend"
  rm -rf "${root}/frontend/dist"
  (cd frontend && npm ci --no-audit --no-fund && npm run build)
else
  test -d "${root}/frontend/dist"
fi

# frontend/dist is a build artifact and is gitignored, so switching to a branch
# from before the Vue migration leaves that branch's app.js and index.html
# behind — and a later build overwrites the assets but not the entry point, so
# the package ships the old UI with the new binary. The pre-Vue bundle had an
# app.js at the top of dist; the Vite one never does.
if [ -e "${root}/frontend/dist/app.js" ]; then
  echo "ERROR: frontend/dist holds a pre-Vue bundle (app.js). Run: rm -rf frontend/dist" >&2
  exit 1
fi

if command -v node >/dev/null 2>&1; then
  echo "==> version metadata"
  APP_VERSION="${app_version}" node "${root}/scripts/bump-windows-version.mjs"
else
  echo "WARN: node not found; skipping wails.json/winres.json version metadata bump." >&2
fi

# sing-box is NOT bundled or embedded: the client downloads it from the update
# server on first launch (with a progress bar). No sing-box binary ships in the
# .exe or lives in the repo.
tmp_bundle="$(mktemp -d)"
trap 'rm -rf "${tmp_bundle}"' EXIT

echo "==> Windows resources"
if [ -f "${root}/winres/icon.ico" ]; then
  cp -f "${root}/winres/icon.ico" "${root}/internal/tray/tray_icon.ico"
fi
go run "github.com/tc-hib/go-winres@${GO_WINRES_VERSION:-v0.3.3}" make --out "cmd/jorbiquevpn/rsrc"

out_dir="${root}/release/binaries"
win_exe_name="Jorbique-VPN.exe"
win_zip_name="Jorbique-VPN-windows.zip"
mkdir -p "${out_dir}"

echo "==> go build"
# 'production' is required by Wails itself (dev vs production runtime); it no longer
# embeds sing-box (that file was removed) — the client downloads it on first launch.
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -tags desktop,production \
  -ldflags "-w -s -H windowsgui -X jorbiquevpn/internal/constants.AppVersion=${app_version}${SERVER_HOST:+ -X jorbiquevpn/internal/constants.ServerHost=${SERVER_HOST}}" \
  -o "${out_dir}/${win_exe_name}" ./cmd/jorbiquevpn

cp -f "${out_dir}/${win_exe_name}" "${tmp_bundle}/${win_exe_name}"
(cd "${tmp_bundle}" && zip -q "${win_zip_name}" "${win_exe_name}")
mv -f "${tmp_bundle}/${win_zip_name}" "${out_dir}/${win_zip_name}"

echo "Built: ${out_dir}/${win_zip_name} (${win_exe_name} inside)"
