# Isolated short-timer DHCP config

The two original `nac-udhcpd` services had unchanged 120-second scopes. During this follow-up only, both service guests used the same dnsmasq configuration as the [default-timer comparison](https://github.com/KidCarmi/nac/tree/8db6fe899809c2a20db741d08f72e8e3711680e1/lab/evidence/dhcp-server-comparison-rc10-20261107), with these two extra lines:

```ini
dhcp-option=58,30i
dhcp-option=59,45i
```

`58` is renewal T1 and `59` is rebinding T2. The `i` suffix forces a four-byte integer. The lease remained `3600s`; router and DNS were `10.70.10.10` for corp and `10.70.20.10` for quarantine. Both guests used `dhcp-authoritative` and their original `ens3` interfaces. `dnsmasq --test` passed on both, and the actual ACK packets show lease/T1/T2 = 3600/30/45 seconds. No NAC policy, RADIUS, switch port or endpoint certificate configuration was changed for this run.

The config was backed up as `/etc/nac-dnsmasq.conf.astra-before-short-timers` on each guest before modification. After evidence collection, the saved config was restored, dnsmasq stopped and the original 120-second udhcpd service restarted. The backup name no longer exists after successful restoration.
