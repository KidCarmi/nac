# Pilot API and evidence contract (DRAFT v0, for ASTRA)

Owner: OPUS (product code, packaging, UI, operator docs).
Validation: ASTRA (lab, repeatable tests).
Branch: `claude/cloud-environment-setup-naawdu` on `KidCarmi/nac`.
Starting commit: `4911d2d940797aeb41be3815af1cf23b25573dc1`.

Status: **draft, may change until a candidate commit is tagged.** Breaking
changes will be listed in the "Changes" section with the commit that made them.

## 1. What OPUS needs from ASTRA first

The lab deliverables are not reachable from OPUS's environment. Please commit
them (sanitized: secrets and keys removed) to a branch, for example
`astra/lab-evidence` under `lab/`:

- `report.md`, `evidence.md`, `rerun.md`
- sanitized vEOS-lab 4.29.2F running-config (dot1x, aaa, radius-server,
  dynamic-authorization, interface sections)
- the FreeRADIUS config and the lab adapter source and logs
- one captured CoA exchange: request attributes, ACK/NAK, and what the switch
  did (reauth, port bounce, or VLAN change)
- `show dot1x hosts detail | json` output (or equivalent) for one authenticated
  host. OPUS needs it to parse the switch-observed state.
- the test CA certificate only, never its key

Until then, the EOS config template and the CoA attribute set are marked
**unverified**.

## 2. Components and ports

| Component | Listens | Called by |
|---|---|---|
| FreeRADIUS 3.2 (EAP-TLS) | UDP 1812 auth, 1813 acct | switch |
| nacd | TCP 8080 (HTTP API and UI) | FreeRADIUS `rest`, operators |
| switch dynamic authorization | UDP 3799 | nacd (CoA/Disconnect) |

FreeRADIUS validates the certificate. nacd makes the authorization decision.
The switch enforces it.

## 3. FreeRADIUS → nacd (service-token Basic auth)

The bodies use FreeRADIUS `rest` `body = json` attribute format. The switch is
identified by the packet source IP (`?client=`), never by `NAS-IP-Address`.

| Call | When | nacd answer |
|---|---|---|
| `POST /api/v1/radius/authorize?client=<ip>` | post-auth, after EAP-TLS succeeded | 200 + VLAN reply attributes (accept), or 401 (reject) |
| `POST /api/v1/radius/reject?client=<ip>` | Post-Auth-Type REJECT (bad certificate, etc.) | 204, records the failure reason |
| `POST /api/v1/radius/accounting?client=<ip>` | Start / Interim / Stop | 204 |
| `GET /api/v1/radius/clients/<ip>` | dynamic client lookup | 200 with secret, or 404 |

Accept reply: `Tunnel-Type=VLAN`, `Tunnel-Medium-Type=IEEE-802`,
`Tunnel-Private-Group-Id=<vlan>`, `Class=nac:<decision-id>`. The switch echoes
`Class` in accounting, which correlates the session.

## 4. Decision record (what tests should assert on)

`GET /api/v1/sessions/{id}` and `GET /api/v1/decisions/{id}` return:

- `inputs`: each field has `value`, `source`, `observed_at`, `authenticated`,
  `status` (`present` | `missing` | `stale`)
- `policy_version` (number and id), `matched_rule`, and `trace` with one
  outcome per rule (`matched` | `not_matched` | `insufficient_evidence` | ...)
- `requested`: the action, segment and VLAN that NAC returned
- `observed`: the VLAN and status the switch reported, with source and time,
  or explicitly `unavailable` / `stale`

Missing-evidence rule: a condition on a missing or stale input never matches
(`eq`, `ne`, `in`). `exists` is false, and the new `absent` operator is true.

## 5. Outage behavior (to be confirmed by ASTRA tests)

When nacd is unreachable, FreeRADIUS reuses the last successful authorization
for the same MAC, certificate serial and switch while it is younger than
`NAC_OUTAGE_CACHE_TTL`. Otherwise it applies `NAC_OUTAGE_FALLBACK`
(`reject` by default, or `quarantine`). Each outage decision is written to the
FreeRADIUS `nac-outage` log.

## 6. CoA

`POST /api/v1/sessions/{id}/quarantine`, `.../restore` and `.../reauthenticate`
target the session by switch, `Acct-Session-Id`, `Calling-Station-Id` and
`NAS-Port-Id`, captured when the request is made. The states are `requested`,
`sent`, `acked`, `nak`, `timeout`, `failed` and `observed`. A retry is never
re-targeted to a different session.

## Changes

- v0 (this file): initial draft.
