#!/usr/bin/env bash
# Builds app/libs/core.aar: the Go engine in core/ with sing-box-extended in it,
# bound to Java by gomobile. Needs Go, Java 17 and the Android NDK
# (ANDROID_NDK_HOME), plus SERVER_HOST and SERVER_CHANNEL like every other
# build (see scripts/server-ldflags.sh).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/.." && pwd)"

# The engine version the server runs; the module itself is pinned in core/go.mod.
singbox_version="1.14.3-extended-2.7.3"
gomobile_version="v0.1.12"
# The protocols the server's profile uses, as the server and desktop builds
# select them, plus Naive.
tags="with_gvisor,with_quic,with_grpc,with_utls,with_sudoku,with_wireguard,with_clash_api,with_naive_outbound,badlinkname,tfogo_checklinkname0"

server_flags="$("${root}/scripts/server-ldflags.sh")"
app_version="$(tr -d '[:space:]' < "${root}/VERSION")"
ldflags="-X github.com/sagernet/sing-box/constant.Version=${singbox_version} -X runtime.godebugDefault=multipathtcp=0,tlssha1=1 -checklinkname=0 -s -w -buildid= ${server_flags} -X jorbiquevpn/internal/constants.AppVersion=${app_version}"

cd "${here}/core"
go install "github.com/sagernet/gomobile/cmd/gomobile@${gomobile_version}" "github.com/sagernet/gomobile/cmd/gobind@${gomobile_version}"
export PATH="$(go env GOPATH)/bin:${PATH}"
mkdir -p "${here}/app/libs"
gomobile bind -v \
  -target android/arm64 \
  -androidapi 29 \
  -javapkg com.jorbique.vpn \
  -trimpath -buildvcs=false \
  -tags "${tags}" \
  -ldflags "${ldflags}" \
  -o "${here}/app/libs/core.aar" \
  .
