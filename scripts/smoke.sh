#!/usr/bin/env bash
# Smoke test: exercises the running API end to end against a local nacd.
# Usage: BASE_URL=http://localhost:8080 ./scripts/smoke.sh
#
# Requires: curl, jq. Assumes the compose stack (or equivalent) is up with the
# bootstrap admin and service token from .env.example.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@nac.local}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-ChangeMe123!}"
SERVICE_TOKEN="${SERVICE_TOKEN:-dev-service-token-change-me-please-1234}"

say() { printf '\n\033[36m==> %s\033[0m\n' "$1"; }

say "Health check"
curl -fsS "$BASE_URL/healthz" | jq .

say "Login as admin"
TOKEN=$(curl -fsS -X POST "$BASE_URL/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" | jq -r .token)
[ -n "$TOKEN" ] && [ "$TOKEN" != "null" ] || { echo "login failed"; exit 1; }

say "Create a policy: allow compliant laptops"
curl -fsS -X POST "$BASE_URL/api/v1/policies" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"allow-compliant-laptops","priority":10,"enabled":true,"action":"allow","segment":"corp",
       "conditions":[{"field":"device_type","operator":"eq","values":["laptop"]},
                     {"field":"posture","operator":"eq","values":["compliant"]}]}' | jq .

say "Register an endpoint"
curl -fsS -X POST "$BASE_URL/api/v1/endpoints" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"mac":"aa:bb:cc:dd:ee:ff","hostname":"laptop-1","device_type":"laptop","posture":"compliant"}' | jq .

say "Evaluate access (enforcement point)"
curl -fsS -X POST "$BASE_URL/api/v1/access/evaluate" \
  -H "Authorization: Bearer $SERVICE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"mac":"aa:bb:cc:dd:ee:ff","posture":"compliant"}' | jq .

say "Smoke test passed"
