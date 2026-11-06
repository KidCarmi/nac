# NAC pilot: operator walkthrough

Everything below is done in the console at `https://<control-node>:8443/`
(see [installation](installation.md) for first sign-in). Each step names the
page to use.

## 1. Onboard the switch (Switches)

1. **Register a switch**:
   - **name**
   - **RADIUS source IP**: the address FreeRADIUS sees as the packet source; in the lab, the switch's routed interface `10.70.99.1`
   - **RADIUS shared secret**: 16+ random characters
   - **corp** and **quarantine** VLAN IDs, e.g. 10 and 20
   - **access ports**, e.g. `Ethernet1`
   - **FreeRADIUS address**: the control node's address as the switch sees it

   Saving writes the FreeRADIUS client list, and FreeRADIUS restarts automatically.
2. Open the switch. **Generated EOS configuration** shows the configuration validated on
   EOS 4.29.2F. Replace `<RADIUS_SHARED_SECRET>`, or use **Show with secret**
   (audited), then apply it on the switch.
3. The switch page shows four separate facts:

   | Fact | Becomes "yes" when |
   |---|---|
   | Configured | NAC has the switch, secret and VLAN mappings |
   | Reachable | eAPI answered a probe (shows "unknown" without eAPI) |
   | Auth request seen | A RADIUS request from this switch reached NAC |
   | Verified | You ran **Verify** after a test endpoint was granted access |

## 2. Authenticate an endpoint (Sessions, Devices)

1. Connect the endpoint (wired, EAP-TLS with its existing certificate).
2. **Sessions** lists it. A certificate that is valid but not yet bound is
   rejected with the explanation "not bound to any device record".
3. **Devices → Add a device record** with the endpoint's MAC. Open the session
   and click **Bind**. This creates the identity binding (certificate issuer +
   serial + CN → device).
4. The endpoint re-authenticates, or you click **Re-authenticate**. The session
   shows the requested segment and VLAN. Then use **Switches → Verify**.

## 3. Explain a connection (Sessions → session)

The session page answers "why":
- **Why**: identity, binding, decision, policy version, matched rule,
  override, and whether NAC can tell the endpoint is still connected.
- **Requested by NAC** vs **Observed on the switch**. Observation is
  `fresh`, `stale` or `unavailable`; it is never guessed.
- **Decision inputs**: each input's value, evidence status
  (present/missing/stale), source, and when it was observed.
- **Rule evaluation**: every rule's outcome, including rules skipped for
  insufficient evidence.
- **Timeline**: certificate failures, decisions, outage rejects, CoA steps
  and observations, in order.

## 4. Diagnose a rejected certificate (Overview, Events)

**Overview → Recent rejections**, or **Events** filtered to `eap-tls`, shows
FreeRADIUS's reason with a plain-language hint, for example:
- "the client certificate is not issued by the CA FreeRADIUS trusts"
- expired, not yet valid, or revoked
- no client certificate sent

Click the row to open the session on that port.

## 5. Preview and publish a policy change (Policy)

1. Edit the **Draft**: add or change rules. Validation errors block
   publishing. For example, a segment with no VLAN on a switch, or an
   unknown input field.
2. **Preview changes** evaluates the latest recorded inputs of each session
   under the active version and under the draft. It lists:
   - changed decisions
   - cases with insufficient evidence

   It is a **prediction based on recorded inputs**.
3. **Publish this draft** activates exactly the previewed draft, as a new
   immutable version. If someone changed the draft after your preview, it
   refuses.
4. Publishing applies at the **next authentication**. To apply it to a
   connected endpoint now, open its session and click **Re-authenticate**.

## 6. Quarantine an active session, then restore access (Sessions → session)

1. **Quarantine**: NAC records an operator override, then sends a CoA-Request
   (switch, `NAS-Port-Id`, MAC) asking the switch to re-authenticate.
2. The CoA table shows each state separately:

   | State | Meaning |
   |---|---|
   | requested | Recorded in NAC |
   | sent | Sent to the switch (retries are identical and never re-targeted) |
   | acked / nak | The switch accepted / refused; a NAK shows the Error-Cause |
   | timeout | No answer from the switch |
   | observed | The switch's re-authentication received the quarantine VLAN |

3. The endpoint may need to renew its IP address in the new VLAN.
4. **Restore** clears the override and sends a CoA. The re-authentication gets
   what the active policy decides.

If a CoA fails, the override still applies at the session's next
authentication.

## 7. Restore a previous policy version (Policy)

**Versions → Restore** re-activates an earlier version. It is recorded in
**Activation history**, and by default the draft is reset to it. It applies at
the next authentication. Sessions under an operator quarantine **stay
quarantined** until you restore them individually.

## 8. After a restart or a NAC outage

Nothing needs re-entering: all state is in the database. Logins rejected while
nacd was unreachable appear in the timeline as `outage` events with source
`freeradius-outage-log`. They are not NAC decisions.
