# Sanitized fresh-instance configuration

The actual disposable CA/private keys, endpoint certificate/key, RADIUS shared secret, eAPI password, admin password and bearer tokens stay root-only inside the isolated lab and are omitted here.

| Component | Relevant setting |
| --- | --- |
| EOS Ethernet1 | `switchport access vlan 10`; `dot1x pae authenticator`; `dot1x port-control auto`; RADIUS source/NAS `10.70.99.1`, auth+accounting target `10.70.99.5`, CoA UDP 3799. VLAN10 corp, VLAN20 quarantine. |
| NAC switch record | `nac-veos-rc1`, Ethernet1, corp→10, quarantine→20, eAPI `https://10.70.99.1`, user `nac-observer`; stored credentials omitted. |
| NAC TLS observation | `SSL_CERT_FILE=/etc/nac/eapi-ca.pem`; `NAC_EAPI_INSECURE_SKIP_VERIFY=false`; trusted eAPI CA SHA-256 `38b2fa8012bd757fba31785038b9697e3ed585143d3eb96ab4ac71f1435718c4`. Console probe answered EOS 4.29.2F. |
| FreeRADIUS EAP | `ca_file=${confdir}/nac/certs/ca.pem`; `certificate_file=${confdir}/nac/certs/server.pem`; `private_key_file=${confdir}/nac/certs/server.key`. The installed CA fingerprint is `A7:C0:B7:04:40:43:1F:EB:39:2F:C0:6F:5E:87:D2:F4:03:E8:44:AB:39:79:8A:48:9A:41:D6:78:A8:C9:9C:81`, matching the endpoint's trust profile. |
| Endpoint | `ens3` only, EAP-TLS profile with server name `radius.nac.lab` and the matching disposable CA, ordinary DHCP via systemd-networkd, no management bypass. The authenticated certificate issuer+serial is bound in NAC; a client-supplied MAC is not the device identity. |
| Corp service and DHCP | `10.70.10.10:8080` HTTP 200, DHCP `10.70.10.50–99`, lease 120 s, router/DNS `10.70.10.10`, domain `nac-lab.test`. |
| Quarantine service and DHCP | `10.70.20.10:8080` HTTP 200, DHCP `10.70.20.50–99`, lease 120 s, router/DNS `10.70.20.10`, domain `nac-lab.test`. |
| Isolation | Separate EVE data bridges; EVE IP forwarding `0`; no cross-segment TCP. |

Policy version 1 was default deny with no rules. Version 2 added enabled priority-10 rule `bound-corp`: `identity.bound eq true` → allow segment `corp`, default deny. Quarantine was a per-session operator override and Restore removed it; neither changed the policy rule.
