# Continuous endpoint TCP probes for CoA qualification

The [endpoint probe](probe_endpoint.py) opens a fresh TCP connection on every attempt, binding the source address for corp (`10.70.10.50`) or quarantine (`10.70.20.50`) on the endpoint's **sole** data NIC `ens3`. It refuses to run if the endpoint has another non-loopback interface or lacks either controlled source address. The [serial controller](probe_control_guest.py) installs, starts and fetches the log without adding a management path or touching authentication. A ten-second baseline smoke run produced [10 corp opens and 10 quarantine blocks](probe-smoke20261106.jsonl); this checks the probe tool, not product CoA.

Use a new `PROBE_RUN_ID` for each action. The endpoint must have both controlled source addresses on `ens3` as in the baseline runner. Do not clear the switch session, reconnect the endpoint or issue `wpa_cli reauthenticate` during the CoA window. Start the probe before the product action, record the action time and response, wait for the probe to end, then fetch its log. Keep EAPOL and RADIUS captures active over the same interval. An endpoint address refresh, if required for normal traffic, is a separate recorded action after the continuous probe window.

```powershell
$py = 'D:\AI\.venv-culvcenter\Scripts\python.exe'
$lab = 'D:\AI\nac-wired-lab'
& $py "$lab\eve_guest.py" --upload "$lab\qualification\probe_endpoint.py" '/root/nac-qualification/probe_endpoint.py'
& $py "$lab\eve_guest.py" 'PROBE_ACTION=install python3 -' --script "$lab\qualification\probe_control_guest.py"
& $py "$lab\eve_guest.py" 'PROBE_ACTION=start PROBE_RUN_ID=coa-run-001 PROBE_SECONDS=120 python3 -' --script "$lab\qualification\probe_control_guest.py"
# Trigger the documented product CoA action here; do not manually reauthenticate.
& $py "$lab\eve_guest.py" 'PROBE_ACTION=fetch PROBE_RUN_ID=coa-run-001 python3 -' --script "$lab\qualification\probe_control_guest.py"
& $py "$lab\eve_guest.py" --download '/root/nac-qualification/probe-coa-run-001.jsonl' "$lab\qualification\probe-coa-run-001.jsonl"
```

The EVE guest copy of `probe_endpoint.py` is already installed in the current lab, so a rerun there should skip the `--upload` step, which refuses to overwrite an existing file. The probe records fresh-connection reachability only. Survival of an **already established** TCP connection requires a separate long-lived service/client pair and remains untested.
