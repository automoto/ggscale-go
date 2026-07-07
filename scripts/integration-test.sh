#!/usr/bin/env bash
# Brings up the integration stack (postgres + ggscale pulled from Docker
# Hub), seeds it, runs the SDK integration tests, and tears the stack
# down.
#
#   KEEP_STACK=1            leave the stack running for debugging
#   GGSCALE_IT_PULL=never   test a locally built buildwrangler/ggscale:latest
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE=(docker compose -f integration/docker-compose.yml)
BASE_URL="${GGSCALE_IT_BASE_URL:-http://127.0.0.1:18080}"
PULL="${GGSCALE_IT_PULL:-always}"

cleanup() {
    "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
}
if [ "${KEEP_STACK:-0}" != "1" ]; then
    trap cleanup EXIT
fi

"${COMPOSE[@]}" up -d --pull "$PULL"

echo "waiting for ggscale at ${BASE_URL}/v1/healthz ..."
ready=""
for _ in $(seq 1 60); do
    if curl -fsS "${BASE_URL}/v1/healthz" >/dev/null 2>&1; then
        ready=1
        break
    fi
    sleep 1
done
if [ -z "$ready" ]; then
    echo "ggscale never became healthy; server logs:" >&2
    "${COMPOSE[@]}" logs ggscale >&2
    exit 1
fi

# Healthy server ⇒ migrations applied; seed tenant/project/keys directly.
"${COMPOSE[@]}" exec -T postgres \
    psql -q -v ON_ERROR_STOP=1 -U ggscale -d ggscale -f /seed.sql

GGSCALE_IT_BASE_URL="$BASE_URL" \
    go test -race -count=1 -tags=integration -v -run Integration .
