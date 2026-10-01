#!/usr/bin/env bash
set -euo pipefail

river_code_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
river_spec_root="${RIVER_SPEC_DIR:-$(dirname -- "$river_code_root")/river-spec}"
river_sdd_cli="$river_spec_root/scripts/sdd.py"

if [[ ! -f "$river_sdd_cli" ]]; then
  echo '需要同级 river-spec 仓库，或用 RIVER_SPEC_DIR 指定其目录。' >&2
  echo 'git clone https://github.com/li-sky/river-spec.git' >&2
  exit 1
fi

case "${1:-}" in
  check|start|run|finish)
    exec python3 "$river_sdd_cli" "$@" --code-dir "$river_code_root"
    ;;
  *)
    exec python3 "$river_sdd_cli" "$@"
    ;;
esac
