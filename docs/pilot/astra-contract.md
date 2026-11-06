# Pilot API and evidence contract (v3, for ASTRA)

Owner: OPUS (product code, packaging, UI, operator docs).
Validation: ASTRA (EVE lab qualification, repeatable tests).
Branch: `claude/cloud-environment-setup-naawdu` on `KidCarmi/nac`.
Starting commit: `4911d2d940797aeb41be3815af1cf23b25573dc1`.
Lab evidence used: `astra/lab-evidence` at `f22c9dd` and `694ed54`.

## 0. Candidate register

A candidate exists only when an **annotated tag `pilot-rc<N>`** points at its
SHA. That SHA is announced in the handoff message and the PR. Every other
commit is work in progress.

| Ref | Status |
|---|---|
| `4911d2d` | ASTRA lab baseline (old NAC plus lab adapter), not a product candidate |
| `fd604b0`, `94ee467`, `44a3e95`, `393847e`, `da0f99a`, `7bdbcf7`, `4203c71` | **non-candidates** (development commits) |
| tag `pilot-rc1` | first candidate (see handoff) |

What a candidate contains:
- the install guide (`docs/pilot/installation.md`)
- the walkthrough (`docs/pilot/operator-walkthrough.md`)
- the known limitations (`docs/pilot/known-limitations.md`)
- the FreeRADIUS 3.2.5 adapter (`deployments/freeradius`)
- the installer (`deployments/pilot/install.sh`)
- console and API workflows for register, map, bind, diagnose, preview,
  publish, restore and CoA

## 1. Installation inputs for the clean lab node

- **Artifact:** `nac-pilot-<version>.tar.gz` built by `make pilot-bundle` at
  the tag, plus its `.sha256`. Copy both to the node; the bundle contains the
  prebuilt nacd.
- **Packages:** the Ubuntu 24.04 archive only (`archive.ubuntu.com`,
  `security.ubuntu.com`), through
  `install.sh --proxy http://10.70.99.254:3128`. The proxy is used for that
  apt run only and is not persisted.
- **Go hosts:** not needed with the bundle. Only a build on the node needs
  `proxy.golang.org`/`sum.golang.org`.
- **Exact installed versions:** recorded in `/etc/nac/install-report.txt`,
  with the nacd and adapter SHA-256.
- **Certificates:** `--ca <lab test CA public cert> --server-cert --server-key`
  (server certificate issued by the lab CA), or `--test-pki` for a throwaway
  CA.

## 2. Components and ports

| Component | Listens | Called by |
|---|---|---|
| FreeRADIUS 3.2.5 (EAP-TLS) | UDP 1812 auth, 1813 acct | switch |
| nacd HTTP (FreeRADIUS only) | `127.0.0.1:8080` | FreeRADIUS `rest` |
| nacd HTTPS (console + API) | TCP 8443 | operators, test runner |
| switch dynamic authorization | UDP 3799 | nacd (CoA-Request) |
| switch eAPI (optional) | TCP 443 | nacd (`show dot1x hosts \| json`) |

## 3. FreeRADIUS → nacd

`rlm_rest`, `body = json`. The service token is sent as the HTTP Basic
password. The switch is identified by the packet source IP (`?client=`).

| Call | When | nacd answer |
|---|---|---|
| `POST /api/v1/radius/authorize` | post-auth, after EAP-TLS succeeded | 200 with `control:Tmp-String-1` = `nac-accept` (+ Tunnel VLAN, `Class=nac:<decision_id>`) or `nac-reject` |
| `POST /api/v1/radius/reject` | Post-Auth-Type REJECT not decided by NAC (certificate failures) | 204 |
| `POST /api/v1/radius/accounting` | if the switch sends accounting | 204 |

Anything other than an HTTP 200 decision is an outage, handled in section 6.

## 4. Records and stable identifiers

| Object | ID | Endpoint |
|---|---|---|
| Switch | `switch.id` (UUID) | `GET /api/v1/switches/{id}` (status: configured / reachable / auth_request_seen / verified) |
| Device | `endpoint.id` (UUID) | `GET /api/v1/endpoints/{id}` |
| Identity binding | `binding.id` + `revision` | `GET /api/v1/identity-bindings` (issuer + normalised serial + optional CN → device) |
| Session | `session.id` (UUID): switch + `NAS-Port-Id` + MAC | `GET /api/v1/sessions/{id}` |
| Decision | `decision.id` (UUID), also in `Class=nac:<id>` | `GET /api/v1/decisions/{id}` |
| CoA action | `coa.id` (UUID) | inside the session detail `coa_requests[]` |
| Policy version | integer `number` + `id` (immutable) | `GET /api/v1/policy-versions/{n}` |
| Timeline event | `event.id` | `GET /api/v1/auth-events?stage=&result=&mac=` |

The session detail contains:
- `requested`: action, segment, VLAN and decision
- `observed`: freshness `fresh`/`stale`/`unavailable`, status, VLAN, source,
  time, and `matches_requested`
- `liveness`: see section 5
- `last_decision`, with `inputs.<field>` = {value, source, observed_at,
  authenticated, status}, plus `trace`, `policy_version`, `matched_rule` and
  `insufficient_evidence`
- `timeline[]`, `coa_requests[]` and `explanation[]`

Missing-evidence rule: `eq`, `ne` and `in` on missing or stale evidence never
match, and the rule is skipped and reported. `exists` is false and `absent` is
true. Posture is never taken from a RADIUS request.

## 5. Session end, staleness and liveness without accounting or eAPI

The validated lab sends no accounting and has no eAPI path. In that setup:

| Question | Behaviour |
|---|---|
| When does NAC end a session? | Only on accounting Stop, or when a different certificate authenticates on the same port and MAC. Otherwise the session stays `active` after the endpoint leaves. |
| What does `active` mean then? | The last authentication was accepted. `liveness.state` is **`unknown`**, with the age of the last authentication. |
| Switch-observed state | `observed.freshness = "unavailable"` (never guessed). With eAPI: `fresh` up to `NAC_OBSERVATION_MAX_AGE` (5 min), then `stale`. |
| `liveness` values | `ended` (NAC ended it), `connected` (fresh eAPI SUCCESS for port+MAC), `absent` (fresh eAPI, MAC not on port), `unknown` (neither) |
| Verify | "decision only" without eAPI. The detail text says the switch side was not observed. |

## 6. Outage behavior (no authorization cache)

| Question | Answer |
|---|---|
| Authorization cache? | **None**: not in nacd, not in FreeRADIUS. TLS session resumption is disabled. |
| TTL / default | Not applicable; there is nothing to enable. |
| Revocation | Every login needs a live decision, so unbinding, quarantine and policy changes apply at the next login. CRL is enforced by FreeRADIUS before nacd is asked. |
| Rejected certificate | Never reaches nacd and never reuses anything. |

| While nacd is unreachable | Result |
|---|---|
| Existing authorized session | Kept by the switch until it re-authenticates |
| New login or re-authentication | Access-Reject, `Reply-Message "NAC decision service unavailable"` |
| Accounting | Answered by FreeRADIUS, logged as not forwarded |
| Recovery | The next authentication gets a live decision |

Provenance: FreeRADIUS writes `/var/log/freeradius/nac-outage.log` (JSON
lines). nacd imports them every 30 s, idempotently, as timeline events
`stage=outage`, `source=freeradius-outage-log`, with switch, port, MAC and
certificate serial. They are never NAC decisions.

ASTRA expectations:
1. Existing session keeps access.
2. New login rejected with that message.
3. Login succeeds after recovery with no manual action.
4. Imported outage events show the right switch, port and MAC.

## 7. CoA

`POST /api/v1/sessions/{id}/quarantine | restore | reauthenticate` (operator
JWT, permission `session:act`). The session must be `active`; one CoA in
flight per session.

1. NAC records the requested state first (quarantine sets an override,
   restore clears it). It applies at the next authentication even if the CoA
   fails.
2. NAC sends a **CoA-Request** with the attribute set vEOS ACKed in the lab
   (request ID 172):
   - `User-Name`
   - `Calling-Station-Id`, byte-for-byte as the switch sent it
   - `NAS-IP-Address`, as the switch sent it, else the registered NAS IP
   - `NAS-Port-Id`
   - `Tunnel-Type=VLAN`, `Tunnel-Medium-Type=IEEE-802`,
     `Tunnel-Private-Group-Id=<requested VLAN>`
3. States:

   | State | Meaning |
   |---|---|
   | `requested` | Recorded |
   | `sent` | On the wire |
   | `acked` | CoA-ACK (Error-Cause recorded) |
   | `nak` | CoA-NAK, with Error-Cause in operator language |
   | `timeout` | No valid response after `NAC_COA_RETRIES`+1 attempts of `NAC_COA_TIMEOUT` |
   | `failed` | Transport error, target changed, or restart mid-exchange |
   | `observed` | The switch's next authentication of the **same session** returned the requested VLAN, within 60 s |

**Retry guard:** retransmissions are byte-identical (same Identifier and
authenticator). Before each one, NAC requires the captured session to still be
current (not ended, same start time, same port); otherwise it fails the CoA
and never re-targets. Without accounting NAC cannot see the endpoint leave, so
the switch is the authoritative guard. The request names switch, port and MAC,
so it cannot apply to another endpoint, and a departed endpoint yields NAK 503
(expected from RFC 5176; not yet captured on vEOS).

**Restore and policy versions:** publishing or restoring a policy version
never clears session overrides.

Expected lab observation: CoA-ACK, then a switch-initiated Access-Request
about 0.2 s later, then the session's CoA becomes `observed`. TCP reachability
needs the endpoint to renew its IP address, and lags 17–22 s in EVE.

## 8. Policy versions

| Call | Purpose |
|---|---|
| `GET /api/v1/policy-draft` | Draft rules, digest, validation |
| `POST`/`PUT`/`DELETE /api/v1/policies[/{id}]` | Edit draft rules |
| `GET /api/v1/policy-draft/preview` | Predicted changes over the latest recorded decision of each session (labelled prediction); `rows[].case.decision_id` references the recorded inputs used |
| `POST /api/v1/policy-draft/publish` `{expected_digest, note}` | Atomic activation of exactly the previewed draft (409 otherwise) |
| `POST /api/v1/policy-versions/{n}/restore` `{reset_draft, note}` | Re-activation, recorded as a new activation |
| `GET /api/v1/policy-versions`, `GET /api/v1/policy-activations` | Immutable history |

Publication applies at the next authentication. Existing sessions change only
through a session action.

## Changes

- v0 (`ce41af4`): initial draft.
- v1 (`596b882`): no-cache outage baseline, CoA targeting, stable IDs.
- v2 (`df0f1a9`): FreeRADIUS upgrade path, explicit no-cache specification,
  outage provenance.
- v3: implemented contract; installation inputs and proxy; session
  end/liveness without accounting or eAPI; CoA retry guard; actual endpoints.
  Changed: FreeRADIUS reject is a 200 decision with
  `control:Tmp-String-1 = nac-reject` (not HTTP 401), so any non-200 means an
  outage.
