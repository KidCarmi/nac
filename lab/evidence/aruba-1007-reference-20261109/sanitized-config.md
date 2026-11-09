# Sanitized reference configuration

Secrets, private keys and vendor image are excluded. The disposable client certificate is identified only by its validated EAP-TLS identity.

```text
EVE topology: Aruba-Role-ACL-1007.unl
Switch management: 10.70.99.1/24 on isolated vnet0_64
FreeRADIUS: 10.70.99.2/24, UDP/1812, isolated vnet0_64
Endpoint: ens3 10.70.10.50/24, gateway 10.70.10.1, isolated vnet0_61
Corp: 10.70.10.10:8080, VLAN 10
Same-subnet peer: 10.70.10.11:8080, VLAN 10
Remediation: 10.70.20.10:8080, VLAN 20

Switch global:
  radius-server host 10.70.99.2 vrf mgmt [shared secret redacted]
  aaa authentication port-access dot1x authenticator
    enable
Switch interface 1/1/1:
  no shutdown
  no routing
  vlan access 10
  aaa authentication port-access dot1x authenticator
    enable
  mac-auth disabled for this EAP-TLS reference run
  preauth-role removed for this EAP-TLS reference run
Switch local role quarantine:
  vlan access 10
  associate policy q-policy
Policy q-policy:
  sequence 10 class allow-rem (IPv4 destination 10.70.20.10/32), permit
  sequence 100 class deny-all (IPv4 any to any), drop
Switch local role corp:
  no associated policy; VLAN 10 inherited from access port

FreeRADIUS 3.0.26:
  client aruba-1007 { ipaddr = 10.70.99.1; secret = [redacted]; require_message_authenticator = yes }
  eap.default_eap_type = tls
  eap.tls.private_key_file = /etc/freeradius/3.0/certs/nac-server.key [not included]
  eap.tls.certificate_file = /etc/freeradius/3.0/certs/nac-server.pem [not included]
  eap.tls.ca_file = /etc/freeradius/3.0/certs/nac-ca.pem [not included]
  fixed authorize entry: wired-endpoint-01 -> Aruba-User-Role := "quarantine" or "corp"
Endpoint wired supplicant:
  driver = wired; interface = ens3; key_mgmt = IEEE8021X; eap = TLS
  identity = wired-endpoint-01; ca_cert/client_cert/private_key = disposable test files [not included]
```

The endpoint and service virtio NICs required `ethtool -K ens3 tx off tso off gso off` in this EVE environment so an invalid offload checksum would not make a healthy TCP service look blocked. This workaround was applied before all baseline and role probes.
