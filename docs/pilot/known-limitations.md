# NAC pilot: known limitations

## Scope

- **One site, Arista EOS 4.29.2F only.** The generated configuration was
  validated on vEOS-lab 4.29.2F (ASTRA). Other EOS versions and other vendors
  are untested.
- **EAP-TLS only.** MAC Authentication Bypass (MAB) and EAP-TLS→MAB fallback
  are not supported. vEOS-lab 4.29.2F rejects `dot1x mac based
  authentication`, so they can only be validated on suitable physical Arista
  hardware.
- **Two segments per switch** in the console form (corp, quarantine). The API
  accepts more.
- **FreeRADIUS 3.2.5** (Ubuntu 24.04) only. 3.0.26, the earlier lab version,
  is not supported. **Single host**: PostgreSQL, nacd and FreeRADIUS on one
  node, with no high availability.

## Decision service outage (no authorization cache)

- While nacd is unreachable, FreeRADIUS rejects every new login and
  re-authentication ("NAC decision service unavailable"). No authorization is
  cached anywhere, and FreeRADIUS TLS session resumption is disabled.
- Sessions already authorized stay up on the switch until they
  re-authenticate. That is switch behavior, not a NAC decision. A switch
  re-authentication timer therefore bounds how long existing sessions survive
  an outage. The lab configuration has none, and the generated configuration
  shows one only as an optional, unvalidated comment.
- While nacd is down, nothing is recorded in NAC. FreeRADIUS logs the rejects,
  and nacd imports them after recovery.

## Sessions without accounting or eAPI (the validated lab setup)

- **Session end.** NAC ends a session only on a RADIUS accounting Stop, or
  when a different certificate authenticates on the same port and MAC. The lab
  switch sends no accounting. A session therefore stays "active" after the
  endpoint unplugs, until the next authentication on that port and MAC.
- **Liveness.** Without a fresh eAPI observation, the session page says
  liveness is **unknown**: "active" means the last authentication was
  accepted, not that the endpoint is connected now.
- **Switch-observed state** comes only from eAPI (`show dot1x hosts | json`).
  The lab's vEOS management is disconnected, so observation shows
  "unavailable". **Verify** is then "decision only".
- **CoA retry guard.** Before every retransmission, nacd checks that the
  captured session is still current in NAC: not ended, same start time, same
  port. If not, nacd abandons the CoA and never re-targets it. Without
  accounting, NAC cannot know the endpoint left, so the real guard is the
  switch. The CoA names the switch (`NAS-IP-Address`), the port (`NAS-Port-Id`)
  and the MAC (`Calling-Station-Id`, exactly as the switch sent it), so it
  cannot affect a different endpoint. If that MAC is no longer on that port,
  the switch answers NAK 503 "session context not found". The 503 behavior on
  vEOS is expected from RFC 5176 but has not been captured in the lab.
- **Acknowledged is not enforced.** `acked` means only that the switch accepted
  the CoA. `observed` means its re-authentication received the requested VLAN.
  Neither proves reachability: ASTRA measured 17–22 s from switch SUCCESS to
  TCP reachability in the virtual lab.

- **Quarantine is per session.** An operator override belongs to one session:
  switch + port + MAC + certificate (issuer and serial). A *rejected*
  certificate never replaces that session, so a failed attempt cannot clear a
  quarantine. A *different valid* certificate on the same port and MAC is a
  different authenticated identity and starts a new session, which the active
  policy decides without the override. To keep a device quarantined
  regardless of certificate, remove its identity bindings or publish a
  policy for it.

## Endpoint addressing after a VLAN change

After a CoA or a re-authentication moves an endpoint between VLANs, the
endpoint needs an address in the new subnet. In the lab the operator renewed
it by hand. NAC does not force DHCP renewal: a port-bounce CoA is not
validated on vEOS 4.29.2F.

## Security and operations

- The FreeRADIUS service token is visible in FreeRADIUS **debug** output
  (`freeradius -X` prints expanded configuration). Do not share debug logs.
- The console certificate is self-signed unless one is supplied.
- Switch RADIUS secrets are encrypted in the database with `NAC_SECRETS_KEY`.
  FreeRADIUS needs them in clear in `/var/lib/nac/freeradius-clients.conf`
  (mode 0640, `nac:freerad`).
- Registering, changing or deleting a switch restarts FreeRADIUS (about a
  second). In-progress EAP exchanges retry.
- Posture has no integration in the pilot: it is set through the device
  record (API/console), is considered stale after `NAC_POSTURE_MAX_AGE`
  (24 h), and is never taken from RADIUS.
- **Not tested here:** the systemd units, because the build environment has
  no systemd. Everything else in the installer was run on Ubuntu 24.04.5, and
  the services were started the way the units start them. CoA against a real
  switch was tested only against an emulator that reproduces the lab's NAK
  402 / ACK 200 behavior.
- **One operator account.** The pilot has no operator management or
  password change (console or API). Only the bootstrapped administrator
  exists; its generated password is in `/root/nac-initial-admin.txt`.
