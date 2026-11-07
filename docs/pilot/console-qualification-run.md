# Single-instance console run (closes the all-console coverage gap)

**Result: PASS on `pilot-rc10`** (ASTRA, 2026-11-07, [evidence `af46f02`](https://github.com/KidCarmi/nac/tree/af46f02eb1f9f5f3bb07ef6ac441f12b6840465d/lab/evidence/clean-ui-rc10-20261107)). Switch **Verify** and the `revoked_token` log/metric were not exercised in that run.

This is the procedure for the single-instance row in
[qualification status](qualification-status.md): one fresh `pilot-rc10`
instance, a real vEOS switch, and every NAC operation done in the console.
Labels and messages below are quoted from the `pilot-rc10` console
(`web/ui/app.js` at `b39649f`). The run passes only if every **Expect** holds.

**Candidate:** `pilot-rc10`, commit `b39649f138e126ed6689a53b590855061042e341`,
nacd SHA-256 `35699eac162cc2a7315efced101bee3ec18ed87ab1cb108f06f9bb1a7b48195e`.
A simulated-switch run (`scripts/ui-walkthrough.js`) does not count.

## What "entirely through the console" covers

- **In the console:** every NAC action: register the switch, create the
  device record, bind, edit the policy, preview, publish, Verify, Quarantine,
  Restore, change the password.
- **Outside the console, and allowed:** things that NAC cannot do and has no
  API for:
  - installation and host configuration (`install.sh`, `/etc/nac/nacd.env`)
  - switch CLI (applying the generated configuration, `write memory`, eAPI
    certificate import)
  - the endpoint (supplicant, DHCP, reachability tests)
  - evidence collection over the API, read-only (`GET`)
- **Not allowed:** any NAC mutation (`POST`, `PUT`, `DELETE`) outside the
  console, except the separate login in step 7 that captures the pre-change
  token.

## Prerequisites beyond the documented installer

1. **Fresh instance:** a new Ubuntu 24.04 host with an empty database,
   installed from `make pilot-bundle VERSION=pilot-rc10` at `b39649f`. Before
   you start, confirm two things:
   - the install report shows commit `b39649f`
   - `sha256sum /usr/local/bin/nacd` equals the hash above
2. **EAP-TLS PKI:** `--test-pki`, or `--ca` / `--server-cert` /
   `--server-key`. The endpoint certificate must chain to the CA that
   FreeRADIUS trusts. Record which option you used.
3. **Switch state:**
   - ZeroTouch Provisioning is cancelled (`show zerotouch`), so
     `write memory` works.
   - UDP 1812/1813 reach the control node from the switch.
   - UDP 3799 (CoA) reaches the switch from the control node.
4. **Accounting and eAPI are commented out in the generated configuration**
   ("OPTIONAL" block). They are needed for **Still connected?** and for panel
   2 **Observed on the switch**. Apply them by hand on the switch, as in
   [installation §7a](installation.md#7a-optional-session-observation-eapi-and-accounting).
5. **Verified eAPI TLS (host and switch, before step 1):**
   - Import the switch certificate on EOS (`certificate:` / `sslkey:`).
   - Set `SSL_CERT_FILE=/etc/nac/eapi-ca.pem` in `/etc/nac/nacd.env`.
   - Restart nacd.
   - The eAPI URL entered in step 1 must use the address in the certificate's
     IP SAN.
6. **DHCP:** a DHCP server on the corp and quarantine VLANs. Record the lease
   time (earlier runs used 120 s).
7. **First sign-in:** use the initial admin password from the install
   report.

## Steps and expected results

### 1. Onboard the switch (Switches)

On **Register a switch**, fill in:
- **Name**
- **RADIUS source IP (as FreeRADIUS sees it)**
- **RADIUS shared secret** (16+ characters)
- **Corp VLAN** 10 and **Quarantine VLAN** 20
- **Access ports (comma-separated)**
- **FreeRADIUS address for the switch config**
- **CoA UDP port** 3799
- **eAPI URL (session observation)** as `https://<IP in the SAN>`, plus
  **eAPI username** and **eAPI password**

Then click **Register**.

**Expect:**
- The flash reads "Switch registered. Apply the generated EOS configuration
  next."
- The switch page opens.
- **Generated EOS configuration** contains `dot1x system-auth-control`,
  `dot1x dynamic-authorization`, `vlan 10`, `vlan 20` and the access-port
  stanza.
- **Configured** shows yes.

Next:
1. Use **Show with secret**. It opens an in-page confirmation, and the
   reveal is audited.
2. Apply the configuration, plus the accounting and eAPI lines (prerequisite
   4).
3. Run `write memory`.
4. Click **Probe management (eAPI)**.

**Expect:**
- The flash reads "Probe finished."
- **Reachable** shows yes with verified TLS.
- **Auth request seen** shows no until the first RADIUS request arrives.

### 2. First authentication: valid but unbound (Sessions)

Connect the endpoint (wired EAP-TLS).

**Expect:**
- **Sessions** lists it with state `rejected` and the certificate CN.
- The session page **Why** explains that the certificate is not bound to a
  device record.
- **Rule evaluation** shows the version 1 result. Version 1 is the initial,
  empty policy, so the result is the default action, `deny`.
- **Auth request seen** on the switch now shows yes.

### 3. Device record and certificate binding (Devices, then the session)

1. **Devices → Add a device record**. Enter **MAC (as the switch reports it)**
   as the MAC shown on the session, plus a **Hostname**, then click
   **Add device**.
2. Open the rejected session. In **Bind this certificate to a device**,
   choose the record and click **Bind**.

**Expect:**
- The flash reads "Bound. It applies at the next authentication."
- **Identity and device → Binding** shows a binding id with `rev 1`.
- The binding is keyed on the issuer and serial shown in the panel.

### 4. Policy: add, preview, publish (Policy)

1. Click **+ rule** and fill in:
   - **Name** `bound-corp`
   - **Priority (lower first)** `10`
   - **Action** `allow`
   - **Segment** `corp`
2. Click **+ condition** and set `identity.bound` `eq` `true`.
3. Click **Add rule**.

**Expect:** the flash reads "Draft updated (not yet published).", and the
draft shows "✓ draft is valid" and "(differs from active version 1)".

4. Click **Preview changes**.

**Expect:**
- The panel is labelled as a prediction.
- **Compared with** reads "active version 1".
- The endpoint's row changes from `deny` to `allow corp`.
- **Changed decisions** is at least 1.

5. Enter a change note, then click **Publish this draft** and confirm it in
   the in-page dialog.

**Expect:**
- The flash reads "Published."
- **Versions** shows version 2 as active, created by the admin.
- **Activation history** shows a publish from 1 to 2.

### 5. Corp admission and Verify (Sessions, Switches)

The rejected session cannot be re-authenticated from NAC: the CoA buttons
are disabled unless the state is `active`. Wait for the next 802.1X attempt
from the endpoint or the switch. If none arrives, restart the endpoint's
supplicant; that is an endpoint action, not a port bounce. Record which one
happened.

**Expect:**
- The session on that port is `active`. It is either the same row or a new
  one at the top of **Sessions**.
- Panel **1. Switch authorization** shows Segment `corp`, VLAN 10 and
  Override `none`.
- Panel **2. Observed on the switch** shows Freshness `fresh`, Status
  `SUCCESS` and VLAN 10.
- Panel **3. Endpoint reachability** shows State `not_observed`.
- **Rule evaluation** shows `bound-corp` matched under policy version 2.

On the switch page, click **Verify (after a test login)**.

**Expect:** the flash reads "Verification recorded.", and **Verified
(record)** shows yes, marked historical, naming this session.

### 6. Quarantine, then Restore (session page)

1. Click **Quarantine**. The in-page dialog names the switch, port and MAC,
   and warns about a brief pause and DHCP renewal. Confirm it.

**Expect:**
- The flash reads "Requested. Following the CoA…".
- The CoA table moves through `requested` → `sent` → `acked` → `observed`.
  The page refreshes itself while the CoA is in flight.
- **By** shows the admin's email.
- Panel 1 shows Segment `quarantine`, VLAN 20, Override
  `quarantine by <admin>`.
- Panel 2 shows `SUCCESS` VLAN 20 (fresh).
- Panel 3 stays `not_observed`.
- Measure separately, at the endpoint, when the old segment is denied and
  when the new one becomes reachable.

2. Click **Restore** and confirm it.

**Expect:**
- The CoA reaches `observed`.
- Panel 1 shows Segment `corp`, VLAN 10, Override `none`.
- Panel 2 shows VLAN 10.
- Panel 3 shows `not_observed`.

**Optional (not part of the gap):** on **Policy → Versions**, click
**Restore** on version 1 and confirm it.

**Expect:**
- The flash reads "Version 1 restored.".
- **Activation history** shows a restore.
- No CoA is sent, and the change applies at the next authentication.

### 7. Account rotation with a retained pre-change token

1. **Before rotating**, outside the console, get a separate token and check
   that it works:

   ```bash
   T=$(curl -sk https://<node>:8443/api/v1/auth/login -d '{"email":"<admin>","password":"<current>"}' | jq -r .token)
   ```

   ```bash
   curl -sk -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $T" https://<node>:8443/api/v1/auth/me
   ```

   **Expect:** `200`.
2. In the console, click your email at the top right to open **Account**.

   **Expect:** Role `admin`, the permissions list, and **Password last
   changed** "never (initial password)".
3. Fill in **Current password**, **New password (at least 12 characters)**
   and **Repeat new password**, then click **Change password**.

   **Expect:**
   - The flash reads "Password changed. Every other session of this account
     is signed out."
   - The console stays signed in, because it received a fresh token.
   - **Password last changed** shows the current time.
4. Check, outside the console:

   | Check | Expect |
   |---|---|
   | `GET /api/v1/auth/me` with `$T` (the retained token) | `401`, `application/problem+json` |
   | nacd log (`journalctl -u nacd`) | `authentication failed` with `reason=revoked_token` |
   | `/metrics` | `nac_auth_failures_total{reason="revoked_token"}` increased |
   | `POST /api/v1/auth/login` with the old password | `401` |
   | `POST /api/v1/auth/login` with the new password | `200` |

## Evidence to keep

- Screenshots of each **Expect**, or a browser recording of the run.
- The `GET /api/v1/sessions/<id>` JSON after steps 5 and 6.
- The `GET /api/v1/audit` export. It must attribute every mutation above to
  the admin (`switch.*`, `binding.*`, `policy.*`, `session.*`,
  `operator.login`, `operator.password_change`).
- The install report and the nacd hash.
- The switch's `show running-config` and `show startup-config | section dot1x`.
- Endpoint DHCP captures and reachability timings.

A failed **Expect** is a defect report only if it reproduces. Include the
step, what the console showed, and the session JSON.
