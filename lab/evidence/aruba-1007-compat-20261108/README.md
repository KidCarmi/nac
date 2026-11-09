# Aruba CX 10.07 compatibility experiment — 2026-11-08

> **Superseded conclusion (2026-11-09):** The [bounded reference run](../aruba-1007-reference-20261109/README.md) identified the missing global 802.1X authenticator enablement, formed an EAP-TLS client session, and proved that this exact simulator enforces the tested per-session IPv4 role policy. The failed MAB observation and working static ACL below remain historical observations; the inference that all per-session role enforcement is unavailable was wrong. The documented classifier-policy limitation does not establish that role-associated port-access policy is nonfunctional.

**Can this image enforce a static access-port ACL? Yes.** With the same endpoint address (`10.70.10.50/24`), the corp TCP probe changed from HTTP 200 to timeout while remediation stayed HTTP 200; removing the ACL restored HTTP 200 to both. [Before](baseline-endpoint.txt), [ACL configuration](acl-applied-switch.txt), [during](acl-applied-endpoint.txt), [removal](acl-removed-switch.txt), [after](acl-removed-endpoint.txt).

**Can this image enforce a per-session quarantine role without moving the endpoint VLAN? Yes, for the IPv4 flows tested on 2026-11-09.** The experiment recorded below did not form a client session and is retained for diagnosis. The [follow-up run](../aruba-1007-reference-20261109/README.md) completed EAP-TLS and demonstrated a role-controlled corp denial and remediation permit with the endpoint address unchanged. NAC integration and CoA remain untested in that reference run.

## Inventory and scope

| Item | Exact value |
| --- | --- |
| EVE VM | Proxmox VM 102, existing EVE-NG installation |
| EVE | `eve-ng 6.2.0-4`, kernel `6.7.5-eveng-6-ksm+` |
| EVE resources after user RAM increase | 19 GiB guest RAM; 4 GiB swap; 77 GiB root filesystem, 11 GiB available at inspection; `/dev/kvm` readable and writable |
| Switch | `ArubaOS-CX Virtual.10.07.0004`; Build SHA `64b88cbfc38a48a03d7b8b6229137e37f59c9310` |
| Switch image payload SHA-256 | `e60ef7d9679f664d1dafcaecb96f7443ec64e5b2abca26c41fdec26c57fafc25` (`virtioa.qcow2`), **not included** |
| Source archive SHA-256 | `a6bfdc0a47a52a27a982998d98970d73b38c0a2c1a2c54e6b89abc2d847c60f7`, **not included** |
| Image provenance | Third-party mirror linked from the [user-provided GitHub README](https://github.com/hegdepavankumar/Cisco-Images-for-GNS3-and-EVE-NG/blob/main/README.md). HPE signature or authorized entitlement was not verified. Treat this as a compatibility experiment, not an authorized vendor image qualification. |
| FreeRADIUS | `3.0.26` in isolated control node |
| NAC | Not connected to this experiment; preserved rc10 NAC qualification remains [separate](../final-rc10-qualification-20261107.md) |
| Topology | [EVE export](Aruba-Role-ACL-1007.unl), SHA-256 `004c7fbf948fa3e9fe06367c94a889c88a73afb56c0b65719006e10f3970ae94` |

The new lab is `/opt/unetlab/labs/Aruba-Role-ACL-1007.unl` in EVE. It has isolated EVE bridges for access, corp, remediation, control, and a same-subnet peer. The endpoint has one data NIC and no management bypass. Existing Proxmox bridges, host settings, unrelated VMs, and prior NAC lab topologies were not altered. The earlier lab nodes were stopped for the VM 102 memory activation and remain stopped.

## Result matrix

| Test | Expected | Observation | Status |
| --- | --- | --- | --- |
| Both TCP services healthy | HTTP 200 from each service | Corp and remediation each answered locally; after correcting virtual NIC offload, the endpoint obtained HTTP 200 from both; same-subnet peer reached corp HTTP 200. | **PASS** |
| Unrestricted baseline | Corp and remediation available | Same `10.70.10.50/24` endpoint: `corp=200`, `remediation=200`. | **PASS** |
| Static ingress ACL on `1/1/1` | Corp denied, remediation permitted | `corp=000` after 2 s timeout, `remediation=200`; endpoint IP and default route unchanged. | **PASS** |
| Remove ACL | Both services available again | `corp=200`, `remediation=200`; no endpoint address change. | **PASS** |
| MAC-auth session and RADIUS role | Switch sends Access-Request, receives fixed role, creates session | MAC is learned on VLAN 10/port `1/1/1`, but `show port-access clients` reports none, 20 s control capture received zero UDP/1812 packets, and corp remains HTTP 200. No role enforcement can be inferred. | **BLOCKED** — trigger/configuration or simulator behavior unresolved |
| Per-session role policy, as attempted on 2026-11-08 | Quarantine role permits remediation and denies corp | CLI accepted policy, but no authenticated client session formed, so this attempt did not test enforcement. The [2026-11-09 reference run](../aruba-1007-reference-20261109/README.md) subsequently passed the tested IPv4 role policy. | **BLOCKED** — original configuration gap, superseded |
| NAC rule or CoA changing role | Same IP, changed reachability caused by NAC | Not run in this original experiment because no authenticated session formed. The later fixed FreeRADIUS reference run did not test NAC or CoA. | **BLOCKED** |

## Diagnosis

Initial TCP probes timed out even with no ACL. A packet trace showed the corp guest receiving a SYN with an invalid checksum; after disabling `tx`, `tso`, and `gso` offload on the Linux endpoint and service NICs, both services returned HTTP 200. This is a virtual NIC dataplane workaround. It must be applied after guest restarts before using a TCP timeout as isolation evidence.

The fixed MAC-auth policy was limited to one test MAC (`00:50:00:00:3e:00`) and returned local role `quarantine`. MAC identity alone is spoofable and was used only for platform capability testing; it is not accepted as NAC device identity. The switch had the `mgmt` RADIUS server in its built-in `radius` group, the control node could ping the switch at `10.70.99.1`, and FreeRADIUS was active. Port reset, endpoint link reset, ARP refresh, and fresh TCP traffic still produced no RADIUS request or client session. Only the server-side RADIUS capture was taken in this trial, so it does not establish whether the switch transmitted a request. These observations do not isolate a product defect in FreeRADIUS or NAC. The later EAP-TLS run proves the tested per-session role policy can enforce without an endpoint VLAN move.

## Reproduce

1. Import the [topology](Aruba-Role-ACL-1007.unl) into EVE and place a separately authorized Aruba simulator image under `/opt/unetlab/addons/qemu/arubacx-10.07/virtioa.qcow2`. Match the hash above if repeating the exact-image experiment. The vendor image and disposable credentials are deliberately excluded.
2. Start nodes 61–66. Get their current console ports with `aruba_node_guest.py` using `ARUBA_NODE_ID=<id> ARUBA_ACTION=status`. Console ports can change after restart.
3. In the endpoint and each Linux service guest run `ethtool -K ens3 tx off tso off gso off`. Assign the endpoint `10.70.10.50/24` with default route `10.70.10.1`, corp service `10.70.10.10/24`, remediation service `10.70.20.10/24` with default routes through the respective Aruba SVIs. Use `10.70.10.11/24` for the separate same-subnet peer. This run used **static** endpoint addressing; it does not qualify DHCP on Aruba.
4. Verify fresh TCP connections to `http://10.70.10.10:8080/` and `http://10.70.20.10:8080/` before an enforcement test.
5. On Aruba, configure `access-list ip deny-corp` with `10 deny tcp any 10.70.10.10/32 eq 8080` and `20 permit any any any`. On `interface 1/1/1`, apply `apply access-list ip deny-corp in`. Probe both services from the endpoint. Remove with `no apply access-list ip deny-corp in` and probe again. The ACL is currently **removed** in the saved switch configuration.
6. The role experiment's sanitized commands are: `class ip allow-rem` → `10 match ip any 10.70.20.10/32`; `class ip deny-all` → `10 match ip any any`; `port-access policy q-policy` → `10 class ip allow-rem`, `100 class ip deny-all action drop`; `port-access role quarantine` → `vlan access 10`, `associate policy q-policy`. Inspect `show port-access policy q-policy` and `show port-access clients`, then decide enforcement only from endpoint TCP probes.

For a broader same-IP quarantine qualification, continue on this isolated simulator with CoA, NAC integration, IPv6, established flows, and a shared phone/PC port. Use authorized physical AOS-CX hardware for final hardware behavior and any capability the simulator cannot demonstrate. Verify packet enforcement and CoA behavior with endpoint probes; a role name or CoA-ACK alone is insufficient.
