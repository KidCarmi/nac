# Proposed pilot API and evidence contract for OPUS and ASTRA

Status: **aligned with OPUS draft v2 (`df0f1a9`) on outage semantics and version boundary; candidate commit still pending**. OPUS identified `fd604b043dc50c5edf9ff94d6184cb165909027d`, `94ee467` and `44a3e95` as non-candidates. The candidate register requires both a named SHA and annotated `pilot-rc<N>` tag. ASTRA owns independent EVE validation; OPUS owns product APIs, installation, UI and operator documentation. No candidate-only step will be marked PASS until it is exercised through the documented product workflow.

## Candidate handoff

OPUS supplies: immutable commit SHA and annotated release-candidate tag, branch/PR, installation guide, clean-install procedure and prerequisites, supported FreeRADIUS adapter version/hash, API or UI routes for switch registration, segment mapping, endpoint/session diagnosis, draft preview, publish, restore and CoA, plus explicit outage behavior and known limitations. The candidate targets FreeRADIUS 3.2.5 on Ubuntu 24.04; the lab's proven 3.0.26 needs a documented upgrade and is not a supported candidate version. A replacement candidate gets a new SHA and a new result directory; previous results remain unchanged.

## Correlation fields

For each EAP-TLS attempt or CoA operation, product events should expose stable identifiers and UTC timestamps for:

| Object | Minimum evidence fields |
| --- | --- |
| Authenticated identity | Certificate issuer, serial and fingerprint or equivalent validated identity; source `FreeRADIUS EAP-TLS`; validation time; mapping record ID/version. Outer EAP User-Name and Calling-Station-Id are separate, untrusted claims. |
| Device | NAC endpoint ID and registered MAC; evidence used to bind validated identity to that record; any posture source and observation time. Missing evidence must remain missing. |
| Switch and session | Product switch ID, NAS IP, port ID, Calling-Station-Id, Acct-Session-Id if available, observed VLAN/state, observation source/time and freshness. |
| Decision | Decision ID, policy version ID, matched rule ID, input snapshot/reference with provenance and freshness, requested segment and mapped VLAN, evaluation and authorization time. |
| RADIUS | Request correlation ID, switch source IP, EAP outcome, Access-Accept/Reject, returned Tunnel attributes and timestamp. Do not log secret or private-key material. |
| CoA | Action ID, operator, exact target identifiers, requested segment/VLAN, request ID, transport result (ACK/NAK/timeout), switch-triggered reauthentication, observed switch state/VLAN, TCP observation and times. Acknowledged is distinct from observed enforcement. |
| Policy change | Draft version ID, captured-event IDs used for preview, predicted decision and uncertainty; published immutable version ID, activation time, old version ID, restore target and semantics for existing sessions. |

The runner records endpoint fresh TCP connection attempts with explicit source IP, destination/port, outcome and timestamps, plus any pre-existing TCP stream outcome. It preserves EAPOL and RADIUS pcaps, sanitized switch/RADIUS configuration, binary hashes and an action transcript. Each step has an expected result, actual result, PASS/FAIL/BLOCKED classification and evidence links.

OPUS's `fd604b0` model names the evidence sources `eap-tls`, `nas`, `nac-binding`, `endpoint-record`, `posture`, `operator` and legacy `evaluate-request`. Its `Evidence` fields are `value`, `source`, `observed_at`, `authenticated`, `status` (`present`, `missing`, `stale`) and `note`. The catalog includes `identity.authenticated`, `identity.subject_cn`, `identity.issuer`, `identity.serial`, `identity.bound`, `mac.matches_device`, `nas.switch`, `nas.port`, `posture` and `session.override`. The candidate API should expose these exact names or document a stable translation. The catalog currently allows `evaluate-request` as a source for `posture` in the legacy API; the supported RADIUS path must never let a client or switch-supplied posture claim use that route.

## Outage semantics agreed in OPUS draft v2, pending candidate validation

1. An already authorized session retains its last switch-enforced access until its ordinary reauthentication/expiry, CoA or disconnect. The product must show that its decision service is unavailable and that the switch observation is stale or independent of the current NAC state; it must not imply a new decision was made.
2. A new EAP-TLS login fails closed when the decision service cannot answer, with Access-Reject and Reply-Message `NAC decision service unavailable`. There is no authorization-result cache, TTL or cache enable setting. A rejected certificate cannot reuse authorization; certificate validation happens before NAC. Each outage reject must be in the FreeRADIUS outage log and later imported to NAC with source `freeradius-outage-log`, never represented as a new NAC decision.
3. After recovery, a new authentication must obtain a current decision. The lab's supplicant retried automatically about 20 seconds after NAC returned; the switch showed VLAN 10 before one fresh TCP probe became reachable. Candidate acceptance needs an agreed convergence deadline measured by continuous probes.

## Proposed test boundaries

- The product installer and web/API operator flow are the action under test. Direct shell edits or database updates are diagnostic or setup work and are logged as manual intervention, not product workflow PASS.
- The endpoint has one data NIC and no management interface. During isolation checks, use source addresses for both target subnets on that one NIC so an off-subnet return path cannot create a false blocked result. Refresh the primary address after a VLAN change and record it.
- Service health is proven from each target bridge before access tests; temporary EVE bridge IPs used for health checks are removed from access/corp before endpoint probes. EVE IP forwarding remains off.
- CoA tests continuously probe both TCP services before, during and after the action. No operator `clear dot1x`, endpoint reconnect or `wpa_cli reauthenticate` may occur in that interval. A switch-triggered EAP/RADIUS exchange is expected and separately recorded.
- On this EOS build, the structured switch command is `show dot1x hosts | json`; `show dot1x hosts detail | json` is invalid. The observed JSON shape is in the ASTRA evidence branch. CoA Request 43 needs `NAS-IP-Address`, `NAS-Port-Id` and `Calling-Station-Id` to target Et1; the corrected lab request got ACK 200 and switch-initiated EAP/RADIUS reauthentication. An ACK is only transport acceptance; a later same-session authorization and endpoint TCP decide enforcement.
- MAB and invalid EAP-TLS fallback remain BLOCKED on vEOS-lab 4.29.2F, which rejects `dot1x mac based authentication`; only suitable authorized physical Arista hardware can close those cases.
