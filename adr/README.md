# Architecture Decision Records

ADRs capture significant architectural decisions, their context, and their
consequences. They are immutable once accepted; a decision is changed by adding
a new ADR that supersedes an old one, never by editing history.

Format: [MADR](https://adr.github.io/madr/)-style (Context / Decision /
Consequences). Use [`0000-template.md`](0000-template.md) for new records.

| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-go-postgres-redis-stack.md) | Go + PostgreSQL + Redis technology stack | Accepted |
| [0002](0002-hexagonal-architecture.md) | Hexagonal architecture with a pure decision engine | Accepted |
| [0003](0003-default-deny-policy-engine.md) | Default-deny, first-match-wins policy semantics | Accepted |
| [0004](0004-auth-jwt-argon2-rbac.md) | JWT sessions, Argon2id, and static RBAC | Accepted (revocation superseded by 0007) |
| [0005](0005-stdlib-router-no-orm.md) | Standard-library router and no ORM | Accepted |
| [0006](0006-wired-pilot-architecture.md) | Wired pilot: FreeRADIUS front end, nacd decisions, versioned policy, identity-scoped override | Accepted |
| [0007](0007-operator-session-revocation.md) | Operator sessions revocable through a per-operator token version | Accepted |
