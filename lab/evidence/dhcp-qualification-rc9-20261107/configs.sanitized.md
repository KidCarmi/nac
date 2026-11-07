# Isolated DHCP topology configuration (sanitized)

Topology: `NAC-Pilot-DHCP.unl` (SHA-256 `8b69cf973f3cc545082aac0334aaf0aaa2e9c7b6563a3f66d5d4ec86c62ea935`). EVE VM 102 only; nodes 31–35. Endpoint node 32 has one data NIC (`ens3`) to vEOS node 31 Ethernet1. Corp node 33 is on Ethernet2/VLAN 10; quarantine node 34 is on Ethernet3/VLAN 20. NAC control node 35 and EOS Ethernet4 use isolated 10.70.99.0/24. The endpoint has no management NIC. EVE `net.ipv4.ip_forward=0` and no host address on either data bridge during endpoint probes.

| Segment | VLAN | Service | DHCP scope | Router and DNS options | Lease |
| --- | ---: | --- | --- | --- | ---: |
| corp | 10 | 10.70.10.10:8080 | 10.70.10.50–99/24 | 10.70.10.10 | 120 s |
| quarantine | 20 | 10.70.20.10:8080 | 10.70.20.50–99/24 | 10.70.20.10 | 120 s |

Each service and its BusyBox `udhcpd` scope run on the same isolated segment. The DNS option was observed at the endpoint; successful name resolution was not separately tested. Segment services were independently checked for HTTP 200 from their own EVE bridge, then the temporary health-check address was removed. There is no intersegment route in the data plane.

Endpoint `/etc/netplan/99-nac-lab.yaml`:

```yaml
network:
  version: 2
  renderer: networkd
  ethernets:
    data:
      match:
        name: 'en*'
      dhcp4: true
      dhcp6: false
      optional: true
```

Relevant EOS intent (secrets omitted):

```text
vlan 10
   name corp
vlan 20
   name quarantine
aaa authentication dot1x default group radius
aaa accounting dot1x default start-stop group radius
dot1x system-auth-control
dot1x dynamic-authorization
interface Ethernet1
   switchport access vlan 10
   dot1x pae authenticator
   dot1x port-control auto
interface Ethernet2
   switchport access vlan 10
interface Ethernet3
   switchport access vlan 20
interface Ethernet4
   no switchport
   ip address 10.70.99.1/24
management api http-commands
   no shutdown
```

The product's switch record maps corp→10 and quarantine→20, RADIUS source 10.70.99.1, access port Ethernet1. The policy v2 rule `bound-eap-corp` requires `identity.bound=true` and `mac.matches_device=true` and allows corp. The device binding uses the client certificate's FreeRADIUS-validated issuer and serial; the MAC is only corroborating evidence. The FreeRADIUS `rlm_rest` adapter uses exact SHA-256 `602c3fc5ff195c818bca6f5a15ca665b588077a07025315362cbb5331826c01b`. `nacd` holds the policy and returns VLAN tunnel attributes. The adapter does not cache authorization results. Its secret, console credentials, disposable CA private key and vendor image remain root-only in EVE and are excluded from exported artifacts.

eAPI on the rc6 lab uses `NAC_EAPI_TLS_ALLOW_RSA_KEX=true` for this stock EOS TLS handshake. `NAC_EAPI_INSECURE_SKIP_VERIFY=true` is a **disposable lab-only** setting for the switch certificate. A pilot switch needs a trusted certificate or the documented EOS ECDHE SSL profile and normal certificate verification. The observation maximum age was five minutes.
