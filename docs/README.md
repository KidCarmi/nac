# Documentation Index

Authoritative documentation for the NAC platform. Documents are kept
**synchronized with the code**; changing behavior requires changing the
relevant docs in the same change (see the
[Definition of Done](guides/contributing.md#definition-of-done)).

## Architecture
- [Overview (HLD, components, sequences)](architecture/overview.md)
- [Business Logic & Domain Model](architecture/business-logic.md)
- [Low-Level Design](architecture/low-level-design.md)
- [Diagrams](architecture/diagrams.md)
- [Integration boundary (versioned API, server-side authorization, attribution)](architecture/integration-boundary.md)

## Pilot (wired EAP-TLS on Arista EOS)
- [Installation](pilot/installation.md) · [Operator walkthrough](pilot/operator-walkthrough.md) · [Known limitations](pilot/known-limitations.md)
- [Qualification status](pilot/qualification-status.md) · [Lab API/evidence contract](pilot/astra-contract.md)

## API
- [OpenAPI 3.1 specification](../api/openapi/openapi.yaml) — also served at `GET /openapi.yaml`

## Database
- [Database Design & ERD](database/schema.md)
- [Migration Guide](database/migrations.md)

## Security
- [Security Policy & Review Checklist](../security/README.md)
- [Attack Surface](../security/attack-surface.md)
- [Threat Model](security/threat-model.md)
- [Trust Boundaries](security/trust-boundaries.md)
- [Authentication, Authorization & RBAC](security/authn-authz.md)
- [Audit Logging](security/audit-logging.md)

## Operations
- [Configuration](operations/configuration.md)
- [Secrets Management](operations/secrets-management.md)
- [Observability](operations/observability.md) · [Logging](operations/logging.md) · [Metrics](operations/metrics.md) · [Tracing](operations/tracing.md)
- [Performance Strategy](operations/performance.md) · [Scalability Strategy](operations/scalability.md)
- [Backup Strategy](operations/backup-strategy.md) · [Disaster Recovery](operations/disaster-recovery.md)
- [Incident Response](operations/incident-response.md)
- [Testing Strategy](operations/testing-strategy.md)
- [Release Process](operations/release-process.md)

## Runbooks
- [Runbook Index](../runbooks/README.md)

## Guides
- [Administrator Guide](guides/administrator.md)
- [Developer Guide](guides/developer.md)
- [User Manual](guides/user-manual.md)
- [Contributing Guide](guides/contributing.md)
- [Coding Standards](guides/coding-standards.md)

## Decisions
- [Architecture Decision Records](../adr/README.md)
- [RFCs](../rfcs/README.md)
