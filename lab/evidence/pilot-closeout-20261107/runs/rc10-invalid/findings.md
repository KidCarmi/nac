# rc10 invalid EAP-TLS certificate — PASS

Exact candidate `b39649f138e126ed6689a53b590855061042e341`, `nacd`
SHA-256 `35699eac162cc2a7315efced101bee3ec18ed87ab1cb108f06f9bb1a7b48195e`.
At 08:50:52Z the Linux endpoint stopped its valid supplicant and started the
disposable client certificate signed by the untrusted test CA; the switch
session was cleared for a fresh authentication. The RADIUS capture records
Access-Reject at 08:51:00.488Z and another retry at 08:51:33.487Z. EOS showed
Et1 EAPOL FAILED, fallback NONE and no assigned VLAN. The endpoint still held
its previous 10.70.10.80/24 DHCP lease, but fresh TCP to corp and quarantine
both failed. This shows the port blocked traffic despite stale addressing.

The valid supplicant was explicitly restarted at 08:52:05Z, followed by a
switch session clear; RADIUS then accepted VLAN 10 and fresh corp TCP opened.
This recovery is an **operator test action**, not automatic invalid-certificate
fallback. [Packets](packet-timeline.json), [continuous probes](endpoint-probe.jsonl),
and bounded pcaps document both phases.
