# Contributing Guide

Thank you for contributing to the **NAC** platform. This guide explains how to
propose changes, the branch and pull-request flow, commit message style, the
**Definition of Done**, code-review expectations, where decision records live,
and how to report security issues privately.

Before writing code, read the [Developer Guide](developer.md) for architecture
and the [Coding Standards](coding-standards.md) for style.

---

## 1. Before you start

- For non-trivial work, open an issue (or an [RFC](../../rfcs/README.md)) to
  align on approach before implementing.
- Confirm your change does not weaken the platform's **fail-closed** posture
  (default-deny engine, deny-by-default RBAC). If it touches security, treat it
  as security-relevant (see §6 and §8).

---

## 2. Branch and PR flow

1. Branch off `main`. Use a short, descriptive branch name
   (e.g. `feat/policy-segment-filter`, `fix/audit-offset-bound`).
2. Make focused commits; keep the change set scoped to one concern.
3. Run the full local pipeline and make it green:

   ```bash
   make ci      # tidy + fmt + vet + lint + test
   ```

4. Open a pull request against `main`. Fill in the description: what changed,
   why, how it was tested, and which docs/artifacts were updated.
5. Ensure CI is green and address review feedback. PRs merge only when the
   **Definition of Done** is satisfied.

---

## 3. Commit message style

Conventional-commit-style messages are encouraged:

```
<type>(<scope>): <imperative summary>

<body explaining the why, if not obvious>
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `perf`,
`build`, `ci`. Examples:

```
feat(policy): support segment filter on list endpoint
fix(audit): reject negative offset with a 422
docs(guides): add worked policy evaluation example
```

Keep the summary in the imperative mood and under ~72 characters; explain
*why* in the body when the change is not self-evident.

---

## Definition of Done

A change is **not complete** until **all** of the following are true. This is
the normative checklist for every PR; reviewers enforce it.

- **Production code** is implemented to standard — no shortcuts, no dead code.
- **Tests** exist and pass:
  - **Unit tests** (table-driven, no infrastructure) for logic and validation.
  - **Integration tests** (build-tagged, against a real PostgreSQL) where the
    change touches persistence.
  - **API tests** covering the new/changed endpoint behavior, including
    authn/authz (deny-by-default verified) and error paths.
- **Documentation updated as applicable**, in the **same change**:
  - `README`
  - architecture docs (`docs/architecture/*`)
  - [threat model](../security/threat-model.md) and trust boundaries
  - **API docs / OpenAPI** (`api/openapi`, served at `GET /openapi.yaml`)
  - **user and admin guides** ([user-manual.md](user-manual.md),
    [administrator.md](administrator.md))
  - **runbooks** ([`runbooks/`](../../runbooks/README.md))
  - **migration docs** for any schema change
  - **diagrams** affected by the change
  - an **ADR** for significant architectural decisions
    ([`adr/`](../../adr/README.md))
  - **CHANGELOG** entry ([`CHANGELOG.md`](../../CHANGELOG.md))
- **No TODOs or placeholders** are left in code, tests, or docs.
- **`make ci` is green** (`tidy + fmt + vet + lint + test`, including `gosec`).
- For **security-relevant changes**, the **security checklist** in
  [`security/README.md`](../../security/README.md) is completed (new entry
  points threat-modeled, inputs validated and bounded, error paths return
  RFC 7807 without leaking detail, audit events emitted for state-changing
  actions, deny-by-default tests added, `govulncheck` reviewed).

If an item does not apply, say so explicitly in the PR rather than omitting it
silently.

---

## 4. Code-review expectations

Reviewers check that a change:

- Respects the **hexagonal dependency rule** — `internal/domain` imports nothing
  internal; services depend on ports, not adapters; wiring stays in `cmd/nacd`.
  See [ADR 0002](../../adr/0002-hexagonal-architecture.md).
- Wraps errors with `%w` against domain sentinels and maps them to HTTP in
  `server/render.go`; all error responses are RFC 7807.
- Uses **parameterized SQL only** and keeps validation in the service layer.
- Preserves **fail-closed defaults** and adds deny-by-default authz tests for
  new routes.
- Ships the tests and docs required by the Definition of Done.
- Reads clearly: comments explain *why*, names are precise, interfaces are small
  and consumer-defined.

Authors should keep PRs small and respond to feedback promptly; reviewers should
be specific and actionable. Aim for at least one approving review before merge.

---

## 5. ADRs vs RFCs

- **ADRs** ([`adr/`](../../adr/README.md)) record *significant architectural
  decisions* in MADR style (Context / Decision / Consequences). They are
  **immutable once accepted** — supersede an old ADR with a new one rather than
  editing history. Start from
  [`adr/0000-template.md`](../../adr/0000-template.md).
- **RFCs** ([`rfcs/`](../../rfcs/README.md)) are for *proposing and discussing*
  larger changes before a decision is reached. Start from
  [`rfcs/0000-template.md`](../../rfcs/0000-template.md).

Rule of thumb: use an RFC to debate the approach; record the chosen approach as
an ADR.

---

## 6. Security checklist for security-relevant changes

A change is security-relevant if it adds an entry point, touches authn/authz,
handles secrets, changes the data model or stored data, or alters the policy
engine. For these, complete the **Pre-release security checklist** in
[`security/README.md`](../../security/README.md) as part of the PR, and update
the [threat model](../security/threat-model.md) and
[trust boundaries](../security/trust-boundaries.md) where boundaries shift.

---

## 7. Standards and conventions

- Code style: [Coding Standards](coding-standards.md).
- Architecture rules and how to add a feature end to end:
  [Developer Guide](developer.md).
- Formatting/linting are enforced by `make ci` and
  [`.golangci.yml`](../../.golangci.yml).

---

## 8. Reporting security issues

**Do not open a public issue for security vulnerabilities.** Report them
privately to the maintainers as described in the
[Security Policy](../../security/README.md). Provide a description, reproduction
steps, and impact. Maintainers aim to acknowledge within 3 business days and to
ship a fix or mitigation according to severity. Coordinated disclosure is
appreciated.
