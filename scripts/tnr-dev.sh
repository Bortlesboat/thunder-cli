#!/usr/bin/env bash
set -euo pipefail

target_env="${1:-}"
case "$target_env" in
  staging) api_url="${TNR_STAGING_API_URL:?Set TNR_STAGING_API_URL or use make cli-staging from the monorepo root}" ;;
  local) api_url=http://localhost:8443 ;;
  *) echo "Usage: $0 <local|staging> [tnr arguments...]" >&2; exit 1 ;;
esac
shift

cli_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mkdir -p "$cli_dir/dist"
(
  cd "$cli_dir"
  bazel run //:go -- build -o "$cli_dir/dist/tnr-$target_env" .
) >&2

exec env -u TNR_API_TOKEN \
  TNR_API_URL="$api_url" \
  TNR_HOME="$cli_dir/dist/$target_env-home" \
  TNR_NO_SELFUPDATE=1 \
  "$cli_dir/dist/tnr-$target_env" "$@"
