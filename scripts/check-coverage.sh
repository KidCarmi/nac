#!/usr/bin/env bash
# Enforce a minimum total statement-coverage threshold.
#
# Usage:
#   scripts/check-coverage.sh [min_percent] [coverage_profile]
#
# Defaults: min 60%, profile coverage.out. The threshold is intentionally set
# below current coverage to leave headroom; it should be ratcheted UP over time
# (never down) as coverage improves. See docs/operations/ci-cd.md.
set -euo pipefail

MIN="${1:-60}"
PROFILE="${2:-coverage.out}"

if [[ ! -f "$PROFILE" ]]; then
  echo "coverage profile not found: $PROFILE" >&2
  echo "run: go test -coverprofile=$PROFILE ./..." >&2
  exit 2
fi

TOTAL="$(go tool cover -func="$PROFILE" | awk '/^total:/ {gsub(/%/,"",$NF); print $NF}')"
if [[ -z "$TOTAL" ]]; then
  echo "could not determine total coverage from $PROFILE" >&2
  exit 2
fi

echo "total coverage: ${TOTAL}% (minimum: ${MIN}%)"

# Compare with awk to avoid bc dependency.
if awk -v t="$TOTAL" -v m="$MIN" 'BEGIN { exit (t+0 >= m+0) ? 0 : 1 }'; then
  echo "coverage gate passed"
else
  echo "coverage gate FAILED: ${TOTAL}% < ${MIN}%" >&2
  exit 1
fi
