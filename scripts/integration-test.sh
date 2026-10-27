#!/usr/bin/env bash
# Brings up the integration stack (postgres, ggscale and a relay node pulled
# from GHCR), seeds it, registers the relay, runs the SDK integration tests, and tears the stack
# down.
#
#   KEEP_STACK=1            leave the stack running for debugging
#   GGSCALE_IT_PULL=never   test a local image named by GGSCALE_IMAGE
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

# Register the relay node the way an operator does, then wait for the
# registry probe (every 10s) to mark it eligible for credentials.
ADMIN=(-H "Authorization: Bearer integration-platform-admin-token-0123456789")
NODES_URL="${BASE_URL}/v1/admin/relay-nodes"
if ! curl -fsS "${ADMIN[@]}" "$NODES_URL" | grep -q 'http://relay:9091/lifecycle'; then
    curl -fsS "${ADMIN[@]}" -H 'Content-Type: application/json' -X POST "$NODES_URL" \
        -d '{"region":"it-east","turn_urls":["turn:127.0.0.1:13478?transport=udp"],"lifecycle_url":"http://relay:9091/lifecycle"}' \
        >/dev/null
fi
echo "waiting for the relay node to become eligible ..."
eligible=""
for _ in $(seq 1 30); do
    if curl -fsS "${ADMIN[@]}" "$NODES_URL" | grep -q '"eligible":true'; then
        eligible=1
        break
    fi
    sleep 1
done
if [ -z "$eligible" ]; then
    echo "relay node never became eligible:" >&2
    curl -sS "${ADMIN[@]}" "$NODES_URL" >&2
    "${COMPOSE[@]}" logs relay >&2
    exit 1
fi

GGSCALE_IT_BASE_URL="$BASE_URL" \
    go test -race -count=1 -tags=integration -v -run Integration .
