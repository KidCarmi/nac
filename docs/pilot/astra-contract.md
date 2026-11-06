# Pilot API and evidence contract (DRAFT v1, for ASTRA)

Owner: OPUS (product code, packaging, UI, operator docs).
Validation: ASTRA (EVE lab qualification, repeatable tests).
Branch: `claude/cloud-environment-setup-naawdu` on `KidCarmi/nac`.
Starting commit: `4911d2d940797aeb41be3815af1cf23b25573dc1`.

Status: **draft v1, may change until a candidate commit is tagged.** Breaking
changes are listed under "Changes", with the commit that made them.

## 0. Candidate register

A candidate exists only when it is listed here **and** an annotated tag
`pilot-rc<N>` points at its SHA. Every other commit is work in progress.

| Commit | Status |
|---|---|
| `4911d2d` | ASTRA lab baseline (old NAC plus lab adapter), not a product candidate |
| `fd604b0` | **non-candidate**: evidence model only |
| `94ee467` | **non-candidate**: engine evidence semantics |
| `44a3e95` | **non-candidate**: policy versions, API only |

No candidate is named until a single SHA has all of these:
- installer and operator guide
- the supported FreeRADIUS adapter
- API and UI workflows for register, map, preview, publish, restore and CoA
- the outage semantics in section 5
- the stable IDs in section 7

## 1. Evidence OPUS still needs (branch `astra/lab-evidence`, under `lab/`)

- Sanitized vEOS-lab 4.29.2F config: the dot1x, aaa, radius-server,
  dynamic-authorization and interface sections.
- Exact CoA exchange: packet code (CoA-Request 43 or Disconnect 40), the full
  attribute list of the NAK'd and the ACK'd request, the ACK attributes, and the
  switch log lines.
- `show dot1x hosts detail | json` (or the closest equivalent) for one
  authenticated host. OPUS needs it to parse switch-observed state.
- The old adapter's FreeRADIUS config, so OPUS can reuse the proven pieces.
- The test CA certificate only, never its key.

Until these arrive, the EOS config template and the eAPI observation parser
are marked **unverified**.

## 2. Components and ports

| Component | Listens | Called by |
|---|---|---|
| FreeRADIUS 3.2 (EAP-TLS) | UDP 1812 auth, 1813 acct | switch |
| nacd | TCP 8080 (HTTP API and UI) | FreeRADIUS `rest`, operators |
| switch dynamic authorization | UDP 3799 | nacd (CoA) |
| switch eAPI (optional) | TCP 443 | nacd (session observation) |

FreeRADIUS validates the certificate. nacd decides. The switch enforces.

## 3. FreeRADIUS → nacd (service token as HTTP Basic password)

The bodies use the FreeRADIUS `rest` `body = json` attribute format. The switch
is identified by the packet source IP (`?client=`), never by `NAS-IP-Address`.

| Call | When | nacd answer |
|---|---|---|
| `POST /api/v1/radius/authorize?client=<ip>` | post-auth, after EAP-TLS succeeded | 200 with VLAN reply attributes (accept), or 401 (reject) |
| `POST /api/v1/radius/reject?client=<ip>` | Post-Auth-Type REJECT (bad certificate, etc.) | 204, records the failure reason |
| `POST /api/v1/radius/accounting?client=<ip>` | Start / Interim / Stop | 204 |

Accept reply: `Tunnel-Type=VLAN`, `Tunnel-Medium-Type=IEEE-802`,
`Tunnel-Private-Group-Id=<vlan>`, `Class=nac:<decision_id>`.

## 4. Decision record (what tests should assert on)

`GET /api/v1/sessions/{id}` and `GET /api/v1/decisions/{id}` return:

- `inputs`: each field has `value`, `source`, `observed_at`, `authenticated`,
  and `status` (`present` | `missing` | `stale`)
- `policy_version` and `policy_version_id`, `matched_rule`, and `trace` with
  one outcome per rule (`matched` | `not_matched` | `insufficient_evidence` |
  `disabled` | `not_reached`)
- `requested`: the action, segment and VLAN NAC returned
- `observed`: the state the switch reported, with source and time, or
  explicitly `unavailable` / `stale`

Missing-evidence rule: `eq`, `ne` and `in` on a missing or stale input never
match; the rule is skipped and reported. `exists` is false and `absent` is
true.

## 5. Outage behavior (agreed baseline: no authorization cache)

ASTRA's regression on `4911d2d` showed the lab adapter had no authorization
cache. The pilot keeps that fail-closed behavior, and the product caches no
authorizations.

| While nacd is unreachable | Result |
|---|---|
| Existing authorized session | Kept by the switch until its next re-authentication (switch session persistence; NAC is not involved) |
| New login, or re-authentication of a cleared session | FreeRADIUS sends **Access-Reject** with `Reply-Message` "NAC decision service unavailable" |
| nacd recovers | Normal decisions resume on the next authentication; the supplicant retries on its own |

Each outage reject is written to the FreeRADIUS `nac-outage` log. Operators set
the switch `dot1x timeout reauth-period` to the outage they want existing
sessions to survive.

ASTRA test expectation: the three rows above.

Known lag: ASTRA measured 17–22 s from switch SUCCESS to TCP reachability.
The product reports the switch's view (`observed`) and does not claim
end-to-end reachability.

## 6. CoA

The product action is `POST /api/v1/sessions/{id}/quarantine`, `.../restore`
or `.../reauthenticate`. NAC first records the requested authorization (a
session override for quarantine/restore), then sends a **CoA-Request** asking
the switch to re-authenticate. That next authentication returns the new VLAN.

Target attributes come from ASTRA's lab. vEOS answered NAK 402 (Missing
Attribute) until both of the first two were present:

- `NAS-IP-Address`: the switch's RADIUS address
- `NAS-Port-Id`: the switch interface, e.g. `Ethernet1`
- `Calling-Station-Id`: the session MAC, so the wrong host is never targeted

States: `requested` → `sent` → `acked` | `nak` (with Error-Cause) | `timeout`,
then `observed` (the next authentication returned the requested VLAN) or
`failed`.

Retransmissions reuse the same RADIUS Identifier and authenticator. Before each
retry, NAC checks the session is still active. If it ended, the request fails
with `target session ended` and is never re-targeted to a new session.

Open item for ASTRA: after the VLAN change, the endpoint needed a new IP
before corp opened. Does vEOS 4.29.2F accept a port-bounce CoA, which would
force DHCP renewal? Until that is confirmed, IP renewal is a documented
operator step and a known limitation.

## 7. Stable identifiers

| ID | Created | Stable until |
|---|---|---|
| `session_id` (UUID) | first Access-Request for switch + `NAS-Port-Id` + MAC | accounting Stop, or the first login after Stop |
| `decision_id` (UUID) | each authorization decision | forever; also echoed by the switch in `Class=nac:<decision_id>` |
| `coa_request_id` (UUID) | each CoA action | forever |
| `policy_version` (integer) | each publish | forever (versions are immutable) |

## 8. Policy version API (available from `44a3e95`)

| Call | Purpose |
|---|---|
| `GET /api/v1/policy-draft` | draft rules, digest, validation |
| `POST`/`PUT`/`DELETE /api/v1/policies[/{id}]` | edit draft rules |
| `GET /api/v1/policy-draft/preview` | predicted changes over recorded inputs |
| `POST /api/v1/policy-draft/publish` `{expected_digest, note}` | atomic activation; 409 if the draft changed after preview |
| `POST /api/v1/policy-versions/{n}/restore` `{reset_draft, note}` | re-activate version n |
| `GET /api/v1/policy-versions`, `GET /api/v1/policy-activations` | history |

Publication applies at the **next authentication**. Existing sessions change
only through a session action (section 6). Operator quarantine overrides
survive publish and restore.

## Changes

- v0 (`ce41af4`): initial draft.
- v1: outage semantics changed to ASTRA's proven baseline (no cache, fail
  closed); CoA target attributes from the lab NAK/ACK; candidate register;
  stable identifiers; policy version API.
