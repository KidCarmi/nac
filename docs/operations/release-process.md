# Release Process

This document describes how a release of the NAC platform is produced: how
versions are numbered and recorded, how the version is stamped into the binary,
the gates a release must pass, how the container image is built, and how to roll
back. The process is designed so that a released binary can always report
exactly what it is (`nacd -version`) and so that every release passes the same CI
gates and security review.

## Semantic versioning

The project adheres to **Semantic Versioning** (`MAJOR.MINOR.PATCH`):

- **MAJOR** — incompatible API or behaviour changes.
- **MINOR** — backwards-compatible functionality.
- **PATCH** — backwards-compatible fixes.

Version numbers are the contract communicated to operators and to enforcement
points that integrate with the API.

## CHANGELOG

The repository maintains a `CHANGELOG.md` following the **Keep a Changelog**
format. Unreleased work accumulates under the **`[Unreleased]`** section as it
merges, grouped under the standard headings (`Added`, `Changed`, `Fixed`, and so
on).

Cutting a release means:

1. Move the accumulated entries out of `[Unreleased]` into a new dated version
   section (`## [X.Y.Z] - YYYY-MM-DD`).
2. Leave a fresh, empty `[Unreleased]` section at the top for subsequent work.
3. Confirm the entries accurately describe what changed at the chosen SemVer
   level — a `Changed`/breaking entry implies at least a MINOR/MAJOR bump.

The CHANGELOG is the human-readable record; the git tag is the machine-readable
one. They must agree.

## Version injection into `pkg/version`

The binary does not hardcode its version. Three variables in `pkg/version`
default to placeholder values for local builds and are overwritten at build time
via linker flags:

| Variable | Default | Meaning |
|----------|---------|---------|
| `Version` | `dev` | Semantic version of the build |
| `Commit` | `unknown` | Git commit SHA built from |
| `BuildDate` | `unknown` | RFC3339 build timestamp |

The `Makefile` injects these with `-ldflags -X` into the build:

```
-X github.com/kidcarmi/nac/pkg/version.Version=$(VERSION) \
-X github.com/kidcarmi/nac/pkg/version.Commit=$(COMMIT) \
-X github.com/kidcarmi/nac/pkg/version.BuildDate=$(BUILD_DATE)
```

The result is queryable at runtime two ways:

- **`nacd -version`** prints the version and exits.
- **`GET /version`** exposes the same `version`, `commit`, and `build_date`
  fields over HTTP (see [observability.md](observability.md)).

A correctly built release therefore reports its real version, commit, and build
date rather than the `dev`/`unknown` placeholders — a quick way to confirm in
production that the intended artifact is running.

## Tagging

Releases are identified by an annotated git tag matching the CHANGELOG version
(e.g. `v1.2.3`). The tag is the source of truth for the `VERSION` injected into
the build and the image. Tag from a commit that has passed all CI gates, and
ensure the working tree is clean so the recorded `Commit` is meaningful.

## Pre-release security checklist

Before tagging a release, complete the **security checklist documented in
[`security/README.md`](../../security/README.md)**. This is a mandatory release
gate, not an optional review. It complements the always-on automated security
checks (the `gosec` linter and the security-specific tests described in
[testing-strategy.md](testing-strategy.md)) with the items that require human
judgement before shipping.

At minimum, confirm that the security-invariant tests still hold:

- `alg=none` JWT rejection,
- RBAC denial paths,
- separation of the service-token (evaluate) and operator-JWT (admin) auth
  paths,
- unknown-field rejection,
- fail-closed engine behaviour (unknown operators and no-match default DENY).

See [`security/README.md`](../../security/README.md) for the authoritative,
complete checklist.

## Building the image

The release image is built from `deployments/docker/Dockerfile` (a distroless,
non-root image) via the Makefile target, which threads the version metadata
through as Docker build arguments:

```bash
make docker-build
# runs:
# docker build -f deployments/docker/Dockerfile \
#   --build-arg VERSION=$(VERSION) \
#   --build-arg COMMIT=$(COMMIT) \
#   --build-arg BUILD_DATE=$(BUILD_DATE) \
#   -t nac/nacd:$(VERSION) .
```

The `VERSION`, `COMMIT`, and `BUILD_DATE` build args flow into the same
`-ldflags -X` injection so the image's binary reports the correct metadata. Tag
the image with the release version (`nac/nacd:$(VERSION)`).

## CI gates

Every release commit must be green on the pipeline in
`.github/workflows/ci.yml`, whose jobs are the release gates:

| Job | Gate |
|-----|------|
| **verify** | `make check`, `go vet`, and `golangci-lint` (`.golangci.yml`) all clean |
| **test** | Unit tests with `-race` and coverage pass |
| **integration** | Integration tests pass against a PostgreSQL service container |
| **docker** | The container image builds |

No release is tagged from a commit that has not passed all four. See
[testing-strategy.md](testing-strategy.md) for what each job exercises.

## Release procedure (summary)

1. Ensure `main` (or the release commit) is green on CI.
2. Finalise `CHANGELOG.md`: move `[Unreleased]` entries into a dated version
   section and open a fresh `[Unreleased]`.
3. Complete the pre-release security checklist in
   [`security/README.md`](../../security/README.md).
4. Create the annotated git tag (`vX.Y.Z`) on the release commit.
5. Build and tag the image with `make docker-build`.
6. Deploy via the Kubernetes manifests in `deployments/kubernetes`. The rolling
   update (`maxUnavailable: 0`, `maxSurge: 1`) keeps capacity intact;
   migrations apply automatically on boot (and `nacd -migrate` can be run
   explicitly).
7. Verify the rollout: `nacd -version` / `GET /version` report the new version,
   `/readyz` is green, and the evaluate path behaves as expected.

## Rollback

Migrations run on every boot and apply only pending UP migrations, each in its
own transaction, so deploying a release is incremental and the application tier
is stateless. If a release misbehaves, **roll back the deployment** to the
previous known-good image version following the deployment runbook
([`../runbooks/deployment.md`](../../runbooks/deployment.md)). The rolling-update
and PDB settings make a rollback as safe as a roll-forward. For data-level
issues introduced by a release (for example a bad mutation), see the recovery
options — including PITR — in [disaster-recovery.md](disaster-recovery.md).

## Related documents

- [testing-strategy.md](testing-strategy.md) — the CI gates in detail.
- [disaster-recovery.md](disaster-recovery.md) — data-level rollback / recovery.
- [observability.md](observability.md) — the `/version` endpoint.
- [`security/README.md`](../../security/README.md) — the pre-release security
  checklist.
