#!/usr/bin/env bash
# macOS client build. The menu-bar UI is pure Go (purego) — no Objective-C, no
# cgo — so everything cross-compiles from Linux or macOS with CGO_ENABLED=0.
#   ./build-macos.sh install   — build as current user, install under /usr/local/libexec/jorbique-vpn (sudo only when needed)
#   ./build-macos.sh package   — build the macOS binary (CGO_ENABLED=0) and produce the .pkg
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cmd="${1:-install}"

if [ "${SKIP_FRONTEND:-0}" != "1" ]; then
  command -v npm >/dev/null 2>&1 || { echo "ERROR: npm is required to build the UI." >&2; exit 1; }
  rm -rf "${root}/frontend/dist"
  (cd "${root}/frontend" && npm ci --no-audit --no-fund && npm run build)
else
  test -f "${root}/frontend/dist/index.html" || { echo "ERROR: frontend/dist is missing." >&2; exit 1; }
fi

# find_lipo prints a lipo-compatible tool for combining Mach-O slices into a
# universal binary: Apple's lipo (macOS Xcode CLT) or LLVM's llvm-lipo (Linux CI,
# from the `llvm` package), including Debian's version-suffixed llvm-lipo-NN.
# Returns non-zero when none is available.
find_lipo() {
  if command -v lipo >/dev/null 2>&1; then echo lipo; return 0; fi
  if command -v llvm-lipo >/dev/null 2>&1; then echo llvm-lipo; return 0; fi
  local cand
  cand="$(ls /usr/bin/llvm-lipo-* /usr/lib/llvm-*/bin/llvm-lipo 2>/dev/null | sort -V | tail -n1 || true)"
  if [ -n "${cand}" ] && [ -x "${cand}" ]; then echo "${cand}"; return 0; fi
  return 1
}

# resize_png scales src to px×px with whichever tool the machine has.
resize_png() {
  local src="$1" dst="$2" px="$3"
  if command -v magick >/dev/null 2>&1; then
    magick "${src}" -resize "${px}x${px}!" "${dst}"
  elif command -v convert >/dev/null 2>&1; then
    convert "${src}" -resize "${px}x${px}!" "${dst}"
  elif command -v sips >/dev/null 2>&1; then
    sips -z "${px}" "${px}" "${src}" --out "${dst}" >/dev/null
  else
    return 1
  fi
}

# build_jorbique_app_icon_icns_to writes AppIcon.icns from the icon master
# (scripts/branding/app-icon.png). Returns 0 if ${icns_dest} exists.
#
# The Dock draws an icon at the size of its artwork, and macOS's own icons fill
# about 80% of their canvas: the master is drawn edge to edge, so it is scaled
# down onto a transparent 1024 canvas first. JORBIQUE_ICON_INSET is the margin
# per side in pixels — a knob, because "matches the neighbours" is judged by eye.
build_jorbique_app_icon_icns_to() {
  local icns_dest="$1"
  local master="${root}/scripts/branding/app-icon.png"
  local inset="${JORBIQUE_ICON_INSET:-100}"
  local tile=$((1024 - 2 * inset))
  rm -f "${icns_dest}"
  [ -f "${master}" ] || return 1
  local work
  work="$(mktemp -d)"
  local padded="${work}/padded.png"
  if command -v magick >/dev/null 2>&1; then
    magick "${master}" -resize "${tile}x${tile}" -background none -gravity center -extent 1024x1024 "${padded}"
  elif command -v convert >/dev/null 2>&1; then
    convert "${master}" -resize "${tile}x${tile}" -background none -gravity center -extent 1024x1024 "${padded}"
  elif command -v sips >/dev/null 2>&1; then
    sips -z "${tile}" "${tile}" "${master}" --out "${padded}" >/dev/null &&
      sips --padToHeightWidth 1024 1024 "${padded}" >/dev/null
  else
    rm -rf "${work}"
    return 1
  fi
  local sz
  for sz in 16 32 64 128 256 512 1024; do
    resize_png "${padded}" "${work}/i${sz}.png" "${sz}" || { rm -rf "${work}"; return 1; }
  done
  if command -v png2icns >/dev/null 2>&1; then
    png2icns "${icns_dest}" \
      "${work}/i16.png" "${work}/i32.png" "${work}/i128.png" \
      "${work}/i256.png" "${work}/i512.png" "${work}/i1024.png" >/dev/null 2>&1 || rm -f "${icns_dest}"
  fi
  if [ ! -f "${icns_dest}" ] && command -v iconutil >/dev/null 2>&1; then
    local icset="${work}/AppIcon.iconset"
    mkdir -p "${icset}"
    cp -f "${work}/i16.png" "${icset}/icon_16x16.png"
    cp -f "${work}/i32.png" "${icset}/icon_16x16@2x.png"
    cp -f "${work}/i32.png" "${icset}/icon_32x32.png"
    cp -f "${work}/i64.png" "${icset}/icon_32x32@2x.png"
    cp -f "${work}/i128.png" "${icset}/icon_128x128.png"
    cp -f "${work}/i256.png" "${icset}/icon_128x128@2x.png"
    cp -f "${work}/i256.png" "${icset}/icon_256x256.png"
    cp -f "${work}/i512.png" "${icset}/icon_256x256@2x.png"
    cp -f "${work}/i512.png" "${icset}/icon_512x512.png"
    cp -f "${work}/i1024.png" "${icset}/icon_512x512@2x.png"
    iconutil -c icns -o "${icns_dest}" "${icset}" 2>/dev/null || rm -f "${icns_dest}"
  fi
  rm -rf "${work}"
  [ -f "${icns_dest}" ]
}

# go_ldflags is what every macOS build links with: the version, and the server
# host when a build targets another one (SERVER_HOST).
go_ldflags() {
  local version="$1"
  printf '%s' "-s -w -X jorbiquevpn/internal/constants.AppVersion=${version}${SERVER_HOST:+ -X jorbiquevpn/internal/constants.ServerHost=${SERVER_HOST}}"
}

package_bundle() {
  # Same resolver the Windows build uses, so both clients built from one commit
  # report the same version (see scripts/app-version.sh).
  local app_version
  app_version="$(bash "${root}/scripts/app-version.sh")"
  local source_dir="${root}/internal/macos"
  local binary="${source_dir}/JorbiqueVPN"
  # Target architectures for the client binary. Default to a universal2 build
  # (arm64 + amd64) so a single .pkg runs natively on both Apple Silicon and
  # Intel Macs with no Rosetta. Each slice carries its own runtime.GOARCH, so the
  # server-downloaded sing-box (internal/update: darwin-arm64 / darwin-amd64) is
  # picked correctly by whichever slice macOS executes. Set MACOS_ARCH to a single
  # value (arm64 or amd64) to force one slice; space-separated values are honored.
  local arches="${MACOS_ARCH:-arm64 amd64}"
  echo "==> Packaging macOS client (version: ${app_version}, arches: ${arches})"
  mkdir -p "${source_dir}"
  # Always rebuild from source: a stale binary left from a previous run would be
  # silently repackaged. Set JORBIQUE_VPN_REUSE_PREBUILT=1 to reuse an externally
  # built binary (e.g. a separate CI build stage).
  if [ "${JORBIQUE_VPN_REUSE_PREBUILT:-0}" = "1" ] && [ -f "${binary}" ]; then
    echo "==> Reusing prebuilt macOS binary (JORBIQUE_VPN_REUSE_PREBUILT=1): ${binary}"
  else
    local slices=() arch lipo_check
    for arch in ${arches}; do
      echo "==> Building macOS binary (CGO_ENABLED=0, GOOS=darwin, GOARCH=${arch})..."
      CGO_ENABLED=0 GOOS=darwin GOARCH="${arch}" \
        go build -trimpath -ldflags="$(go_ldflags "${app_version}")" -o "${binary}.${arch}" ./cmd/jorbiquevpn-macos
      slices+=("${binary}.${arch}")
    done
    if [ "${#slices[@]}" -gt 1 ]; then
      local lipo_bin
      if ! lipo_bin="$(find_lipo)"; then
        echo "ERROR: a universal (multi-arch) build needs 'lipo' or 'llvm-lipo', and neither was found." >&2
        echo "       Install Xcode Command Line Tools (macOS) or the 'llvm' package (Linux)," >&2
        echo "       or set MACOS_ARCH=arm64 (or amd64) for a single-arch build." >&2
        rm -f "${slices[@]}"
        exit 1
      fi
      echo "==> Combining ${#slices[@]} slices into a universal binary (${lipo_bin})..."
      "${lipo_bin}" -create -output "${binary}" "${slices[@]}"
      rm -f "${slices[@]}"
    else
      mv -f "${slices[0]}" "${binary}"
    fi
    chmod 755 "${binary}"
    if lipo_check="$(find_lipo)"; then
      echo "✓ Built: ${binary} (CGO=0; archs: $(${lipo_check} -archs "${binary}" 2>/dev/null || echo "${arches}"))"
    else
      echo "✓ Built: ${binary} (CGO=0, arches: ${arches})"
    fi
  fi

  # Apple Silicon refuses to execute an unsigned binary, so sign it here. On
  # macOS use codesign; on a Linux runner use rcodesign (indygreg/apple-codesign),
  # which signs Mach-O without Apple tooling. A Developer-ID identity is used when
  # its .p12 material is provided; otherwise the signature is ad-hoc (enough to
  # launch locally, not for Gatekeeper distribution).
  if [ "$(uname -s)" = "Darwin" ] && command -v codesign >/dev/null 2>&1; then
    codesign --force --sign - "${binary}"
    echo "✓ ad-hoc signed (codesign)"
  elif command -v rcodesign >/dev/null 2>&1; then
    if [ -n "${APPLE_CODESIGN_P12:-}" ] && [ -n "${APPLE_CODESIGN_P12_PASSWORD:-}" ]; then
      # Pass the P12 password via a 0600 temp file so the secret never appears in
      # the process argument list (`ps aux`). Clean it up on both success and failure.
      p12_pw_file="$(mktemp)"
      chmod 600 "$p12_pw_file"
      printf '%s' "${APPLE_CODESIGN_P12_PASSWORD}" > "$p12_pw_file"
      rcodesign sign --p12-file "${APPLE_CODESIGN_P12}" --p12-password-file "$p12_pw_file" \
        "${binary}" || { rm -f "$p12_pw_file"; exit 1; }
      rm -f "$p12_pw_file"
      echo "✓ Developer-ID signed (rcodesign)"
    else
      rcodesign sign "${binary}"
      echo "✓ ad-hoc signed (rcodesign)"
    fi
  else
    echo "WARN: neither codesign nor rcodesign found — the arm64 binary is unsigned and will not launch on Apple Silicon." >&2
  fi

  # sing-box is NOT bundled: the client downloads it from the update server on
  # first launch (with a progress window).

  local out_dir="${root}/release/binaries"
  mkdir -p "${out_dir}"

  # Apple Installer validates pkg-info @version and bundle CFBundleVersion — numeric x.y.z only.
  local pkg_semver="0.0.1"
  if [[ "${app_version}" =~ ^([0-9]+\.[0-9]+\.[0-9]+) ]]; then
    pkg_semver="${BASH_REMATCH[1]}"
  elif [[ "${app_version}" =~ ^([0-9]+\.[0-9]+) ]]; then
    pkg_semver="${BASH_REMATCH[1]}.0"
  fi

  # Installer .pkg: the binary under /usr/local/libexec/jorbique-vpn, data under
  # /Library, and /Applications/Jorbique VPN Extended.app, whose launcher runs the
  # installed binary.
  local macos_app_bundle_name="Jorbique VPN Extended.app"
  # The bundle holds two executables. CFBundleExecutable is the launcher script,
  # which is what Finder runs; next to it is JorbiqueVPN, a symlink to the
  # installed binary. Everything that starts the menu bar goes through the
  # symlink: a process whose executable path is inside Contents/MacOS gets the
  # bundle as its main bundle, which is what puts the app's icon on the window
  # in Stage Manager and Mission Control. Go resolves the symlink away
  # (core.resolveExecutable), so the installed layout under libexec is unchanged.
  local macos_app_bundle_exe="JorbiqueVPN-launcher"
  local macos_app_bundle_bin="JorbiqueVPN"
  local macos_app_bundle_rel="Applications/${macos_app_bundle_name}"
  local pkg_path="${out_dir}/Jorbique-VPN-Extended-macOS.pkg"
  rm -f "${pkg_path}"
  local pkg_work
  pkg_work="$(mktemp -d)"
  local pkgroot="${pkg_work}/root"
  local libexec="${pkgroot}/usr/local/libexec/jorbique-vpn"
  mkdir -p "${libexec}" \
    "${pkgroot}/Library/Application Support/JorbiqueVPN/cfg" \
    "${pkgroot}/Library/Logs/JorbiqueVPN" \
    "${pkgroot}/Library/Caches/JorbiqueVPN" \
    "${pkgroot}/${macos_app_bundle_rel}/Contents/MacOS" \
    "${pkgroot}/${macos_app_bundle_rel}/Contents/Resources"
  cp -f "${binary}" "${libexec}/JorbiqueVPN"
  chmod 755 "${libexec}/JorbiqueVPN"
  # The upgrade/rollback tool ships with the client it manages. A rollback is
  # needed exactly when the machine has a bad build on it, which is the worst
  # moment to be told to clone a repository first.
  cp -f "${root}/scripts/pkg/jorbique-vpn-admin.sh" "${libexec}/jorbique-vpn-admin"
  chmod 755 "${libexec}/jorbique-vpn-admin"
  if compgen -G "${source_dir}/cfg/*.json" >/dev/null; then
    cp -f "${source_dir}/cfg"/*.json "${pkgroot}/Library/Application Support/JorbiqueVPN/cfg/"
  fi
  cp -f "${root}/scripts/pkg/JorbiqueVPN-launcher.sh" "${pkgroot}/${macos_app_bundle_rel}/Contents/MacOS/${macos_app_bundle_exe}"
  chmod 755 "${pkgroot}/${macos_app_bundle_rel}/Contents/MacOS/${macos_app_bundle_exe}"
  ln -sfn "/usr/local/libexec/jorbique-vpn/JorbiqueVPN" "${pkgroot}/${macos_app_bundle_rel}/Contents/MacOS/${macos_app_bundle_bin}"

  local icns_dest="${pkgroot}/${macos_app_bundle_rel}/Contents/Resources/AppIcon.icns"
  local have_icon=0
  if build_jorbique_app_icon_icns_to "${icns_dest}"; then
    have_icon=1
    echo "✓ App icon: ${icns_dest}"
  else
    echo "WARN: Could not build AppIcon.icns (need sips/iconutil on macOS, or ImageMagick and png2icns)." >&2
  fi

  local plist="${pkgroot}/${macos_app_bundle_rel}/Contents/Info.plist"
  # CFBundleIconFile only when an .icns was actually produced: a key pointing at
  # a file that is not in Resources leaves the bundle with the generic icon and
  # no way to tell why.
  local icon_key=""
  if [ "${have_icon}" -eq 1 ]; then
    icon_key=$'  <key>CFBundleIconFile</key>\n  <string>AppIcon</string>'
  fi
  cat > "${plist}" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key>
  <string>${macos_app_bundle_exe}</string>
  <key>CFBundleIdentifier</key>
  <string>one.jorbique.vpn</string>
  <key>CFBundleName</key>
  <string>Jorbique VPN Extended</string>
  <key>CFBundleDisplayName</key>
  <string>Jorbique VPN Extended</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>${app_version}</string>
  <key>CFBundleVersion</key>
  <string>${pkg_semver}</string>
${icon_key:+${icon_key}
}  <key>LSMinimumSystemVersion</key>
  <string>11.0</string>
  <key>NSHighResolutionCapable</key>
  <true/>
  <!-- No LSUIElement: the client has a window of its own, and a window with no
       Dock icon cannot be minimised anywhere or brought back. The status item is
       installed either way — the policy decides whether there is also an icon. -->
</dict>
</plist>
EOF

  if [ "${have_icon}" -eq 1 ]; then
    cp -f "${icns_dest}" "${libexec}/AppIcon.icns"
    echo "✓ AppIcon.icns also staged next to the binary (Dock icon for CLI-launched binary)"
  fi

  chmod 644 "${plist}"
  echo "==> Creating macOS installer package (.pkg)…"
  MACOS_APP_BUNDLE_REL="./${macos_app_bundle_rel}" \
    bash "${root}/scripts/pkg/build-flat-pkg.sh" "${pkgroot}" "${pkg_path}" "${pkg_semver}" "Jorbique VPN Extended ${app_version}" "${root}"
  echo "✓ Built: ${pkg_path}"
  rm -rf "${pkg_work}"

  if [ -n "${CI:-}" ] && [ ! -f "${pkg_path}" ]; then
    echo "ERROR: CI expected ${pkg_path} (see scripts/pkg/build-flat-pkg.sh; Linux needs xar and mkbom)." >&2
    exit 1
  fi
}

if [ "$cmd" = "package" ]; then
  package_bundle
  exit 0
fi

if [ "$cmd" != "install" ]; then
  echo "Usage: $0 install | $0 package" >&2
  exit 1
fi

if [[ $EUID -eq 0 ]]; then
  echo "ERROR: Do not run this script via sudo." >&2
  echo "Run as regular user: $0 install" >&2
  exit 1
fi

# A developer install: the same layout the package uses, without the package.
# sing-box is not built here — the client downloads the server's build on first
# launch, the same way an installed one does.
INSTALL_DIR="/usr/local/libexec/jorbique-vpn"
PATH_LINK_DIR="/usr/local/bin"

run_privileged() {
  if command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    "$@"
  fi
}

echo "==> Building JorbiqueVPN (Go)..."
cd "${root}"
# Pure-Go (purego) build: pin CGO_ENABLED=0 so a Mac with Xcode installed does
# not silently compile the cgo variants instead, producing a binary that differs
# from what the .pkg ships.
CGO_ENABLED=0 go build -trimpath -ldflags="$(go_ldflags "$(bash "${root}/scripts/app-version.sh")")" -o JorbiqueVPN ./cmd/jorbiquevpn-macos
echo "✓ JorbiqueVPN built"

echo "==> Installing JorbiqueVPN to ${INSTALL_DIR}..."
run_privileged mkdir -p "${INSTALL_DIR}"
run_privileged cp -f JorbiqueVPN "${INSTALL_DIR}/JorbiqueVPN"
run_privileged xattr -cr "${INSTALL_DIR}/JorbiqueVPN"
run_privileged codesign --force --sign - "${INSTALL_DIR}/JorbiqueVPN"
echo "✓ JorbiqueVPN installed to ${INSTALL_DIR}/JorbiqueVPN"

ic_tmp_install="$(mktemp -t jorbiqueicns).icns"
if build_jorbique_app_icon_icns_to "${ic_tmp_install}"; then
  run_privileged cp -f "${ic_tmp_install}" "${INSTALL_DIR}/AppIcon.icns"
  run_privileged chmod 644 "${INSTALL_DIR}/AppIcon.icns"
  echo "✓ AppIcon.icns → ${INSTALL_DIR}/AppIcon.icns (Dock icon beside CLI binary)"
fi
rm -f "${ic_tmp_install}"

# The same upgrade/rollback tool the package installs, so a developer install
# and a packaged one are managed the same way.
run_privileged cp -f "${root}/scripts/pkg/jorbique-vpn-admin.sh" "${INSTALL_DIR}/jorbique-vpn-admin"
run_privileged chmod 755 "${INSTALL_DIR}/jorbique-vpn-admin"

run_privileged mkdir -p "${PATH_LINK_DIR}"
run_privileged ln -sf "${INSTALL_DIR}/JorbiqueVPN" "${PATH_LINK_DIR}/jorbique-vpn"
run_privileged ln -sf "${INSTALL_DIR}/jorbique-vpn-admin" "${PATH_LINK_DIR}/jorbique-vpn-admin"
echo "✓ PATH symlinks: ${PATH_LINK_DIR}/jorbique-vpn, ${PATH_LINK_DIR}/jorbique-vpn-admin"

echo "Done. Start it with: sudo jorbique-vpn"
