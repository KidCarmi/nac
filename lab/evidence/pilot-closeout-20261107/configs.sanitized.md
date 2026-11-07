# Isolated lab configuration, sanitized

Topology: [NAC-Pilot-DHCP.unl](NAC-Pilot-DHCP.unl), EVE-NG VM 102 on Proxmox.
All VLAN service and endpoint links are inside EVE; EVE `net.ipv4.ip_forward=0`.
Proxmox bridges and unrelated VMs were unchanged. The proven static-address
baseline is separate from this DHCP qualification.

| Role | Link and addressing | Port or service |
|---|---|---|
| vEOS-lab 4.29.2F | Ethernet1 wired endpoint; Ethernet2 corp; Ethernet3 quarantine; Ethernet4 control 10.70.99.1/24 | dot1x auto, dynamic authorization, accounting |
| Corp | VLAN 10, 10.70.10.0/24 | DHCP at 10.70.10.10, HTTP/TCP 10.70.10.10:8080 |
| Quarantine | VLAN 20, 10.70.20.0/24 | DHCP at 10.70.20.10, HTTP/TCP 10.70.20.10:8080 |
| Clean rc10 control guest | 10.70.99.4/24 | nacd, FreeRADIUS 3.2.5, PostgreSQL, eAPI observer |
| Linux preliminary endpoint | one data NIC, MAC 00:50:00:00:02:00, DHCP | `wpa_supplicant` EAP-TLS; no management NIC |
| Windows qualified endpoint | one e1000 data NIC, MAC 00:50:00:00:02:00, DHCP on the same Et1 | Windows Server 2025 Standard Evaluation Desktop Experience build 26100; setup NAT NIC and corrective floppy removed before primary run; machine EAP-TLS with trusted CA and RADIUS server name `radius.nac.lab` |

Product switch registration mapped `corp→10`, `quarantine→20`, RADIUS source
and NAS IP `10.70.99.1`, CoA port 3799, RADIUS target `10.70.99.4`.
The FreeRADIUS client secret, disposable CA private key, EAP-TLS client key,
observer password and admin credential remain only in the disposable lab.
FreeRADIUS validates the certificate. The NAC identity binding uses the
validated EAP-TLS issuer `/CN=NAC Lab Disposable CA` and serial
`2E1B47DC86A00B7F7250500B4A21F454B2EFD819`, bound to the endpoint record;
the policy requires `identity.bound=true` and `mac.matches_device=true`.
The client-supplied MAC alone does not establish identity.

The rc10 rule sequence used `bound-eap-corp` priority 100, action `allow`,
conditions unchanged. Published v2 targeted corp; published v3 changed only
`segment` to quarantine; published v4 changed only `segment` back to corp.
The operator Quarantine/Restore pair used an override under v2 and is
separate from the rule-only pair. A 120 s lease was offered in each subnet;
DHCP ACKs did not carry explicit T1/T2 options. The Linux client attempted
unicast renewal around 60 s and broadcast rebinding around 105 s.
The Windows client also renewed each 120 s lease around 60 s; it automatically
obtained the other subnet after each CoA without a manual renewal or link bounce.
T2=105 s is the [RFC 2131](https://datatracker.ietf.org/doc/html/rfc2131/) default for this lease and was not measured in the
Windows run.

Trusted EOS eAPI TLS used a disposable lab CA and a switch certificate with
IP SAN 10.70.99.1. EOS required `copy flash:nac-eapi.crt certificate:nac-eapi.crt`
and `copy flash:nac-eapi.key sslkey:nac-eapi.key`, then an SSL profile with
TLS 1.2 and cipher list
`ECDHE-RSA-AES256-GCM-SHA384:ECDHE-RSA-AES128-GCM-SHA256`.
The NAC guest set `SSL_CERT_FILE=/etc/nac/eapi-ca.pem` and
`NAC_EAPI_INSECURE_SKIP_VERIFY=false`; the public CA file SHA-256 was
`38b2fa8012bd757fba31785038b9697e3ed585143d3eb96ab4ac71f1435718c4`.
The same CA's certificate for IP 10.70.99.9 was rejected for the 10.70.99.1
eAPI URL. These EOS changes were on the disposable switch running config;
the saved startup config was not changed.
