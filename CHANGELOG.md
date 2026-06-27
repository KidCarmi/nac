# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Control-plane API (`nacd`)** with REST endpoints for endpoints, policies,
  access evaluation, audit, and authentication.
- **Access-decision engine** (`internal/access`): pure, deterministic,
  first-match-wins with secure default-deny.
- **Authentication & authorization**: Argon2id password hashing, HMAC-SHA256
  JWT operator sessions with pinned algorithm, role-based access control
  (admin/operator/viewer), and a static service token for enforcement points.
- **PostgreSQL persistence** via `pgx` with an embedded, transactional
  migration runner and the initial schema (operators, endpoints, policies,
  audit_events).
- **Redis policy cache** with a no-op fallback that preserves correctness.
- **Observability**: structured JSON logging (slog) with secret redaction,
  Prometheus metrics, and W3C trace-context propagation.
- **Audit logging**: append-only record of security-relevant actions.
- **Operational endpoints**: `/healthz`, `/readyz`, `/metrics`, `/version`.
- **Deployment**: distroless non-root Docker image, Docker Compose stack, and
  hardened Kubernetes manifests (Deployment, Service, HPA, PDB, ConfigMap,
  Secret template).
- **CI**: lint, vet, unit tests with race detector, integration tests against
  PostgreSQL, and container build.
- **Documentation**: architecture, threat model, database design, runbooks,
  guides, ADRs, and OpenAPI 3.1 specification.
- **Hardened CI/CD pipeline** extending the correctness baseline with:
  - Coverage enforcement (minimum threshold via `scripts/check-coverage.sh`).
  - Go fuzz tests (`FuzzNormalizeMAC`, `FuzzTokenParse`, `FuzzVerifyPassword`,
    `FuzzEvaluateBody`) run on every PR.
  - Benchmark-regression gate (`BenchmarkEvaluate` + benchstat, 20% threshold).
  - Migration validation across PostgreSQL 15/16/17 (clean, idempotent,
    down/up) via `scripts/validate-migrations.sh`.
  - Dependency & vulnerability scanning: govulncheck, OSV-Scanner, Dependabot.
  - Static security analysis: gosec and CodeQL (security-and-quality), SARIF.
  - Secret scanning: gitleaks (with `.gitleaks.toml` allowlist).
  - Container & filesystem scanning: Trivy and Grype.
  - IaC/Kubernetes validation: Checkov, kube-linter, kube-score.
  - OpenAPI linting (Spectral) and breaking-change detection (oasdiff).
  - DAST: OWASP ZAP API scan against a live Compose stack.
  - OSSF Scorecard with SARIF reporting.
  - Supply-chain release pipeline: reproducible builds, SPDX SBOMs, Cosign
    keyless image signing, and SLSA build-provenance + SBOM attestations.

[Unreleased]: https://github.com/kidcarmi/nac/commits/main
