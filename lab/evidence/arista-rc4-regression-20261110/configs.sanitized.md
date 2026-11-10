# Sanitized Arista rc4 lab settings

The [EVE export](NAC-Pilot-DHCP.unl) has four isolated bridges: endpoint–Ethernet1, corp–Ethernet2, quarantine–Ethernet3, and RADIUS/eAPI control–Ethernet4. The endpoint has one DHCP4 data interface (`ens3`) and no management route. EVE `net.ipv4.ip_forward=0`; no EVE host address was present on the data bridges during endpoint tests. Temporary, self-cleaning namespaces verified each TCP service from its own bridge before admission tests.

Relevant running EOS lines, with the disposable RADIUS secret removed:

```text
Software image version: 4.29.2F
radius-server host 10.70.99.2 key [REDACTED]  # live rc4 target, deliberately unsaved
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
```

The product switch record maps `corp` → VLAN 10 and `quarantine` → VLAN 20, platform `arista-eos`, enforcement `vlan`, RADIUS source/NAS IP `10.70.99.1`, server `10.70.99.2`, access port `Ethernet1`, and CoA port 3799. Its eAPI observer answered `show dot1x hosts`; this run did not independently retest TLS trust. FreeRADIUS validates the client certificate and passes its issuer/serial to NAC's existing binding; the MAC corroborates that identity. The v2 rule `bound-eap-corp` requires `identity.bound=true` and `mac.matches_device=true`, action `allow`, segment `corp`. The v13 draft/published rule changed **only** `segment` to `quarantine`, then the operator restored v2. A separate operator Quarantine override took precedence over the v2 corp rule, as the decision record shows.

The endpoint's netplan uses `renderer: networkd`, `dhcp4: true`, `dhcp6: false` on `ens3`. Corp and quarantine each have a 120-second DHCP lease on their distinct /24. A product CoA does not toggle link carrier; the guest retained its old address until an explicitly recorded manual `networkctl down/up` in all four transitions. RADIUS VLAN attributes and switch session state are insufficient to claim endpoint recovery.
