# Lab restoration check

At 17:09:50 UTC the endpoint supplicant was stopped. `clear dot1x host all` left no Et1 session. At 17:10:43 UTC both service guests reported `nac-udhcpd` active, only BusyBox bound to UDP/67, and the original `option lease 120` scope. Corp guest free disk: 31 MiB; quarantine: 63 MiB. The timer-only dnsmasq configs were replaced with their saved pre-run copies. Both TCP services independently returned HTTP 200 from their own EVE bridges and EVE `ip_forward` was 0. Captures were stopped and downloaded. No Proxmox bridge, host setting or unrelated VM was modified.
