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
   (audited), then apply it on the switch and **save it** (`write memory`).

   **Before you apply it, check ZeroTouch Provisioning.** A factory-fresh EOS
   switch (vEOS-lab included) boots in ZeroTouch Provisioning. In that state
   `write memory` fails, so the configuration you applied is lost at the next
   reboot. On the switch, `show zerotouch` reports the state. If it is active,
   run Arista's `zerotouch cancel`, which reloads the switch. Then apply the
   configuration, run `write memory`, and confirm it was saved:
   `show startup-config | section dot1x` should list the dot1x lines.
   The pilot lab hit this on vEOS-lab 4.29.2F after a full restart.
3. The switch page shows four separate facts:

   | Fact | Becomes "yes" when |
   |---|---|
   | Configured | NAC has the switch, secret and VLAN mappings |
   | Reachable | eAPI answered a probe (shows "unknown" without eAPI) |
   | Auth request seen | A RADIUS request from this switch reached NAC |
   | Verified (record) | You ran **Verify** after a test endpoint was granted access. This is a **record** of that check, marked historical: it names the session used and says whether that session is still active. It does not describe the current state. For that, use Reachable and the sessions' observation and liveness. |

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
- Three separate facts, in three panels:
  1. **Switch authorization**: what NAC told the switch (segment, VLAN,
     override, decision).
  2. **Observed on the switch**: the switch's own report through eAPI
     (status, VLAN, freshness `fresh` / `stale` / `unavailable`) and whether
     the endpoint is still connected. Observation is never guessed.
  3. **Endpoint reachability**: always `not_observed`. NAC does not test the
     endpoint. A matching switch VLAN does not mean the endpoint has a usable
     address there yet (see section 6).
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

   It is a **prediction based on recorded inputs**. A session under an
   operator quarantine is marked **override applies**. It stays quarantined
   whatever the draft says, but the row still shows what the draft alone
   would decide.
3. **Publish this draft** asks for confirmation in an in-page dialog, then activates exactly the previewed draft, as a new
   immutable version. If someone changed the draft after your preview, it
   refuses.
4. Publishing applies at the **next authentication**. To apply it to a
   connected endpoint now, open its session and click **Re-authenticate**.
   On vEOS-lab 4.29.2F, NAC's CoA interrupted the endpoint for up to about
   58 s, even when its VLAN did not change. So use it only for sessions whose
   VLAN should change (see [known limitations](known-limitations.md)).

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

   **observed** is a RADIUS fact: the switch re-authenticated the endpoint and
   NAC answered with the requested VLAN. It does not prove the endpoint has a
   working address in that VLAN. With eAPI configured, **Observed on the
   switch** also shows the switch's own view (status and VLAN).
3. Check the endpoint, not only NAC. **observed** and a matching switch
   observation prove switch authorization, not endpoint reachability.
   - In the lab, every endpoint lost the old segment within about 1–2.5 s.
   - **Windows Server 2025 (build 26100)** reached the new segment in about
     30–65 s, at the tested 120 s and 1-hour leases.
   - **A Linux (systemd-networkd) endpoint** reached it only when its old
     lease expired, which can take the remaining lease time. That was 30 min
     in one run with a 1-hour lease. The link stays up, and the lab DHCP server
     ignores the client's earlier attempts to keep its old address.
   - Plan lease lengths for your endpoints with the
     [operating envelope](known-limitations.md#operating-envelope-measured-pilot-rc10).
   - NAC does not bounce the port. See
     [known limitations](known-limitations.md#switch-authorization-is-not-endpoint-reachability).
4. **Restore** clears the override and sends a CoA. The re-authentication gets
   what the active policy decides.

If a CoA fails, the override still applies at the session's next
authentication.

## 7. Restore a previous policy version (Policy)

**Versions → Restore** re-activates an earlier version. It is recorded in
**Activation history**, and by default the draft is reset to it. It applies at
the next authentication: to apply it to a connected endpoint now, open its
session and click **Re-authenticate**. Sessions under an operator quarantine
**stay quarantined** until you restore them individually.

## 8. Is the endpoint still connected? (Sessions → session)

**Still connected?** on the session page has four values:

| Value | Basis |
|---|---|
| ended | RADIUS accounting Stop for this session |
| connected | A fresh eAPI observation lists the port and MAC |
| absent | A fresh eAPI observation does not list the port and MAC |
| unknown | Neither accounting nor a fresh observation, so "active" only means the last authentication was accepted |

Setting up eAPI and accounting is in
[installation §7a](installation.md#7a-optional-session-observation-eapi-and-accounting).

## 9. Your account (Account)

Click your email at the top right:
- **Account** shows your role and permissions as the server enforces them.
- **Change password** needs the current password. It signs out your other
  sessions.
- A forgotten password is reset on the control node with
  `sudo nac-reset-password <email>`.

## 10. After a restart or a NAC outage

Nothing needs re-entering in NAC: all state is in the database. **After a
switch reboot**, check that the switch kept the NAC configuration: its
RADIUS target and the eAPI SSL profile must match **Generated EOS
configuration**. If they don't, apply it again and run `write memory`. The
lab switch once came back with an older saved RADIUS target. Logins rejected while
nacd was unreachable appear in the timeline as `outage` events with source
`freeradius-outage-log`. They are not NAC decisions.
