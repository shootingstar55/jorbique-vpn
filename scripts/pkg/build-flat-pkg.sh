#!/usr/bin/env bash
# Build installable Jorbique-VPN-Extended-macOS.pkg from a staging tree that mirrors absolute paths under /.
#   pkgroot/usr/local/libexec/jorbique-vpn/...
#   pkgroot/Applications/Jorbique VPN Extended.app/...
# On macOS prefers pkgbuild; on Linux builds a flat package (xar + Payload + Bom) per bomutils tutorial.
set -euo pipefail

usage() {
  echo "Usage: $0 <pkgroot_dir> <output.pkg> <version> <installer_title> <repo_root>" >&2
  exit 2
}

[[ "${#}" -eq 5 ]] || usage
PKGROOT="$(cd "$1" && pwd)"
OUT_PKG="$(cd "$(dirname "$2")" && pwd)/$(basename "$2")"
VERSION="$3"
# Defensive: Apple pkg-info @version must be numeric semver.
if [[ "${VERSION}" =~ ^([0-9]+\.[0-9]+\.[0-9]+) ]]; then
  VERSION="${BASH_REMATCH[1]}"
elif [[ "${VERSION}" =~ ^([0-9]+\.[0-9]+) ]]; then
  VERSION="${BASH_REMATCH[1]}.0"
else
  VERSION="0.0.1"
fi
TITLE="$4"
REPO_ROOT="$(cd "$5" && pwd)"

POSTINSTALL_SRC="${REPO_ROOT}/scripts/pkg/postinstall"
PREINSTALL_SRC="${REPO_ROOT}/scripts/pkg/preinstall"
[[ -f "${POSTINSTALL_SRC}" ]] || { echo "Missing ${POSTINSTALL_SRC}" >&2; exit 1; }
[[ -f "${PREINSTALL_SRC}" ]] || { echo "Missing ${PREINSTALL_SRC}" >&2; exit 1; }
[[ -d "${PKGROOT}/usr/local/libexec/jorbique-vpn" ]] || { echo "pkgroot must contain usr/local/libexec/jorbique-vpn" >&2; exit 1; }

rm -f "${OUT_PKG}"

if [[ "$(uname -s)" == "Darwin" ]] && command -v pkgbuild >/dev/null 2>&1; then
  echo "==> Creating .pkg with pkgbuild (macOS)…"
  SCRIPTS="$(mktemp -d)"
  trap 'rm -rf "${SCRIPTS}"' EXIT
  cp -f "${PREINSTALL_SRC}" "${SCRIPTS}/preinstall"
  cp -f "${POSTINSTALL_SRC}" "${SCRIPTS}/postinstall"
  chmod 755 "${SCRIPTS}/preinstall" "${SCRIPTS}/postinstall"
  pkgbuild \
    --identifier "one.jorbique.vpn.pkg" \
    --version "${VERSION}" \
    --install-location / \
    --root "${PKGROOT}" \
    --scripts "${SCRIPTS}" \
    --min-os-version "11.0" \
    "${OUT_PKG}"
  echo "✓ Built: ${OUT_PKG}"
  exit 0
fi

echo "==> Creating flat .pkg (xar + Payload + Bom, Linux-friendly)…"
command -v mkbom >/dev/null 2>&1 || { echo "ERROR: mkbom not found (build https://github.com/hogliux/bomutils or install mkbom)." >&2; exit 1; }
command -v xar >/dev/null 2>&1 || { echo "ERROR: xar not found." >&2; exit 1; }
if ! { cpio --help 2>&1 || true; } | grep -qi gnu; then
  echo "ERROR: GNU cpio is required for a macOS-compatible Payload (e.g. Debian package: cpio)." >&2
  exit 1
fi

FLAT="$(mktemp -d)"
SCRIPTS_DIR="$(mktemp -d)"
trap 'rm -rf "${FLAT}" "${SCRIPTS_DIR}"' EXIT
mkdir -p "${FLAT}/base.pkg"

NUM_FILES="$(find "${PKGROOT}" | wc -l | tr -d ' ')"
INSTALL_KB="$(du -sk "${PKGROOT}" | awk '{print $1}')"

cp -f "${PREINSTALL_SRC}" "${SCRIPTS_DIR}/preinstall"
cp -f "${POSTINSTALL_SRC}" "${SCRIPTS_DIR}/postinstall"
chmod 755 "${SCRIPTS_DIR}/preinstall" "${SCRIPTS_DIR}/postinstall"

# Payload: GNU cpio supports --owner 0:80 (root:wheel).
( cd "${PKGROOT}" && find . | LC_ALL=C sort | cpio -o --format odc --owner 0:80 | gzip -c ) > "${FLAT}/base.pkg/Payload"

( cd "${SCRIPTS_DIR}" && find . | LC_ALL=C sort | cpio -o --format odc --owner 0:80 | gzip -c ) > "${FLAT}/base.pkg/Scripts"

mkbom -u 0 -g 80 "${PKGROOT}" "${FLAT}/base.pkg/Bom"

# PackageInfo: bundle entry must match .app Info.plist CFBundleIdentifier / CFBundleVersion.
MACOS_APP_BUNDLE_REL="${MACOS_APP_BUNDLE_REL:-./Applications/Jorbique VPN Extended.app}"
cat > "${FLAT}/base.pkg/PackageInfo" <<EOF
<?xml version="1.0" encoding="utf-8"?>
<pkg-info format-version="2" identifier="one.jorbique.vpn.pkg" version="${VERSION}" install-location="/" auth="root">
  <payload installKBytes="${INSTALL_KB}" numberOfFiles="${NUM_FILES}"/>
  <scripts>
    <preinstall file="./preinstall"/>
    <postinstall file="./postinstall"/>
  </scripts>
  <bundle-version>
    <bundle id="one.jorbique.vpn" CFBundleIdentifier="one.jorbique.vpn" path="${MACOS_APP_BUNDLE_REL}" CFBundleVersion="${VERSION}"/>
  </bundle-version>
</pkg-info>
EOF

# shellcheck disable=SC2018,SC2019
TITLE_ESC="$(printf '%s' "${TITLE}" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g')"

cat > "${FLAT}/Distribution" <<EOF
<?xml version="1.0" encoding="utf-8"?>
<installer-script minSpecVersion="1.000000" authoringTool="jorbique-build" authoringToolVersion="1.0" authoringToolBuild="1">
    <title>${TITLE_ESC}</title>
    <options customize="never" allow-external-scripts="no"/>
    <domains enable_anywhere="true"/>
    <installation-check script="pm_install_check();"/>
    <script>function pm_install_check() {
  if(!(system.compareVersions(system.version.ProductVersion,'11.0') >= 0)) {
    my.result.title = 'Failure';
    my.result.message = 'Requires macOS 11 or later.';
    my.result.type = 'Fatal';
    return false;
  }
  return true;
}</script>
    <choices-outline>
        <line choice="choice1"/>
    </choices-outline>
    <choice id="choice1" title="Jorbique VPN Extended">
        <pkg-ref id="one.jorbique.vpn.pkg"/>
    </choice>
    <pkg-ref id="one.jorbique.vpn.pkg" installKBytes="${INSTALL_KB}" version="${VERSION}" auth="Root">#base.pkg</pkg-ref>
</installer-script>
EOF

( cd "${FLAT}" && xar --compression none -cf "${OUT_PKG}" . )
echo "✓ Built: ${OUT_PKG}"
