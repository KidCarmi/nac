# IPv6 fresh-flow rerun

Start from the isolated [EAP-TLS reference lab](../aruba-1007-reference-20261109/rerun.md), with the session in `corp` and [direct role CoA](../aruba-1007-coa-20261109/rerun.md) working.

1. On the endpoint's only data NIC add `ip -6 addr add 2001:db8:70:10::50/64 dev ens3`. On the corp service NIC add `ip -6 addr add 2001:db8:70:10::10/64 dev ens3`. Wait for duplicate-address detection to finish.
2. On corp start `python3 -m http.server 8083 --bind 2001:db8:70:10::10`. Require HTTP 200 locally and from the endpoint with `curl -g -6 --connect-timeout 2 --max-time 3 http://[2001:db8:70:10::10]:8083/` before testing isolation.
3. Start [bounded capture](aruba_ipv6_capture_guest.py) on EVE. Send session-targeted CoA with `Aruba-User-Role=quarantine`. Require CoA ACK and switch role change. Open a **fresh** IPv6 TCP connection from the endpoint and record the HTTP result. Compare endpoint and corp taps for the IPv6 SYN. Probe IPv4 corp independently as a control.
4. Send `Aruba-User-Role=corp` by CoA and require fresh IPv6 and IPv4 HTTP 200. Remove the temporary IPv6 addresses and stop the disposable service.

This test uses static, same-subnet IPv6 addresses. It does not exercise IPv6 remediation or routed IPv6.
