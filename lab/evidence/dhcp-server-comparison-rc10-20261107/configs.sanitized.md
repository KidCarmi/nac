# DHCP comparison configuration

Both service guests retained their original `/etc/nac-udhcpd.conf` 120-second scopes. For the comparison only, dnsmasq 2.91-0ubuntu0.22.04.2 ran on `ens3` with:

```ini
port=0
interface=ens3
bind-interfaces
dhcp-authoritative
log-dhcp
log-facility=/var/log/nac-dnsmasq.log
dhcp-leasefile=/var/lib/nac-dnsmasq.leases
dhcp-range=10.70.<SEGMENT>.50,10.70.<SEGMENT>.99,255.255.255.0,3600s
dhcp-option=option:router,10.70.<SEGMENT>.10
dhcp-option=option:dns-server,10.70.<SEGMENT>.10
dhcp-option=option:domain-name,nac-lab.test
```

`<SEGMENT>` is `10` for corp and `20` for quarantine. Real DHCP ACKs contained lease/T1/T2 = 3600/1800/3150 seconds. No NAC, RADIUS, EAP-TLS, switch, or endpoint configuration changed between the early and near-T1 phases. NAC policy v4 rule `bound-eap-corp`; Quarantine and Restore used the operator override and CoA. RADIUS, EAP-TLS, and eAPI configuration is recorded without secrets in the published rc10 baseline evidence.
