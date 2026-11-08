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
`ip`, `arping` (`iputils-arping`; `ping` is a fallback), `timeout`
(coreutils) and `networkctl`.

## Behavior (revision 2, after the owner-side static review)

Pinned for the lab run:

| File | SHA-256 |
|---|---|
| `nac-dhcp-recover` | `424c347ed6ecdc64d9ee3dad7dcc12e12b625fd29211a378ec1c4b068a7521bd` |
| `nac-dhcp-recover@.service` | `3a30728ce8c10f49e4328facf3157a88929358b991a6a41231f6461b19dbb84b` |

The NAC candidate stays `pilot-rc10` (`b39649f`, nacd `35699eac…`).

The hook runs one probe cycle about every second.

### Preconditions

All of these must hold, or the hook takes no action:
1. `wpa_cli status` reports `suppPortStatus=Authorized`.
2. systemd-networkd holds a **DHCPv4 lease file** for the interface:
   `/run/systemd/netif/leases/<ifindex>`.
3. The interface's default route was installed by DHCP (`proto dhcp`).

Static addressing, a static default route, or no default route: no action.
The hook makes **no claim** about what the DHCP client does in those cases.
Use it only on an endpoint whose interface is explicitly configured for
DHCP in systemd-networkd.

### Trigger

The gateway must fail **every** probe for at least
`NAC_HOOK_UNREACHABLE_SECONDS` (default 5) of **elapsed monotonic time**,
measured from `/proc/uptime` from the first failed probe. Logs report both
the elapsed time and the number of failed probes.

The probe is ARP (`arping -c 1 -w 1`), so it does not depend on ICMP. If
`arping` is not installed, it falls back to ICMP `ping` and logs
`probe=icmp`. With ICMP probing, a gateway that blocks ICMP looks
unreachable, so install `arping` (`iputils-arping`) for the test.

### Schedule within one outage episode

An episode runs from the first failed probe until the gateway answers
`NAC_HOOK_RECOVER_PROBES` (default 3) probes **in a row**. One stray reply
does not end it.

- **Action 1:** when the trigger is first met.
- **Action 2:** no sooner than `NAC_HOOK_MIN_INTERVAL` (default 30 s) after
  action 1's commands finished.
- **Each later action:** no sooner than twice the previous wait, capped at
  `NAC_HOOK_MAX_INTERVAL` (default 300 s). With the defaults, the waits are
  30 s, then 60 s.
- **Cap:** at most `NAC_HOOK_MAX_ACTIONS` (default 3) actions per episode.
  After that the hook only logs `cap_reached` until the episode ends.
- **Episode end** (gateway answers, port not authorized, or no DHCP lease or
  route) resets the counters and the schedule.

### Action and failures

The action is `NAC_HOOK_ACTION`:
- `linkcycle` (default): `networkctl down`, 1 s, `networkctl up`. This is
  the measured diagnostic.
- `reconfigure`: `networkctl reconfigure`. This is not measured.

Each command runs under `timeout` (`NAC_HOOK_CMD_TIMEOUT`, default 10 s).
`wpa_cli` and the probes also have deadlines.

A failing or timed-out command is logged as `recovery_failed step=… rc=…`
and counts as an action. If `down` fails, the hook still attempts `up`.
`action_done` means only that the commands succeeded. **DHCP recovery is
confirmed only by the gateway answering** (`episode_end
reason=gateway_answers`), and by the lab's own probes.

### Logs

Every decision goes to the journal, tagged `nac-dhcp-recover`:
- `started`
- `episode_start`
- `action n=…/…` and `action_done`
- `recovery_failed`
- `cap_reached`
- `episode_end reason=…`

### Checked here, with stubbed tools only

These checks used stubbed `wpa_cli`, `ip`, `arping` and `networkctl`.
- **Precondition guards:** no action while unauthorized, or with a route but
  no lease file.
- **Trigger timing:** action 1 at 2.0 s of elapsed time, with a 2 s
  threshold.
- **Schedule and cap:** action 2 at least 3 s and action 3 at least 6 s
  after the previous action, with 3 s and 6 s test settings. The cap of 3
  held.
- **Episode handling:** the episode ended only after 3 good probes in a row,
  and a new episode started cleanly afterwards.
- **Failure handling:** a failing `networkctl up` was logged as
  `recovery_failed` and not as done.

It has **not** run against a real supplicant or switch.

### Known side effects (to measure, not assume)

- **`linkcycle` takes the interface down.** If the switch sees carrier loss,
  it ends the 802.1X session (accounting Stop) and runs a fresh
  authentication. The NAC quarantine override is identity-scoped
  (ADR-0006), so a quarantined endpoint must stay quarantined
  **throughout**. Verify this with continuous probes, not only the final
  VLAN.
- **Same-VLAN gaps.** On vEOS-lab an accepted VLAN-bearing CoA interrupts
  forwarding for up to about 58 s. The hook cannot tell that gap from a VLAN
  move, so it may act during a same-VLAN Re-authenticate. How many actions
  it takes is an outcome to measure. The loop does not guarantee one.
- **Gateway outage.** A real gateway failure triggers up to
  `NAC_HOOK_MAX_ACTIONS` link cycles per episode. That is bounded, but each
  one causes accounting churn.

## Lab test plan (ASTRA)

**Fixed:**
- candidate `pilot-rc10` (`b39649f`, nacd `35699eac…`)
- vEOS-lab 4.29.2F, FreeRADIUS 3.2.5
- the one-NIC Ubuntu 22.04.5 systemd-networkd EAP-TLS endpoint used before

**Only change:** hook revision 2 (the pinned hashes above), installed
with `arping` (`iputils-arping`) and enabled with its defaults:
`linkcycle`, ARP probe, 5 s elapsed, waits of 30 s then 60 s, and a cap of 3
actions per episode. Record the hook's journal lines.

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

### Negative controls (added after the owner-side review)

Each control is a separate, uninterrupted run. Count every hook action and
record the log line for each.

| # | Control | Expected hook behavior |
|---|---|---|
| N1 | Gateway ICMP blocked; ARP and application traffic healthy | No action with `probe=arp`. Also run once without `arping` (`probe=icmp`), where up to the cap is expected |
| N2 | Gateway truly down long enough to reach the cap, then restored | Exactly `NAC_HOOK_MAX_ACTIONS` actions on the documented schedule, then `cap_reached`, then `episode_end` after restoration |
| N3 | Gateway answering intermittently | No episode end on single replies, no extra actions beyond the schedule |
| N4 | Static address and static default route | No action |
| N5 | No default route | No action |
| N6 | Real `wpa_cli status` before, during and after a cycle | Recorded |
| N7 | Recovery command made to fail (for example `networkctl` unavailable) | `recovery_failed`, never reported as recovered |
| N8 | Quarantine during a hook-induced session replacement | Continuous probes show the corp service denied throughout, VLAN 20 and the override present |
| N9 | Same-VLAN Re-authenticate, with and without the hook | Outage and every action counted; compare against +21–58 s without the hook |

### Timing and thresholds

- Measure user-visible timing from the **product action**. Report CoA
  request, ACK and Access-Accept times separately.
- The owner-side review recommends **≤ 60 s new-segment recovery and ≤ 5 s
  old-segment denial** as *provisional lab acceptance thresholds for the
  explicitly tested combination*, not a universal SLA. The owner decides.

### Scope

Out of scope:
- Windows, which recovers on its own
- unmanaged endpoints
- voice and shared ports
- physical switches and physical links

A pass in VLAN-transition cases alone does not qualify the hook as an
always-on client remedy. It stays opt-in and unqualified until the negative
controls also pass.
