# One-hour Linux DHCP lab configuration, sanitized

The isolated EVE-NG topology is [NAC-Pilot-DHCP.unl](NAC-Pilot-DHCP.unl) on Proxmox VM 102, SHA-256 `8b69cf973f3cc545082aac0334aaf0aaa2e9c7b6563a3f66d5d4ec86c62ea935`. EVE IPv4 forwarding is 0. Proxmox bridges and unrelated VMs were unchanged. This run uses 3,600-second leases and is separate from the proven 120-second baseline and the Windows one-hour run.

| Component | Effective setting |
|---|---|
| Switch | Arista vEOS-lab 4.29.2F, Ethernet1 802.1X auto and dynamic authorization; Ethernet2 corp VLAN10, Ethernet3 quarantine VLAN20, Ethernet4 control 10.70.99.1/24 |
| Corp | VLAN10, 10.70.10.0/24, DHCP/DNS and HTTP/TCP at 10.70.10.10:8080 |
| Quarantine | VLAN20, 10.70.20.0/24, DHCP/DNS and HTTP/TCP at 10.70.20.10:8080 |
| DHCP | BusyBox udhcpd scopes `opt lease 3600`; no explicit T1/T2 options 58/59 |
| NAC control | Isolated rc10 guest 10.70.99.4/24, nacd pinned to candidate b39649f and FreeRADIUS 3.2.5; RADIUS accounting and trusted EOS eAPI TLS |
| Linux endpoint | Ubuntu 22.04.5, exactly one data interface `ens3`, MAC 00:50:00:00:02:00; systemd-networkd DHCP, wpa_supplicant wired EAP-TLS; no management bypass NIC |

The Windows QEMU endpoint was shut down before Linux EVE node32 was started on Ethernet1. A residual Windows switch authorization briefly allowed Linux to acquire a corp DHCP lease before EAP-TLS; the switch session was explicitly cleared, then fresh corp and quarantine TCP were both blocked until Linux authenticated. The same validated EAP-TLS issuer/serial is bound to the NAC endpoint record. The active rule is `bound-eap-corp` allow corp under policy version 4; operator Quarantine/Restore overrides do not change it.

The switch RADIUS target and CoA source are on the isolated control network. Shared secrets, test keys, private CA material, admin credentials and vendor media remain in the lab and are excluded here.
