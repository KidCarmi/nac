# NAC Web Console (placeholder)

> **Status: not yet implemented.** This directory is a placeholder for the
> future React-based admin console. There is no application code here yet. The
> NAC platform is operated today via the REST API (`/api/v1`) and `kubectl`.

## Intended scope

A browser-based operator UI built **on top of the existing REST API** — the web
console is a client, not a new backend. Planned surfaces:

- **Login** — operator authentication via the JWT login flow (see below).
- **Endpoints** — browse/search endpoints, view and edit attributes and
  `posture`.
- **Policies** — list policies in priority order, with a **condition builder**
  for the engine's first-match-wins / all-conditions-AND model, plus
  enable/disable and priority editing.
- **Audit viewer** — search and inspect the decision audit log
  (`GET /api/v1/audit`).
- **Dashboards** — visualize the Prometheus metrics exposed at `/metrics`
  (request rates, latency, `nac_access_decisions_total`, auth failures).

## Planned stack

- **React + TypeScript**
- **Vite** for the dev server and build
- A generated, typed API client (see below)

## Consuming the API

- The backend serves its OpenAPI specification at **`/openapi.yaml`**. The
  console will generate a typed client from that spec, so the UI stays in sync
  with the API contract rather than hand-writing request code.
- **Auth:** the console authenticates operators through the JWT login flow and
  sends the resulting bearer JWT on `/api/v1` requests. Note that the
  `POST /api/v1/access/evaluate` path uses a static **service token** and is for
  enforcement points, **not** the operator console — the UI does not use it.

## Status & contributions

This is a future workstream. Contributions are welcome — see
[contributing.md](../docs/guides/contributing.md) and
[coding-standards.md](../docs/guides/coding-standards.md). When work starts, this
README will be replaced with real setup and development instructions.
