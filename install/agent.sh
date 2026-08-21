#!/usr/bin/env bash
set -Eeuo pipefail

# The RouteGlass Server replaces this marker when serving /install/agent.sh.
# For the static GitHub copy, pass --server explicitly.
SERVER_ORIGIN="${ROUTEGLASS_SERVER_ORIGIN:-__ROUTEGLASS_SERVER_ORIGIN__}"
REPO="${ROUTEGLASS_REPO:-steinyanaa/routeglass}"
ARGS=(agent)
HAS_SERVER=0
for arg in "$@"; do [[ "$arg" == --server ]] && HAS_SERVER=1; done
if (( ! HAS_SERVER )); then
  [[ "$SERVER_ORIGIN" != __ROUTEGLASS_SERVER_ORIGIN__ ]] || {
    printf 'error: static agent installer requires --server https://routeglass.example\n' >&2
    exit 2
  }
  ARGS+=(--server "$SERVER_ORIGIN")
fi
ARGS+=("$@")

tmp=$(mktemp "${TMPDIR:-/tmp}/routeglass-install.XXXXXXXX")
trap 'rm -f -- "$tmp"' EXIT
curl -fsSL --retry 3 "https://raw.githubusercontent.com/$REPO/main/install/install.sh" -o "$tmp"
bash "$tmp" "${ARGS[@]}"
