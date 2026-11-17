#!/usr/bin/env bash
# End-to-end demo of review-only change requests (slice A), no switch or EVE
# needed. It:
#   1. builds nacd from this checkout;
#   2. RESETS the given disposable database (DEMO_RESET=1 is required);
#   3. starts nacd on 127.0.0.1:${DEMO_PORT:-18080};
#   4. runs the console walkthrough (scripts/ui-change-requests.js), which
#      proposes the draft, validates, simulates, reproduces, rebases and
#      cancels, and checks that the active policy and NAC's simulated RADIUS
#      authorization reply (VLAN 10) did not change. The RADIUS request is
#      simulated through the adapter API: no EAP-TLS, switch, DHCP or
#      endpoint traffic, so this is not endpoint evidence;
#   5. prints the resulting change request as the API returns it.
#
#   DEMO_RESET=1 NAC_DB_DSN=postgres://nac:nac@localhost:5432/nac_demo?sslmode=disable \
#     scripts/demo-change-requests.sh [screenshot-dir]
#
# Needs Go, psql, curl, node and Playwright with Chromium. Never point it at a
# database you want to keep: it drops the public schema.
set -euo pipefail
: "${NAC_DB_DSN:?set NAC_DB_DSN to a DISPOSABLE database}"
if [ "${DEMO_RESET:-}" != "1" ]; then
  echo "refusing: set DEMO_RESET=1 to confirm $NAC_DB_DSN may be wiped" >&2
  exit 2
fi
port="${DEMO_PORT:-18080}"
work="$(mktemp -d)"
trap 'kill "${pid:-0}" 2>/dev/null || true; rm -rf "$work"' EXIT

go build -o "$work/nacd" ./cmd/nacd
psql "$NAC_DB_DSN" -qc 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;' >/dev/null 2>&1

export NAC_BOOTSTRAP_ADMIN_EMAIL=admin@nac.local
export NAC_BOOTSTRAP_ADMIN_PASSWORD="demo-$(date +%s)-passphrase"
export NAC_SERVICE_TOKEN="demo-service-token-$(date +%s)-0123456789"
export NAC_SECRETS_KEY="demo-secrets-key-0123456789abcdef0123456789"
export NAC_JWT_SIGNING_KEY="demo-jwt-signing-key-0123456789abcdef0123"
export NAC_HTTP_ADDR="127.0.0.1:$port"
export NAC_FREERADIUS_CLIENTS_FILE="$work/clients.conf" NAC_FREERADIUS_OUTAGE_LOG="$work/outage.log"
export NAC_FREERADIUS_PROCESS_CHECK=off
"$work/nacd" >"$work/nacd.log" 2>&1 &
pid=$!
for _ in $(seq 1 60); do curl -sf "http://127.0.0.1:$port/readyz" >/dev/null && break; sleep 0.5; done

NAC_URL="http://127.0.0.1:$port" NAC_ADMIN_PASSWORD="$NAC_BOOTSTRAP_ADMIN_PASSWORD" \
  node scripts/ui-change-requests.js "${1:-}"

tok=$(curl -sf "http://127.0.0.1:$port/api/v1/auth/login" \
  -d "{\"email\":\"admin@nac.local\",\"password\":\"$NAC_BOOTSTRAP_ADMIN_PASSWORD\"}" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
id=$(curl -sf -H "Authorization: Bearer $tok" "http://127.0.0.1:$port/api/v1/change-requests" |
  sed -n 's/.*"change_requests":\[{"id":"\([^"]*\)".*/\1/p')
echo
echo "== GET /api/v1/change-requests/$id (as the API returns it)"
curl -sf -H "Authorization: Bearer $tok" "http://127.0.0.1:$port/api/v1/change-requests/$id"
echo
