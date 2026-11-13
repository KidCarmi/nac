# Sanitized rc5 packet timeline

Source: EVE host captures on the isolated Aruba topology, 2026-11-13 UTC. Raw archive SHA-256 `32e00cafeb8b81fcd6932203216caf3dcb20fe7e3a58c65385003fec37ffd109`, retained locally and on EVE; it is not published in this package. Capture cap was 180 s or 5,000 packets per tap. This summary omits RADIUS authenticators, TLS certificate material and payloads.

| UI action | CoA request | CoA response | Role attribute | Endpoint EAPOL in action capture | Endpoint result |
|---|---|---|---|---:|---|
| Apply active policy | 09:02:00.760439, ID `0xc5` | ACK 09:02:00.783940 | Aruba 14823/1 `corp`, no Tunnel attributes | 0 | Corp role remained Applied; VLAN 10 and DHCP IP unchanged |
| Quarantine | 09:03:03.541970, ID `0x2c` | ACK 09:03:03.588060 | Aruba 14823/1 `quarantine`, no Tunnel attributes | 0 | Fresh corp TCP timed out; remediation HTTP 200; switch role quarantine Applied |
| Restore | 09:05:51.742865, ID `0xf4` | ACK 09:05:51.776250 | Aruba 14823/1 `corp`, no Tunnel attributes | 0 | First fresh corp SYN/ACK 09:05:51.858845; subsequent HTTP 200; switch role corp Applied |

All three requests targeted the same certificate-authenticated identity and port 1/1/1. Switch session remained dot1x Authenticated, VLAN 10. The endpoint retained `10.70.10.50/24`; there was no DHCP renewal or manual reconnect during these three role actions. The Apply capture overlaps the later Quarantine action because it ran for the full bounded window; the zero EAPOL count applies to the entire window. The Quarantine probe logger did not cover the transition, so no time-to-denial is reported. The Restore SYN/ACK was 0.116 s after request and 0.083 s after ACK on the same EVE capture clock.
