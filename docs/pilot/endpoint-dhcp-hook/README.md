# Endpoint DHCP recovery hook: reference and lab test plan

**Status: UNQUALIFIED.** This is endpoint configuration for *managed* Linux
endpoints. NAC does not install, configure or depend on it. It exists to test
remedy option 1 under "Recovery boundary" in
[known limitations](../known-limitations.md#recovery-boundary-dnsmasq-comparison-pilot-rc10)
against ASTRA's proposed acceptance target. That target is proposed, not
agreed.

## Why a hook

- **The cause.** After a CoA moves a session to another VLAN, the link stays
  up and the Linux DHCP client keeps its old lease:
  - the T1 renewal goes unicast to the old server, which is unreachable
  - a broadcast rebind may be ignored (BusyBox udhcpd) or lost (short-timer
    run, `e86242e`)
  - after a lost rebind, RFC 2131 backoff can postpone the next try by many
    minutes
- **What worked.** In both lab diagnostics, a manual `networkctl down/up`
  recovered in 6.2 s and 6.9 s, because DHCP restarts from INIT. In INIT the
  client sends a Discover and retransmits it within seconds, not minutes.
- **What the hook does.** It automates that action, but only when 802.1X is
  authorized, the endpoint has a lease, and its gateway has stopped
  answering.

## Files

| File | Install as |
|---|---|
| `nac-dhcp-recover` | `/usr/local/sbin/nac-dhcp-recover`, mode 0755 |
| `nac-dhcp-recover@.service` | `/etc/systemd/system/nac-dhcp-recover@.service` |

Enable it with `systemctl enable --now nac-dhcp-recover@<interface>`.

It needs `wpa_cli` able to reach the supplicant's control socket (default
`/run/wpa_supplicant`; set `NAC_HOOK_WPA_CTRL` if yours differs), plus
`ip`, `ping` and `networkctl`.

## Behavior

The hook runs once a second.

- **It acts only when all three hold:**
  1. `wpa_cli status` reports `suppPortStatus=Authorized`
  2. the interface has a default route
  3. that gateway has not answered a ping for `NAC_HOOK_UNREACHABLE_SECONDS`
     probes in a row (default 5)
- **The action** is `NAC_HOOK_ACTION`:
  - `linkcycle` (default): `networkctl down`, 1 s, `networkctl up`. This is
    the measured diagnostic.
  - `reconfigure`: `networkctl reconfigure`. This is not yet measured.
- **Backoff:** at least `NAC_HOOK_MIN_INTERVAL` (default 30 s) between
  actions, doubling up to `NAC_HOOK_MAX_INTERVAL` (default 300 s). It resets
  when the gateway answers again.
- **It never acts:**
  - while the port is unauthorized
  - without a default route. No lease means DHCP is already discovering.
- **Logs:** every decision goes to the journal, tagged `nac-dhcp-recover`.

Checked here with stubbed `wpa_cli`, `ip`, `ping` and `networkctl`:
- no action while unauthorized or without a route
- one link cycle after the threshold
- backoff before the next cycle
- quiet once the gateway answers

It has **not** run against a real supplicant or switch.

### Known side effects (to measure, not assume)

- **`linkcycle` takes the interface down.** If the switch sees carrier
  loss, it ends the 802.1X session (accounting Stop) and runs a fresh
  authentication. The NAC quarantine override is identity-scoped
  (ADR-0006), so a quarantined endpoint must come back quarantined. **Verify
  this.**
- **Same-VLAN gaps.** On vEOS-lab, an accepted VLAN-bearing CoA interrupts
  forwarding for up to about 58 s, even without a VLAN change. The hook
  cannot tell that gap from a VLAN move, so it will cycle once during a
  same-VLAN Re-authenticate. Measure whether that lengthens or shortens the
  outage.
- **Gateway outage.** If the gateway itself fails, the hook cycles with
  backoff. That is bounded, but it adds accounting churn.

## Lab test plan (ASTRA)

**Fixed:**
- candidate `pilot-rc10` (`b39649f`, nacd `35699eac…`)
- vEOS-lab 4.29.2F, FreeRADIUS 3.2.5
- the one-NIC Ubuntu 22.04.5 systemd-networkd EAP-TLS endpoint used before

**Only change:** the hook installed and enabled with its defaults
(`linkcycle`, 5 s, 30–300 s). Record the hook's journal lines.

**DHCP server:** run both, because the hook should not depend on the server:
1. the original BusyBox udhcpd, the worst case: it ignores wrong-subnet
   rebinds
2. authoritative dnsmasq 2.91

Use the 3600 s lease with default timers on both.

**Cases.** Each case is an uninterrupted primary run, with no manual
endpoint action:

| # | Case | Lease age |
|---|---|---|
| 1 | Quarantine (corp → quarantine) | early (< 5 min after acquisition) |
| 2 | Restore (quarantine → corp) | early |
| 3 | Quarantine | near T1 |
| 4 | Restore | near T1 |
| 5 | Same-VLAN Re-authenticate (corp) | any |
| 6 | Quarantine then Restore after a full restart (switch, control node, endpoint) | any |

**Record for each case:**
- the product action time
- CoA-ACK, Access-Accept and VLAN
- EAPOL timeline
- the hook decision and action times
- link and carrier state on the switch: `show interfaces Ethernet1`, plus logging
- accounting Stop/Start and NAC session replacement
- the DHCP exchange on both bridges
- endpoint address, route and DNS
- fresh TCP to both services
- old-segment denial time and new-segment reachability time

**Verdict per case:**
- **PASS:** it meets the proposed target: new segment within 60 s of the
  CoA, old segment denied within 5 s, no manual action.
- **And additionally:**
  - **no quarantine loss:** after a hook-induced re-authentication, a
    quarantined identity is still quarantined (VLAN 20, override present)
  - **no unbounded churn:** at most one hook action per transition, unless
    the gateway really stayed down
- **Case 5:** report the outage with the hook against the earlier
  same-VLAN measurements without it (+21–58 s).

**Out of scope:** Windows (it recovers on its own), unmanaged endpoints, and
physical switches.
