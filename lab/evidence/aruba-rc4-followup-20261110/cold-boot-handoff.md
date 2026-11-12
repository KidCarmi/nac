# Aruba rc4 cold-boot handoff

**Frozen candidate:** `pilot-aruba-rc4` commit `0148bf88985c5da9086b54aafd002fbfbfe71b75`; installed `nacd` SHA-256 `dd2b61371585d317bf07684733996c69e265dfdb9e762e354eeca4f7821b49d7`; adapter aggregate SHA-256 `3c45063efb451ff86dac23d695e9c57c307b86ef8054b790a759cbb63e80db32`. The switch was ArubaOS-CX Virtual 10.07.0004 in the isolated EVE topology on VM 102.

## Result and exact manual workaround

**Automatic recovery: FAIL in the virtual lab.** After the cold restart, the endpoint sent EAPOL Starts for 145 seconds. The switch sent no EAP response, no RADIUS exchange occurred, `show port-access clients interface 1/1/1 detail` found no client, and fresh protected TCP timed out. This observation ended **before** operator repair. The later successful login is a separate diagnostic result and does not change the FAIL classification.

The successful operator action was entered on the **AOS-CX switch console** at 2026-11-10 11:57:55 UTC:

```text
configure terminal
interface 1/1/1
aaa authentication port-access dot1x authenticator
disable
enable
end
show running-config interface 1/1/1
show port-access clients interface 1/1/1 detail
```

`disable` and `enable` are commands in the interface's `dot1x authenticator` submode. `show running-config interface 1/1/1` then showed `aaa authentication port-access dot1x authenticator` with `enable`. The preceding attempt to use `no enable` returned `Invalid input: enable`; it did **not** perform the reset. The transcript of the successful sequence and the failed syntax attempt are retained locally as `manual-authenticator-disable-enable.txt` and `manual-authenticator-reset.txt` under `D:/AI/nac-wired-lab/qualification/runs/aruba-rc4-coldrestart-20261110/`.

**Scope:** this toggles the authenticator on switch port `1/1/1`; it does not toggle the global authenticator or shut down the physical interface. Every 802.1X client using that port can lose or repeat authorization during the toggle. Treat it as disruptive on a shared phone/PC port; shared-port behavior has not been tested here. This is an operator console workaround, not a NAC product action or an automatic recovery mechanism.

## Prerequisites and verification

1. Use only the isolated `Aruba-Role-ACL-1007.unl` topology. Verify its SHA-256 is `004c7fbf948fa3e9fe06367c94a889c88a73afb56c0b65719006e10f3970ae94` and that node 61 is the Aruba switch and node 62 is the one-data-NIC wired endpoint. Do not apply the workaround to an unrelated switch or port.
2. Verify the saved **global and interface** 802.1X authenticator configuration, RADIUS target, local `corp`/`quarantine` roles and service health. The [sanitized switch configuration](../aruba-rc4-clean-20261110/sanitized-switch-config.txt) records the relevant lines. NAC, FreeRADIUS and PostgreSQL must be running and the endpoint's wired supplicant must be sending EAPOL Starts. Collect endpoint/access/control captures and fresh TCP probes before intervening.
3. Allow the full **145-second no-repair observation** first. Check endpoint EAPOL, switch client state, switch-originated UDP/1812 and protected TCP. If the same no-response condition appears, record it as failed automatic recovery before applying the manual commands above.
4. After the toggle, require an EAPOL response and a new switch-originated RADIUS request/valid EAP-TLS result; check `show port-access clients interface 1/1/1 detail` for the authenticated client and `corp` role. Check the endpoint's DHCP address, route and DNS, verify the corp TCP service independently, then require a **fresh endpoint TCP connection** to return HTTP 200. A configured `enable`, RADIUS Access-Accept or displayed role alone is insufficient.
5. If authentication succeeds but TCP still fails, diagnose DHCP, checksum offload and remediation return routing separately. These needed manual repair in the earlier rc4 restart; six guest boot units later persisted those prerequisites. Do not call a postrepair pass automatic recovery. The [clean rc4 report](../aruba-rc4-clean-20261110/README.md) separates those earlier interventions.

The bounded local cold/negative capture archive has SHA-256 `415c09e156f2e1e4394ef7c96449c3dc150e343e820603495ce14f5686af599f`. Its raw packet data and console transcript were retained locally, outside the published package. The [follow-up result matrix](README.md) records the postrepair EAP-TLS admission and protected HTTP 200. The [100-cycle campaign](timing-summary.json) was run **after** this repair and does not retest automatic cold boot.

## Remaining coverage and prerequisites

| Case | Status for pinned rc4 | Exact gap / existing evidence |
|---|---|
| Windows 10/11 wired EAP-TLS and DHCP | **BLOCKED** | No authorized Windows 10/11 endpoint with a single data NIC, valid client certificate and ordinary wired 802.1X/DHCP profile was available. A historical Windows Server 2025 run does not qualify Windows 10/11. |
| Physical Aruba or Arista access switch | **BLOCKED** | No authorized physical switch connected to the isolated access, service and control networks. Virtual cold-boot behavior cannot establish the hardware outcome. |
| Prompt Arista DHCP renewal after VLAN 10↔20 | **FAIL on the tested Linux client; other clients unverified** | [rc4 Arista console evidence](README.md#arista-linux-dhcp-and-ui-semantics) measured ~93 s and ~108 s until lease expiry, with no manual endpoint action. A supported, tested prompt renewal mechanism and Windows client behavior remain open. |
| Aruba shared phone/PC port | **BLOCKED** | No second authenticated device on port `1/1/1`; the reset and role CoA were tested with one endpoint. Need a physical or suitable virtual multi-client port and per-client reachability checks. |
| NAC-driven IPv6 authorization and reachability | **BLOCKED** | A [fixed-policy reference](../aruba-1007-ipv6-20261109/README.md) passed one fresh same-subnet IPv6 TCP denial/restore, but did not use NAC. IPv6 remediation, routed/peer-initiated flows, established connections and DHCPv6 remain untested. |
| NAC-driven UDP authorization and reachability | **BLOCKED** | A [fixed-policy reference](../aruba-udp-reference-20261109/README.md) passed endpoint-initiated IPv4 UDP echo denial/restore, but did not use NAC. Peer-initiated, IPv6, routed and already-open UDP behavior remain untested. |
| Peer-initiated containment | **UNVERIFIED** | The [reference packet test](../aruba-1007-peer-20261109/README.md) showed an IPv4 SYN reaching the endpoint despite incomplete two-way TCP; do not claim full bidirectional isolation without a NAC-driven packet test and a revised role policy. |

The isolated Aruba topology's five nodes (61–65) were stopped after the campaign. The local `aruba-topology-stopped.txt` records each node at state `0` after stop. No lab nodes were started for this handoff.
