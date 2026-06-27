# CI/CD & Supply-Chain Security

This document describes the platform's continuous-integration and
continuous-delivery pipeline. The pipeline enforces four concerns beyond code
correctness: **supply-chain integrity**, **security posture**, **operational
readiness**, and **release governance**. Every gate is defined as code under
[`.github/workflows/`](../../.github/workflows/).

## 1. Workflow overview

| Workflow | File | Trigger | Purpose |
|----------|------|---------|---------|
| CI | `ci.yml` | push/PR | Lint, vet, format, unit tests + **coverage gate**, **fuzz smoke**, integration, image build |
| Security | `security.yml` | push/PR + weekly | govulncheck, OSV-Scanner, gosec, gitleaks, Trivy (fs + image), Grype |
| CodeQL | `codeql.yml` | push/PR + weekly | Deep static analysis (security-and-quality suite) |
| OSSF Scorecard | `scorecard.yml` | push + weekly | Repository security-posture scoring |
| IaC & Kubernetes | `iac.yml` | push/PR (deployments) | Checkov, kube-linter, kube-score |
| OpenAPI | `openapi.yml` | push/PR (spec) | Spectral lint + **oasdiff breaking-change** detection |
| DAST | `dast.yml` | PR + weekly | OWASP ZAP API scan against a live stack |
| Performance | `perf.yml` | PR | Benchmark **regression gate** (benchstat, 20% threshold) |
| Migrations | `migrations.yml` | push/PR (storage) | Migration validation on PostgreSQL 15/16/17 |
| Release | `release.yml` | tag `v*` | Reproducible build, SBOM, image signing, **SLSA provenance** |

All scanners emit **SARIF** uploaded to the GitHub **code-scanning** tab
(`Security → Code scanning`), giving a single triage surface with one
`category` per tool.

## 2. Correctness gates (`ci.yml`)

- **verify** — `make check` (go.mod tidy + gofmt clean) + `go vet` + golangci-lint v2.
- **test** — `go test -race`, then `scripts/check-coverage.sh` enforces a
  minimum **total statement coverage of `MIN_COVERAGE` (currently 60%)**.
  > **Threshold policy:** the gate is set below current coverage to avoid
  > flakiness and is **ratcheted up, never down**, as coverage improves. Raise
  > `MIN_COVERAGE` in `ci.yml` in the same PR that raises coverage.
- **fuzz** — runs each Go fuzz target for 60s (matrix):
  `FuzzNormalizeMAC`, `FuzzTokenParse`, `FuzzVerifyPassword`, `FuzzEvaluateBody`.
  Discovered crashers are uploaded as artifacts and become regression seeds.
- **integration** — build-tagged tests against a real PostgreSQL service.
- **build-image** — sanity build of the container image.

Run locally:
```bash
make ci                                   # verify + test
go test -coverprofile=coverage.out ./... && ./scripts/check-coverage.sh 60
go test -run=NONE -fuzz=FuzzNormalizeMAC -fuzztime=30s ./pkg/validation/
```

## 3. Security posture

| Tool | What it catches | Failure mode |
|------|-----------------|--------------|
| **govulncheck** | Known CVEs in Go deps **reachable** by our code | Fails CI; gates releases |
| **OSV-Scanner** | Vulnerable dependencies (OSV DB) | SARIF (advisory) |
| **gosec** | Insecure Go patterns (also in golangci-lint) | SARIF |
| **gitleaks** | Committed secrets | Fails on findings |
| **Trivy (fs)** | Vulnerable deps + IaC misconfig in the tree | SARIF (HIGH/CRITICAL) |
| **Trivy (image)** + **Grype** | OS/lib CVEs in the built image | SARIF |
| **CodeQL** | Taint/dataflow security & quality issues | SARIF |
| **Dependabot** | Outdated/vulnerable deps, actions, base images | Opens PRs weekly |

Secret-scanning false positives (the documented dummy hash, dev-only compose
secrets, `.env.example` placeholders) are allow-listed in
[`.gitleaks.toml`](../../.gitleaks.toml) — see
[Secrets Management](secrets-management.md).

### govulncheck locally
```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

## 4. Operational readiness

### IaC / Kubernetes (`iac.yml`)
- **Checkov** — Kubernetes + Dockerfile misconfiguration (SARIF).
- **kube-linter** — production best-practices (resource limits, non-root,
  read-only rootfs, probes) on `deployments/kubernetes/`.
- **kube-score** — manifest scoring.

The manifests are already hardened (non-root 65532, `readOnlyRootFilesystem`,
dropped capabilities, seccomp `RuntimeDefault`, probes, HPA, PDB); these gates
prevent regressions.

### OpenAPI (`openapi.yml`)
- **Spectral** lints `api/openapi/openapi.yaml` against
  [`.spectral.yaml`](../../.spectral.yaml) (every operation tagged, described,
  with a success response).
- **oasdiff** fails a PR that introduces a **breaking API change** versus the
  spec on the base branch — protecting downstream enforcement-point integrations.

### DAST (`dast.yml`)
Brings up the Compose stack and runs the **OWASP ZAP** API scan driven by the
served OpenAPI document, exercising every documented endpoint against a live
instance. Alerts are tuned in [`.zap/rules.tsv`](../../.zap/rules.tsv) and the
HTML/JSON report is uploaded as an artifact. Advisory by default (a fresh
deployment has no data); promote to blocking once triaged for your environment.

### Migrations (`migrations.yml`)
Runs [`scripts/validate-migrations.sh`](../../scripts/validate-migrations.sh)
across PostgreSQL **15, 16, and 17**, asserting three properties:
1. **Clean apply** to an empty database creates all core tables.
2. **Idempotency** — re-running applies nothing.
3. **Down/up cycle** — `.down.sql` reverses the schema and a subsequent up
   restores it (the "upgraded then rebuilt" path).

```bash
NAC_DB_DSN=postgres://nac:nac@localhost:5432/nac?sslmode=disable \
  ./scripts/validate-migrations.sh
```

### Performance (`perf.yml`)
Runs `BenchmarkEvaluate` (the decision hot path) `count=6` on both the PR and
the base branch and compares with **benchstat**. A median regression **> 20%**
fails the job. The report is uploaded as an artifact.

## 5. Release governance (`release.yml`)

Triggered by a semver tag (`vX.Y.Z`). Stages:

1. **vuln-gate** — `govulncheck` on the exact tagged commit; a known
   reachable CVE blocks the release.
2. **reproducible-build** —
   - Builds the binary **twice** with `CGO_ENABLED=0`, `-trimpath`, and
     `SOURCE_DATE_EPOCH` pinned to the commit time, and **fails if the two
     SHA-256 digests differ** (verified reproducibility).
   - Generates an **SPDX SBOM** (Syft).
   - Produces **SLSA build provenance** and an **SBOM attestation** for the
     binary via GitHub's `attest-build-provenance` / `attest-sbom`.
3. **release-image** —
   - Builds and pushes the image to **GHCR** (`ghcr.io/<owner>/<repo>`) with
     build-in provenance + SBOM (`docker/build-push-action`).
   - **Signs** the image keylessly with **Cosign** (Sigstore OIDC — no
     long-lived keys).
   - Attaches **SBOM** and **SLSA provenance** attestations to the image in the
     registry.

### Verifying a release
```bash
# Verify the Cosign signature (keyless / Fulcio).
cosign verify ghcr.io/kidcarmi/nac@<digest> \
  --certificate-identity-regexp 'https://github.com/kidcarmi/nac/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# Verify SLSA build provenance and SBOM attestation.
gh attestation verify oci://ghcr.io/kidcarmi/nac@<digest> --owner kidcarmi
cosign verify-attestation --type spdxjson ghcr.io/kidcarmi/nac@<digest> ...
```

Reproducibility is verifiable locally:
```bash
SOURCE_DATE_EPOCH=$(git log -1 --pretty=%ct) \
CGO_ENABLED=0 GOFLAGS=-trimpath go build -ldflags "-s -w" -o /tmp/a ./cmd/nacd
# rebuild and compare sha256sum.
```

## 6. Permissions & hardening

- Workflows default to `permissions: contents: read`; jobs widen narrowly
  (`security-events: write` for SARIF, `packages/id-token/attestations: write`
  only in `release.yml`).
- Scorecard runs with `persist-credentials: false` and publishes results.
- Actions are pinned by version; **Dependabot** keeps them current.

## 7. Gate summary (blocking vs advisory)

| Gate | Blocking? |
|------|-----------|
| Lint / vet / format / unit tests / coverage ≥ 60% | **Blocking** |
| Fuzz smoke (crash found) | **Blocking** |
| Integration & migration validation | **Blocking** |
| govulncheck (CI + release gate) | **Blocking** |
| gitleaks | **Blocking** |
| OpenAPI breaking change | **Blocking** |
| Benchmark regression > 20% | **Blocking** |
| Reproducible-build mismatch (release) | **Blocking** |
| OSV / Trivy / Grype / CodeQL / Checkov / kube-* / ZAP / Scorecard | **Advisory** (SARIF) — triage in the Security tab; promote to blocking per policy |

Advisory gates surface findings without blocking day-one development; teams
should set a date to promote each to blocking once the baseline is clean.

## 8. Related documents
- [Release Process](release-process.md)
- [Testing Strategy](testing-strategy.md)
- [Secrets Management](secrets-management.md)
- [Security Policy & Checklist](../../security/README.md)
- [Threat Model](../security/threat-model.md)
