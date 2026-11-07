# One-hour Windows DHCP lab configuration, sanitized

The isolated EVE-NG topology is [NAC-Pilot-DHCP.unl](NAC-Pilot-DHCP.unl) on Proxmox VM 102. Its SHA-256 is `8b69cf973f3cc545082aac0334aaf0aaa2e9c7b6563a3f66d5d4ec86c62ea935`. EVE IPv4 forwarding was 0. The Proxmox bridges and unrelated VMs were unchanged. This is the 3,600-second lease extension of the earlier 120-second DHCP run; the static-address baseline remains separate.

| Component | Effective setting |
|---|---|
| Switch | Arista vEOS-lab 4.29.2F, Ethernet1 802.1X auto with dynamic VLAN authorization; Ethernet2 corp, Ethernet3 quarantine, Ethernet4 control 10.70.99.1/24 |
| Corp | VLAN 10, subnet 10.70.10.0/24; DHCP and HTTP/TCP service at 10.70.10.10:8080 |
| Quarantine | VLAN 20, subnet 10.70.20.0/24; DHCP and HTTP/TCP service at 10.70.20.10:8080 |
| DHCP | BusyBox udhcpd in each segment, `opt lease 3600`; DNS and default route set to that segment's .10; no explicit renewal or rebinding options (58/59) |
| NAC control | Isolated guest 10.70.99.4/24, pinned nacd binary and FreeRADIUS 3.2.5, PostgreSQL, trusted eAPI TLS observer, RADIUS accounting |
| Windows endpoint | Windows Server 2025 Standard Evaluation Desktop Experience build 26100, one e1000 data NIC, MAC 00:50:00:00:02:00, DHCP, wired machine EAP-TLS; no management bypass NIC during the run |

Product switch registration mapped `corp→10`, `quarantine→20`; NAS and RADIUS source were 10.70.99.1. The switch sent RADIUS to 10.70.99.4 and accepted CoA on port 3799. The disposable CA validated the EAP-TLS client. NAC bound the validated certificate issuer and serial to the endpoint record; the policy required `identity.bound=true` and `mac.matches_device=true`. The client-reported MAC alone did not establish device identity. The active rule was `bound-eap-corp`, priority 100, allow corp, policy version 4. Quarantine and Restore were operator overrides and did not change the rule.

Trusted EOS eAPI TLS used the disposable lab CA and a switch certificate with IP SAN 10.70.99.1. The observer used `SSL_CERT_FILE=/etc/nac/eapi-ca.pem` and `NAC_EAPI_INSECURE_SKIP_VERIFY=false`. The CA public certificate SHA-256 was `38b2fa8012bd757fba31785038b9697e3ed585143d3eb96ab4ac71f1435718c4`. EOS running configuration was saved to startup and verified across reboot before this run.

No test key, private CA key, RADIUS shared secret, service token, admin password, or vendor image is included in this package.
