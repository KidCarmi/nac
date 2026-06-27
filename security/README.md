# Security Policy

## Reporting a vulnerability

Please report security issues privately to the maintainers (do **not** open a
public issue). Provide a description, reproduction steps, and impact. We aim to
acknowledge within 3 business days and to ship a fix or mitigation according to
severity. Coordinated disclosure is appreciated.

## Supported versions

The latest minor release receives security fixes. Pre-1.0, only `main` and the
most recent tagged release are supported.

## Security model summary

`nac` is built shift-left: security is a property of the design, not a layer
bolted on. The authoritative analysis lives in:

- [Threat Model](../docs/security/threat-model.md)
- [Trust Boundaries](../docs/security/trust-boundaries.md)
- [Attack Surface](attack-surface.md)
- [Authentication, Authorization & RBAC](../docs/security/authn-authz.md)
- [Audit Logging](../docs/security/audit-logging.md)

### Controls at a glance

| Control | Implementation |
|---------|----------------|
| Password storage | Argon2id (PHC format, constant-time verify) |
| Sessions | JWT HS256, algorithm pinned, iss/aud/exp validated |
| Machine auth | Static service token, constant-time compare |
| Authorization | Static, fail-closed RBAC matrix, per-route checks |
| Input validation | Strict JSON decode (no unknown fields) + domain validation |
| Output | RFC 7807 problem+json; no internal detail leakage |
| Injection | Parameterized SQL only (`pgx`) |
| Transport | TLS terminated at ingress; DB/Redis over TLS in prod |
| Secrets | Env-injected from a secrets manager; never logged |
| Abuse | Per-client rate limiting, body-size caps, timeouts |
| Accountability | Append-only audit log with correlation ids |
| Defaults | Default-deny engine; secrets required outside dev |

## Pre-release security checklist

Before tagging a release, confirm:

- [ ] `make lint` passes (includes `gosec`).
- [ ] No secrets in code, fixtures, or committed config (`git grep` for keys).
- [ ] Dependencies reviewed; `go mod tidy` clean; no unexpected modules.
- [ ] Threat model updated for any new entry point / data store / boundary.
- [ ] AuthN/AuthZ tests cover new routes (deny-by-default verified).
- [ ] New inputs validated and bounded; error paths return RFC 7807, not detail.
- [ ] Audit events emitted for new state-changing actions.
- [ ] `govulncheck ./...` clean (enforced in CI and as a release gate).
- [ ] Code-scanning (SARIF) findings triaged in the Security tab: gosec,
      CodeQL, OSV, Trivy, Grype, Checkov, kube-linter, Scorecard.
- [ ] No secrets introduced (gitleaks clean; allowlist only for documented,
      non-functional placeholders).
- [ ] OpenAPI changes are non-breaking (oasdiff) or intentionally versioned.
- [ ] Release artifacts are signed and attested (Cosign + SLSA provenance);
      see [CI/CD & Supply-Chain Security](../docs/operations/ci-cd.md).
