#!/usr/bin/env bash
# Validate database migrations against a PostgreSQL instance.
#
# Exercises three properties that the embedded migration runner must hold:
#   1. Clean apply:   migrations apply successfully to an empty database.
#   2. Idempotency:   re-running applies nothing and does not error.
#   3. Down/up cycle: the .down.sql files reverse the schema, and a subsequent
#                     up re-applies cleanly (the "upgraded then rebuilt" path).
#
# Usage:
#   NAC_DB_DSN=postgres://nac:nac@localhost:5432/nac?sslmode=disable \
#     scripts/validate-migrations.sh
#
# Requires: psql, go (to run cmd/nacd -migrate). The DSN must point at a
# database the caller is willing to have mutated/reset.
set -euo pipefail

: "${NAC_DB_DSN:?NAC_DB_DSN must be set}"
export NAC_DB_DSN
MIGRATIONS_DIR="internal/storage/postgres/migrations"

psql_dsn() { psql "$NAC_DB_DSN" -v ON_ERROR_STOP=1 -t -A "$@"; }

reset_db() {
  echo "--> resetting public schema"
  psql_dsn -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" >/dev/null
}

count_migrations_files() {
  find "$MIGRATIONS_DIR" -name '*.up.sql' | wc -l | tr -d ' '
}

applied_count() {
  psql_dsn -c "SELECT count(*) FROM schema_migrations;" 2>/dev/null || echo 0
}

echo "=== 1. clean apply ==="
reset_db
go run ./cmd/nacd -migrate
EXPECTED="$(count_migrations_files)"
GOT="$(applied_count)"
echo "applied migrations: got=$GOT expected=$EXPECTED"
[[ "$GOT" == "$EXPECTED" ]] || { echo "FAIL: applied count mismatch" >&2; exit 1; }

# Core tables must exist.
for tbl in operators endpoints policies audit_events; do
  EXISTS="$(psql_dsn -c "SELECT to_regclass('public.$tbl') IS NOT NULL;")"
  [[ "$EXISTS" == "t" ]] || { echo "FAIL: table $tbl missing" >&2; exit 1; }
done
echo "all core tables present"

echo "=== 2. idempotency (re-apply) ==="
go run ./cmd/nacd -migrate
GOT2="$(applied_count)"
[[ "$GOT2" == "$EXPECTED" ]] || { echo "FAIL: re-apply changed migration count ($GOT2 != $EXPECTED)" >&2; exit 1; }
echo "re-apply is a no-op (count stable at $GOT2)"

echo "=== 3. down/up cycle ==="
# Apply every .down.sql in reverse lexical order, then drop the ledger.
for f in $(find "$MIGRATIONS_DIR" -name '*.down.sql' | sort -r); do
  echo "--> applying down: $f"
  psql_dsn -f "$f" >/dev/null
done
psql_dsn -c "DROP TABLE IF EXISTS schema_migrations;" >/dev/null
# Core tables should now be gone.
for tbl in operators endpoints policies audit_events; do
  EXISTS="$(psql_dsn -c "SELECT to_regclass('public.$tbl') IS NOT NULL;")"
  [[ "$EXISTS" == "f" ]] || { echo "FAIL: table $tbl still present after down" >&2; exit 1; }
done
echo "down migrations removed all core tables"

go run ./cmd/nacd -migrate
GOT3="$(applied_count)"
[[ "$GOT3" == "$EXPECTED" ]] || { echo "FAIL: re-up after down did not restore migrations" >&2; exit 1; }
echo "re-up after down restored schema (count=$GOT3)"

echo "=== migration validation passed ==="
