#!/usr/bin/env bash
# Prints the version both clients build with: $APP_VERSION when set (a one-off
# build), otherwise the VERSION file at the repository root. Bump VERSION to
# release a new version.
#
# Always prints a bare x.y.z. Windows resource headers and the macOS installer
# both reject anything else: the value ends up verbatim in the PE version
# resource (scripts/bump-windows-version.mjs) and in Info.plist, so a malformed
# one stops the build rather than being swapped for some other version.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

ver_re='^v?([0-9]+\.[0-9]+\.[0-9]+)$'

source="APP_VERSION"
version="${APP_VERSION:-}"
# The Makefile passes 0.0.0-dev when nobody asked for a version.
if [ -z "${version}" ] || [ "${version}" = "0.0.0-dev" ]; then
	source="VERSION"
	version="$(tr -d '[:space:]' <"${root}/VERSION")"
fi
if [[ ! "${version}" =~ ${ver_re} ]]; then
	printf 'app-version: %s=%s is not a bare x.y.z\n' "${source}" "${version}" >&2
	exit 1
fi
printf '%s\n' "${BASH_REMATCH[1]}"
