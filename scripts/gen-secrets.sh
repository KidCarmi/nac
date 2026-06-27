#!/usr/bin/env bash
# Generate strong secrets for a non-development deployment and print them as
# environment variable assignments. Pipe into your secrets manager; do NOT
# write the output to a committed file.
set -euo pipefail

command -v openssl >/dev/null || { echo "openssl is required" >&2; exit 1; }

echo "NAC_JWT_SIGNING_KEY=$(openssl rand -base64 48 | tr -d '\n')"
echo "NAC_SERVICE_TOKEN=$(openssl rand -hex 32)"
echo "# Generated $(date -u +%Y-%m-%dT%H:%M:%SZ). Store these in your secrets manager."
