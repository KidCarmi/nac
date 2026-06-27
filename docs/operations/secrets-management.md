# Secrets Management

The NAC daemon depends on a small, well-defined set of secrets. This document
enumerates each secret, explains how secrets are injected at runtime, how to
generate them, and the guarantees the platform makes about never exposing them
in logs. Rotation procedures are covered separately in the
[secret rotation runbook](../../runbooks/secret-rotation.md).

## Inventory of secrets

| Secret | Environment variable | Format / constraint | Used for |
|--------|----------------------|---------------------|----------|
| JWT signing key | `NAC_JWT_SIGNING_KEY` | >= 32 bytes; required in non-development | HMAC signing and verification of operator JWTs. |
| Service token | `NAC_SERVICE_TOKEN` | >= 24 characters; required in non-development | Shared secret presented by service-to-service clients. |
| Database credentials | `NAC_DB_DSN` | PostgreSQL connection string (contains username/password) | Authenticating to PostgreSQL. |
| Redis password | `NAC_REDIS_PASSWORD` | Free-form string | Authenticating to Redis, when the cache is enabled. |

All four are consumed strictly from the process environment. The application
never reads secrets from disk and never accepts them on the command line, which
keeps them out of process listings and shell history.

> The bootstrap administrator password (`NAC_BOOTSTRAP_ADMIN_PASSWORD`) is also
> sensitive. It is used only on first boot to provision an initial operator and
> is stored as an argon2 hash; it is redacted in logs like every other secret.

## How secrets are injected

Secrets are delivered to the process as environment variables, populated from a
secrets manager rather than committed to source control. In Kubernetes the
non-secret configuration lives in the `nacd-config` ConfigMap and the secrets in
the `nacd-secrets` Secret; both are mounted via `envFrom` so every key becomes an
environment variable in the container.

Recommended injection mechanisms, in order of preference:

| Mechanism | How it works |
|-----------|--------------|
| **External Secrets Operator (ESO)** | An `ExternalSecret` resource syncs values from an upstream store (AWS Secrets Manager, GCP Secret Manager, Azure Key Vault, etc.) into the `nacd-secrets` Kubernetes Secret. The deployment consumes the synced Secret unchanged. |
| **Vault Agent** | A sidecar or init container authenticates to HashiCorp Vault and renders secrets into the environment, keeping the values out of the Kubernetes API server entirely. |
| **sealed-secrets** | Secrets are encrypted at rest in Git as a `SealedSecret` and decrypted in-cluster by the controller into the `nacd-secrets` Secret. Suitable for GitOps workflows where the encrypted form is safe to commit. |

The committed manifest at
[`deployments/kubernetes/config.yaml`](../../deployments/kubernetes/config.yaml)
is a **template only**. It contains `CHANGEME` placeholders and must never hold
real secret values in version control:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: nacd-secrets
  namespace: nac
type: Opaque
stringData:
  NAC_DB_DSN: "postgres://nac:CHANGEME@postgres:5432/nac?sslmode=require"
  NAC_REDIS_ADDR: "redis:6379"
  NAC_REDIS_PASSWORD: "CHANGEME"
  NAC_JWT_SIGNING_KEY: "CHANGEME-generate-with-openssl-rand-base64-48"
  NAC_SERVICE_TOKEN: "CHANGEME-generate-with-openssl-rand-hex-32"
```

In production, replace this Secret with one populated by ESO, Vault Agent, or
sealed-secrets.

## Generating secrets

The JWT signing key and service token must be high-entropy random values. Use
OpenSSL to generate them:

```bash
# JWT signing key — 48 random bytes, base64-encoded (well above the 32-byte minimum)
openssl rand -base64 48

# Service token — 32 random bytes, hex-encoded (64 characters, above the 24-char minimum)
openssl rand -hex 32
```

A helper script, [`scripts/gen-secrets.sh`](../../scripts/gen-secrets.sh),
produces both in `KEY=value` form ready to pipe into a secrets manager:

```bash
$ ./scripts/gen-secrets.sh
NAC_JWT_SIGNING_KEY=Qm9uZ29... (base64, 48 bytes of entropy)
NAC_SERVICE_TOKEN=3f1c9a... (64 hex characters)
# Generated 2026-06-27T00:00:00Z. Store these in your secrets manager.
```

The script requires `openssl` and intentionally prints to stdout. **Do not
redirect its output into a committed file** — pipe it directly into your secrets
manager's import command.

Database credentials and the Redis password are typically provisioned by the
respective managed-service or operator and stored alongside the generated tokens
in the same secrets manager.

## Secret length enforcement

The application validates secret strength at startup in non-development
environments:

- `NAC_JWT_SIGNING_KEY` must be at least **32 bytes**. The recommended
  `openssl rand -base64 48` produces 48 bytes of entropy, comfortably exceeding
  the minimum.
- `NAC_SERVICE_TOKEN` must be at least **24 characters**. The recommended
  `openssl rand -hex 32` produces a 64-character token.

If either secret is missing or too short in staging or production, the process
refuses to start. See [configuration.md](configuration.md) for the full
validation rules.

## Rotation

Secrets should be rotated on a regular schedule and immediately upon any
suspected compromise. The step-by-step procedure — including how to roll the JWT
signing key without invalidating all in-flight tokens at once, and how to rotate
the service token across clients — is documented in the
[secret rotation runbook](../../runbooks/secret-rotation.md).

Rotation overview:

1. Generate the new secret with the commands above.
2. Publish the new value to the secrets manager.
3. Roll the deployment so pods pick up the updated `nacd-secrets`.
4. Decommission the old value once all clients are confirmed on the new one.

## Never-log guarantees

The logging layer (`internal/observability/logging.go`) installs a `ReplaceAttr`
hook on the slog handler that **redacts sensitive attribute keys** before they
are written, regardless of log level or format. The following keys are replaced
with `[REDACTED]`:

```
password, password_hash, authorization, token, service_token,
jwt, secret, signing_key, set-cookie, cookie
```

This means a secret accidentally attached to a log record under any of these
keys never reaches stderr. Combined with the rules below, secrets stay out of the
observability pipeline:

- **Do not** place secret values under log keys that are not on the redaction
  list, and do not interpolate secrets into free-form log messages — only keyed
  attributes are redacted.
- **Do not** echo the environment (for example `env` dumps) from operational
  tooling running in the same context.
- Secrets are not emitted in metrics or trace context; trace and request IDs are
  randomly generated identifiers carrying no secret material.

See [logging.md](logging.md) for the complete redaction behavior and example log
lines.

## Related documentation

- [Configuration](configuration.md) — environment variable reference and
  validation rules.
- [Logging](logging.md) — redaction list and request-scoped fields.
- [Secret rotation runbook](../../runbooks/secret-rotation.md) — operational
  rotation procedure.
- [`deployments/kubernetes/config.yaml`](../../deployments/kubernetes/config.yaml) — ConfigMap and Secret template.
- [`scripts/gen-secrets.sh`](../../scripts/gen-secrets.sh) — secret generation helper.
