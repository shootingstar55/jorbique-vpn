#!/usr/bin/env bash
# Prints the -X flags that point a build at its update server. The host and the
# channel are not in the repository: they come from SERVER_HOST and
# SERVER_CHANNEL, or from build.env at the repo root (gitignored, see
# build.env.example). CI passes them from the repository's Actions secrets.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [ -f "${root}/build.env" ]; then
  # Values already in the environment win over the file.
  host="${SERVER_HOST:-}"
  channel="${SERVER_CHANNEL:-}"
  # shellcheck disable=SC1091
  . "${root}/build.env"
  SERVER_HOST="${host:-${SERVER_HOST:-}}"
  SERVER_CHANNEL="${channel:-${SERVER_CHANNEL:-}}"
fi

if [ -z "${SERVER_HOST:-}" ] || [ -z "${SERVER_CHANNEL:-}" ]; then
  echo "ERROR: SERVER_HOST and SERVER_CHANNEL must be set (environment or build.env, see build.env.example)." >&2
  exit 1
fi

channel="${SERVER_CHANNEL#/}"
channel="${channel%/}"
printf '%s' "-X jorbiquevpn/internal/constants.ServerHost=${SERVER_HOST} -X jorbiquevpn/internal/constants.ExtendedUpdatePath=/${channel}/"
