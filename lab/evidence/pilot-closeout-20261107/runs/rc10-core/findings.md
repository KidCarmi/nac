# rc10 real vEOS / Linux DHCP qualification

Exact product commit `b39649f138e126ed6689a53b590855061042e341`, installed
`nacd` SHA-256 `35699eac162cc2a7315efced101bee3ec18ed87ab1cb108f06f9bb1a7b48195e`,
unchanged FreeRADIUS adapter SHA-256
`602c3fc5ff195c818bca6f5a15ca665b588077a07025315362cbb5331826c01b`.
FreeRADIUS 3.2.5, vEOS-lab EOS 4.29.2F. This run used the separate clean rc10
Ubuntu overlay, one Linux wired supplicant NIC on Ethernet1, DHCP, and an
isolated EVE data topology. EVE IP forwarding was 0. Each service returned
HTTP 200 independently from its own segment before testing (see `runner.log`).

| Transition | Product CoA time (UTC) | Old TCP denied | New TCP open | Result |
|---|---|---:|---:|---|
| Operator Quarantine, corp→quarantine | 08:38:16.664 | 1.964 s | 24.397 s | PASS |
| Operator Restore, quarantine→corp | 08:39:50.372 | 1.386 s | 110.933 s | PASS |
| Rule-only v3 corp→quarantine | 08:42:30.539 | 1.065 s | 72.114 s | PASS |
| Rule-only v4 quarantine→corp | 08:44:21.668 | 1.601 s | 79.464 s | PASS |

These times apply only to this Linux run, from the action's recorded time to
the first continuous **fresh** TCP probe result. The observation timeout was
600 s. No manual DHCP renewal, endpoint reconnect or switch-port bounce was
used during these four transitions. The initial rc10 corp decision at
08:37:29 matched `bound-eap-corp` policy v2, identity bound from the prior
validated EAP-TLS issuer and serial, and returned `Class=nac:58629776…` and
VLAN 10. The switch showed EAPOL SUCCESS and the endpoint opened corp TCP.

The first operator CoA received ACK, forced switch EAPOL reauthentication and
returned `Class=nac:3a325bcd…`/VLAN 20 with a quarantine override. Restore
returned VLAN 10 without the override. Changing **only** the draft rule's
`segment` from corp to quarantine, then previewing/publishing v3 and using the
product Re-authenticate action, returned `Class=nac:deae1ee7…`/VLAN 20 with
`matched_rule=bound-eap-corp`, policy v3 and no override. The reverse rule-only
v4 returned VLAN 10. [Packet timeline](packet-timeline.json) contains the
switch-originated RADIUS requests, CoA Request/ACK IDs, Tunnel-Type 13,
Tunnel-Medium-Type 6, VLAN 10/20 and DHCP. [Probe summary](probe-summary.json)
contains the separate denial/reachability timings and address/route/DNS
changes. Raw bounded [RADIUS](radius.pcap0), [EAPOL/endpoint](endpoint.pcap0),
[corp DHCP](corp-dhcp.pcap0), and [quarantine DHCP](quarantine-dhcp.pcap0)
captures are retained.

The DHCP servers offered 120 s leases and did not include explicit T1/T2
options. The endpoint sent an old-server unicast renewal around 60 s and a
broadcast rebinding request around 105 s; when that failed, it discovered the
new VLAN's server about 15 s later. This validates the T1≈60 s and T2≈105 s
default-timer inference **for this Linux client**. The measured recovery varied
with remaining lease time when each CoA arrived. In rc10's API,
`endpoint_reachability.state=not_observed` remained separate from a fresh eAPI
`SUCCESS VLAN` observation and `connected` switch liveness. The endpoint TCP
probe, not the eAPI observation, establishes application access.

Trusted eAPI TLS was also retested on this exact rc10 binary. With the
disposable CA installed as `SSL_CERT_FILE=/etc/nac/eapi-ca.pem` and
`NAC_EAPI_INSECURE_SKIP_VERIFY=false`, `Probe` and `Observe` reached EOS over
TLS 1.2/ECDHE. Replacing only the switch certificate with one signed by the
same CA but valid for IP 10.70.99.9 made the rc10 probe report `reachable=no`
and an explicit x509 IP mismatch for 10.70.99.1. Restoring the good certificate
returned `reachable=yes`; see [failed probe](eapi-mismatch-probe.txt) and
[restored probe](eapi-restored-probe.txt). The switch startup configuration and
private keys were not exported.
