# rc10 decision-service outage and recovery — PASS

Exact candidate `b39649f138e126ed6689a53b590855061042e341`, `nacd`
SHA-256 `35699eac162cc2a7315efced101bee3ec18ed87ab1cb108f06f9bb1a7b48195e`.
Before outage, the same validated certificate had policy v4 corp, EOS fresh
SUCCESS VLAN 10 and endpoint corp TCP open. Expected: existing switch
authorization remains until reauthentication; a new authentication fails
closed without NAC decision caching; after recovery, the supplicant may retry
and regain corp.

`nacd` stopped at 08:56:24.656Z. At 08:56:32Z the **existing session** still
opened fresh corp TCP. The operator then used `clear dot1x host all` on the
isolated test switch to create a **new authentication**; EOS reported EAPOL
FAILED/fallback NONE, FreeRADIUS sent Access-Reject at 08:56:55.136Z, and
the endpoint's stale corp lease could no longer open corp TCP. There was no
NAC authorization cache granting a new login. The previously open session
resided in the switch until it was explicitly cleared.

`nacd` was started at 08:57:26.072Z; an immediate readiness read raced startup,
then `/readyz` reported ready at 08:57:39.159Z with the same pinned binary.
The existing supplicant retried automatically, producing an rc10 policy v4
decision at 08:57:26.585Z and RADIUS Access-Accept VLAN 10 at 08:57:27.171Z.
At 08:57:50Z the endpoint again opened corp TCP and quarantine remained
blocked, without manual endpoint renewal or reconnect. [Packet timeline](packet-timeline.json),
[probes](endpoint-probe.jsonl), [before](before-session.txt), and
[after](after-session.txt) correlate the result. The test used an explicit
switch session clear to trigger the **new** login during the outage.
