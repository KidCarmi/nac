# Architecture Diagrams

A consolidated collection of Mermaid diagrams for the NAC platform: system context,
containers, internal components, key sequences (access evaluation, operator login, create
policy with cache invalidation), and the entity-relationship model.

These expand on the diagrams embedded in the [Architecture Overview](./overview.md) and
[Low-Level Design](./low-level-design.md). The authoritative ERD also appears in the
[Database Schema](../database/schema.md).

---

## (a) System context (C4 level 1)

The platform sits between operators (humans administering policy) and enforcement points
(switches, wireless controllers, gateways, or agents that ask "should this device get
access?"). It persists to PostgreSQL and optionally caches enabled policies in Redis.

```mermaid
flowchart TB
    operator["Operator<br/>(admin / operator / viewer)"]
    enforcer["Enforcement point<br/>(switch / WLC / gateway / agent)"]

    subgraph platform["NAC Platform (nacd)"]
        nacd["nacd<br/>HTTP API + policy engine"]
    end

    pg[("PostgreSQL<br/>system of record")]
    redis[("Redis<br/>policy cache (optional)")]

    operator -->|"JWT-authenticated REST<br/>manage endpoints & policies"| nacd
    enforcer -->|"service-token REST<br/>POST /access/evaluate"| nacd
    nacd -->|"pgx pool, SQL"| pg
    nacd -->|"read-through cache (fail-open)"| redis
```

The two inbound actors authenticate differently: operators present a JWT and are gated by
RBAC; enforcement points present a shared **service token**. Redis is optional — if it is
absent or unreachable, the engine reads enabled policies straight from Postgres.

---

## (b) Container diagram (C4 level 2)

Inside the single `nacd` process, the inbound HTTP adapter delegates to application services,
which use outbound adapters for persistence and caching. There is one deployable binary; the
"containers" here are logical runtime collaborators.

```mermaid
flowchart TB
    subgraph nacd["nacd process"]
        http["HTTP server<br/>internal/server"]
        svc["Application services<br/>access · endpoint · policy · audit · auth"]
        pgad["Postgres adapter<br/>internal/storage/postgres"]
        cachead["Cache adapter<br/>internal/cache"]
        http --> svc
        svc --> pgad
        svc --> cachead
    end

    pg[("PostgreSQL")]
    redis[("Redis (optional)")]
    obs["Metrics scraper<br/>GET /metrics"]

    pgad --> pg
    cachead --> redis
    obs --> http
```

`cmd/nacd/main.go` is the composition root: it loads config, builds the logger, connects the
pool, runs embedded migrations, optionally connects Redis, constructs repositories and
services, optionally bootstraps an admin, builds the server, and serves with graceful
shutdown on SIGINT/SIGTERM.

---

## (c) Component diagram (internal packages)

The package dependency graph follows the hexagonal rule: everything points inward toward
`internal/domain`, which imports no other internal package. Adapters implement the port
interfaces declared in `domain`; only `main` binds interfaces to concretions.

```mermaid
flowchart TB
    main["cmd/nacd/main.go"]

    subgraph inbound["Inbound adapter"]
        server["internal/server<br/>router · middleware · handlers · render"]
    end

    subgraph services["Services"]
        access["internal/access<br/>Engine + Service"]
        endpoint["internal/endpoint"]
        policy["internal/policy"]
        audit["internal/audit"]
        auth["internal/auth<br/>jwt · password · rbac · context"]
    end

    subgraph outbound["Outbound adapters"]
        postgres["internal/storage/postgres"]
        cache["internal/cache"]
    end

    domain["internal/domain<br/>types · errors · ports"]
    pkg["pkg/problem · pkg/validation · pkg/version"]

    main --> server & access & endpoint & policy & audit & auth & postgres & cache
    server --> access & endpoint & policy & audit & auth & pkg
    access --> domain
    endpoint --> domain
    policy --> domain & cache
    audit --> domain
    auth --> domain
    postgres --> domain
    cache --> domain
```

Note that `internal/policy` depends on the `PolicyCache` port; the concrete `internal/cache`
implements it. `internal/server` depends on service *interfaces* (declared in the server
package) rather than concrete services, which keeps handlers unit-testable with fakes.

---

## (d) Sequence: access evaluation

An enforcement point asks whether a device (by MAC) should be granted access. The access
service resolves the endpoint, merges any request-time context, fetches enabled policies
(cache-first), and runs the pure engine. The decision is audited best-effort.

```mermaid
sequenceDiagram
    autonumber
    participant E as Enforcement point
    participant MW as Middleware (service token)
    participant H as access handler
    participant AS as access.Service
    participant ES as endpoint source (GetByMAC)
    participant PS as policy source (EnabledPolicies)
    participant CA as Cache
    participant DB as Postgres
    participant EN as Engine (pure)
    participant AU as audit.Recorder

    E->>MW: POST /api/v1/access/evaluate (service token)
    MW->>MW: requireServiceToken (constant-time)
    MW->>H: authorized
    H->>AS: Evaluate(ctx, {mac, ip?, posture?, attrs?})
    AS->>ES: GetByMAC(mac)
    alt found
        ES-->>AS: endpoint
    else not found
        ES-->>AS: ErrNotFound
        AS->>AS: synthesize minimal unknown endpoint
    end
    AS->>AS: merge request-time overrides (ip/posture/attributes)
    AS->>PS: EnabledPolicies(ctx)
    PS->>CA: GetEnabled()
    alt cache hit
        CA-->>PS: policies
    else miss / error (fail-open)
        PS->>DB: ListEnabled()
        DB-->>PS: policies
        PS->>CA: SetEnabled(policies)
    end
    PS-->>AS: enabled policies
    AS->>EN: Evaluate(endpoint, policies)
    EN-->>AS: AccessDecision (first match or default deny)
    AS->>AU: Record(access.evaluate, outcome)
    AS-->>H: AccessDecision
    H-->>E: 200 JSON decision
```

The engine sorts policies by `(priority asc, name asc)`, applies first-match-wins with all
conditions AND-ed, and returns a secure **default deny** when nothing matches. See the
[engine algorithm](./low-level-design.md#3-internalaccess--the-policy-engine-and-service).

---

## (e) Sequence: operator login

Login exchanges email + password for a short-lived JWT. The handler verifies the Argon2id
hash in constant time and, on success, issues an HS256 token. Failures return a generic
`401` to resist account enumeration.

```mermaid
sequenceDiagram
    autonumber
    participant C as Client
    participant H as auth handler
    participant OR as OperatorRepository
    participant PW as password (Argon2id)
    participant TI as TokenIssuer (HS256)
    participant AU as audit.Recorder

    C->>H: POST /api/v1/auth/login {email, password}
    H->>OR: find operator by lower(email)
    alt found & enabled
        OR-->>H: operator (with PHC hash)
        H->>PW: Verify(password, hash)  %% constant-time
        alt match
            PW-->>H: ok
            H->>TI: Issue(sub, email, role, ttl)
            TI-->>H: signed JWT
            H->>AU: Record(login, success)
            H-->>C: 200 {token}
        else mismatch
            PW-->>H: fail
            H->>AU: Record(login, failure)
            H-->>C: 401 generic
        end
    else not found / disabled
        OR-->>H: ErrNotFound / disabled
        H->>PW: dummy verify (constant work)
        H->>AU: Record(login, failure)
        H-->>C: 401 generic
    end
```

The dummy-verify branch keeps the response time roughly constant whether or not the account
exists, mitigating timing-based enumeration. See
[AuthN/AuthZ](../security/authn-authz.md).

---

## (f) Sequence: create policy (with cache invalidation)

Creating or mutating a policy must not leave the enabled-policy cache stale. After a
successful write, the policy service invalidates the cache so the next `EnabledPolicies`
read repopulates it from the database.

```mermaid
sequenceDiagram
    autonumber
    participant C as Client (operator)
    participant MW as Middleware (JWT + RBAC)
    participant H as policies handler
    participant PSvc as policy.Service
    participant DB as Postgres (policy_repo)
    participant CA as Cache
    participant AU as audit.Recorder

    C->>MW: POST /api/v1/policies (JWT)
    MW->>MW: requireOperator + requirePermission(policy:write)
    MW->>H: authorized
    H->>H: decodeJSON (strict)
    H->>PSvc: Create(ctx, policy)
    PSvc->>PSvc: validate (name/action/priority/conditions/values)
    PSvc->>DB: INSERT (parameterized, conditions as JSONB)
    alt unique violation
        DB-->>PSvc: ErrConflict (SQLSTATE 23505)
        PSvc-->>H: ErrConflict
        H-->>C: 409 problem
    else inserted
        DB-->>PSvc: policy
        PSvc->>CA: Invalidate(nac:policies:enabled)
        PSvc->>AU: (handler) Record(policy create, success)
        PSvc-->>H: policy
        H-->>C: 201 JSON
    end
```

Every mutation path (`Create`, `Update`, `Delete`) calls `cache.Invalidate`; an invalidation
failure is logged but not propagated, and because the cache fails open the next read still
returns correct data from Postgres.

---

## (g) Entity-relationship diagram

The relational model has four tables created by migration `0001_init`. There are no foreign
keys between them by design — `audit_events` references actors and resources by id/type as
loosely-coupled strings so the append-only log is never blocked by referential integrity.
The authoritative version with column types and indexes lives in
[Database Schema](../database/schema.md).

```mermaid
erDiagram
    OPERATORS {
        uuid id PK
        text email UK "unique lower(email)"
        text password_hash "Argon2id PHC"
        text role "admin|operator|viewer"
        bool enabled
        timestamptz created_at
        timestamptz updated_at
    }
    ENDPOINTS {
        uuid id PK
        text mac UK "unique, normalized"
        text ip
        text hostname
        text device_type
        text access_status "CHECK enum"
        text posture "CHECK enum"
        jsonb attributes "GIN indexed"
        timestamptz created_at
        timestamptz updated_at
    }
    POLICIES {
        uuid id PK
        text name UK "unique lower(name)"
        text description
        text action "CHECK enum"
        int priority "partial idx WHERE enabled"
        bool enabled
        jsonb conditions
        timestamptz created_at
        timestamptz updated_at
    }
    AUDIT_EVENTS {
        uuid id PK
        timestamptz occurred_at "idx desc"
        text actor "idx"
        text action
        text resource_type "idx (resource)"
        text resource_id
        text outcome "success|failure|denied"
        text request_id
        text remote_addr
        jsonb metadata
    }
```

`policies_enabled_priority_idx` is a **partial** index on `(priority) WHERE enabled`,
matching the engine's hot path (read enabled policies ordered by priority). `endpoints` has a
GIN index on `attributes`; `audit_events` is indexed on `occurred_at desc`, `actor`, and
resource columns for fast forensic queries.

---

## See also

- [Architecture Overview](./overview.md) · [Low-Level Design](./low-level-design.md)
- [Business Logic](./business-logic.md)
- [Database Schema](../database/schema.md) · [Migration Guide](../database/migrations.md)
- [AuthN/AuthZ](../security/authn-authz.md) · [Audit Logging](../security/audit-logging.md)
- [Threat Model](../security/threat-model.md) · [Trust Boundaries](../security/trust-boundaries.md)
