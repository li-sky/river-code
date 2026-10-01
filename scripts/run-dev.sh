#!/usr/bin/env bash
set -euo pipefail
river_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$river_root"
if [[ ! -f .env ]]; then
  echo '请复制 .env.example 为 .env，并配置 POSTGRES_PASSWORD。' >&2
  exit 1
fi
# .env 是本地受信任的系统配置文件；与示例一致，值使用 shell 合法引号。
set -a
source .env
set +a
: "${POSTGRES_PASSWORD:?请在 .env 中配置 POSTGRES_PASSWORD}"
river_go="${GO_BIN:-}"
if [[ -z "$river_go" ]]; then
  for river_candidate in /workspace/toolchain/go/bin/go /usr/local/go/bin/go "$(command -v go || true)"; do
    if [[ -x "$river_candidate" ]] && "$river_candidate" version 2>/dev/null | head -1 | grep -q '^go version go'; then
      river_go="$river_candidate"
      break
    fi
  done
fi
if [[ -z "$river_go" ]]; then
  echo '需要 Go 1.23+；可通过 GO_BIN 指定 Go 编译器。' >&2
  exit 1
fi
docker compose up -d --wait db
if [[ ! -d frontend/node_modules ]]; then
  (cd frontend && npm ci)
fi
(cd frontend && npm run build)
export DATABASE_URL="${DATABASE_URL:-postgres://${POSTGRES_USER:-river}:${POSTGRES_PASSWORD}@127.0.0.1:${POSTGRES_PORT:-5432}/${POSTGRES_DB:-river}?sslmode=disable}"
export BASE_URL="${BASE_URL:-http://localhost:8080}"
export STATIC_DIR="$river_root/frontend/dist"
echo "RIVER 已构建，将监听 ${LISTEN_ADDR:-:8080}；访问 $BASE_URL。"
cd backend
exec "$river_go" run ./cmd/river
