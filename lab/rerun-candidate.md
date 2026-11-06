# Rerun the EVE wired NAC qualification

Use the [candidate topology](NAC-Pilot-Candidate.unl), [runner](run_candidate.py), and OPUS's `docs/pilot/installation.md` and `docs/pilot/operator-walkthrough.md` from the **exact candidate checkout**. The qualified run used `fe7c9810848910aa4dc4536a5074d3ebaef72c7e`; its [matrix](runs/candidate-fe7c981-20261106/evidence-matrix.md), [metadata](runs/candidate-fe7c981-20261106/metadata.json) and [executed runner snapshot](runs/candidate-fe7c981-20261106/run_candidate.executed.py) identify it. Preserve the prior run directory before changing the control overlay. The EVE node images, root-only PKI, RADIUS secret and control credentials remain in EVE, outside deliverables. Do not attach an endpoint management NIC or enable EVE IP forwarding.

## Establish the exact candidate

Check out the **full candidate SHA** from OPUS's branch and run `make pilot-bundle` with Go 1.25. Verify `BUNDLE-MANIFEST.txt` contains the announced `nacd_sha256`; compare a second independent build. If the annotated `pilot-rc1` remote tag is absent, identify the candidate by full SHA and state that the release tag is pending. The installer takes a bundle and its `.sha256`, the existing disposable CA/public server certificate and root-only server key. On a fresh control overlay, follow `candidate_transition_guest.py` (stop node 15 only), archive the old overlay root-only, recreate it from `/opt/unetlab/addons/qemu/linux-nac24-control-bios/virtioa.qcow2`, then start node 15. `reset_candidate_control_guest.py` documents the guarded pattern for the first archived candidate; update its unique archive name and backing-image assertions for each subsequent run. Stage the verified bundle with `candidate_serial_guest.py` using the candidate's dedicated bundle port and run OPUS's `deployments/pilot/install.sh --proxy http://10.70.99.254:3128 --ca ... --server-cert ... --server-key ...`. Record `/etc/nac/install-report.txt` and `/readyz`. The proxy is limited to candidate control `10.70.99.2` and approved package hosts. It never routes endpoint data.

The EOS startup config must persist before restart testing. On a factory-fresh vEOS boot, check `show zerotouch`; if active, `zerotouch cancel` reloads EOS. Apply the generated switch config, `write memory`, and check `show startup-config`. The initially accepted `write memory` attempt while ZeroTouch was active actually returned `% Cannot copy to startup-config when ZeroTouch is enabled`; that first reboot lost the config. The corrected procedure was verified by another vEOS reboot.

## Invoke the runner and product UI

From PowerShell in the authorized workstation, substitute the settled exact candidate SHA and hash:

```powershell
$py = 'D:\AI\.venv-culvcenter\Scripts\python.exe'
$q = 'D:\AI\nac-wired-lab\qualification'
$env:EVE_GUEST_SCRIPT = 'D:\AI\nac-wired-lab\eve_guest.py'
$run = 'candidate-<7-hex-sha>-<YYYYMMDD>'
& $py "$q\run_candidate.py" $run init --commit '<40-hex-sha>' --nacd-sha256 '<64-hex-sha256>'
& $py "$q\run_candidate.py" $run preflight
& $py "$q\run_candidate.py" $run capture-start
& $py "$q\run_candidate.py" $run service-health
& $py "$q\run_candidate.py" $run fresh
& $py "$q\run_candidate.py" $run probe --expect none
```

Open the operator console through the loopback-only `candidate_console_tunnel.py` at `https://127.0.0.1:18443/ui/`. In **Switches**, register the actual RADIUS source `10.70.99.1`, port `Ethernet1`, corp 10 and quarantine 20; apply generated EOS config and save it. In **Devices**, add the endpoint record and bind the rejected valid certificate's FreeRADIUS-validated issuer+serial+CN. Publish a corp rule requiring `identity.bound=true` and `mac.matches_device=true`; keep default deny. The runner deliberately leaves all policy, device and CoA actions to the documented product UI and records only lab-side probes. Record each UI action's UTC time, policy version, decision ID and CoA state in the run's `operator-actions.md`.

```powershell
& $py "$q\run_candidate.py" $run valid-start
& $py "$q\run_candidate.py" $run probe --expect corp
& $py "$q\run_candidate.py" $run switch-status
& $py "$q\run_candidate.py" $run fresh
& $py "$q\run_candidate.py" $run invalid-start
& $py "$q\run_candidate.py" $run probe --expect none
& $py "$q\run_candidate.py" $run valid-restart
& $py "$q\run_candidate.py" $run probe --expect corp
```

For the invalid test, use the separately staged invalid-CA certificate; a fresh session means clear Et1 before starting it. The `fresh` action clears the switch and stops the supplicant, recreating the two static source addresses on **one** endpoint NIC. The two addresses let fresh TCP probes explicitly choose the source subnet without an off-subnet return-path false block. No address renewal was needed in this static lab. With DHCP, renew after each VLAN change and log that action.

In **Policy**, change only the segment on the same rule from corp to quarantine. Preview against recorded events and record predicted decisions and uncertainty. Publish, then use the session's **Re-authenticate (apply active policy)** action. No shell reauthentication is part of this policy step.

```powershell
& $py "$q\run_candidate.py" $run probe --expect quarantine
& $py "$q\run_candidate.py" $run switch-status
```

To test the product's operator CoA as a real access change, first restore the prior corp policy in **Policy → Versions**, use the session **Re-authenticate** action, and confirm corp access. This policy-version restoration is recorded separately from the later session override restoration.

```powershell
& $py "$q\run_candidate.py" $run probe --expect corp
& $py "$q\run_candidate.py" $run continuous-install
& $py "$q\run_candidate.py" $run continuous-start --probe-id coa1 --seconds 180 --hold-corp
```

With probes running, use the product session **Quarantine** action. Once the CoA ACK, switch VLAN 20 and fresh TCP transition are observed, release the already established corp socket and record whether its delayed HTTP request gets a response:

```powershell
& $py "$q\run_candidate.py" $run continuous-release --probe-id coa1
```

Then use the product session **Restore** action. Do not clear Et1, reconnect the endpoint or invoke `wpa_cli reauthenticate` during this CoA interval. Wait for both permitted and denied fresh TCP states to appear. Then:

```powershell
& $py "$q\run_candidate.py" $run continuous-stop --probe-id coa1
& $py "$q\run_candidate.py" $run continuous-fetch --probe-id coa1
& $py "$q\run_candidate.py" $run probe --expect corp
```

For outage, record an existing corp probe, stop only nacd, probe existing access, clear Et1 and invoke endpoint reauthentication **explicitly as operator actions** to create a new login, expect both TCP services BLOCKED, restore nacd, then wait for automatic supplicant retry and corp OPEN. `start_nacd` can report a transient `/readyz` failure before the service is ready; verify `CONTROL_ACTION=verify` and do not infer failure from that one curl.

```powershell
& $py "$q\run_candidate.py" $run outage-stop
& $py "$q\run_candidate.py" $run probe --expect corp
& $py 'D:\AI\nac-wired-lab\eve_guest.py' 'SWITCH_ACTION=clear_session python3 -' --script "$q\candidate_switch_guest.py"
& $py "$q\run_candidate.py" $run reauth
& $py "$q\run_candidate.py" $run probe --expect none
& $py "$q\run_candidate.py" $run outage-recover
& $py "$q\run_candidate.py" $run probe --expect corp --deadline 120
```

Finally stop/restart **candidate nodes 11–15 only**. Wait for vEOS boot, verify saved config and service health before treating a fresh unauthenticated block as enforcement evidence. The current runner includes readiness and service-health checks before `fresh`. Then verify valid corp, activate the saved quarantine version through the UI and reauthenticate, then restore corp and reauthenticate. Wait up to 90 seconds for data forwarding after each switch SUCCESS; record transient both-blocked periods separately. In the qualified `fe7c981` run the final post-reboot restoration used explicit operator endpoint `wpa_cli reauthenticate`, and a transient EAPOL failure preceded success. Keep that action visible; it is separate from the dedicated CoA test.

```powershell
& $py "$q\run_candidate.py" $run restart-stop
& $py "$q\run_candidate.py" $run restart-start
& $py "$q\run_candidate.py" $run preflight
& $py "$q\run_candidate.py" $run service-health
& $py "$q\run_candidate.py" $run fresh
& $py "$q\run_candidate.py" $run probe --expect none
& $py "$q\run_candidate.py" $run valid-start
& $py "$q\run_candidate.py" $run probe --expect corp
& $py "$q\run_candidate.py" $run capture-stop
& $py "$q\run_candidate.py" $run collect --probe-id coa1
```

Parse captures with `pcap_timeline.py`, inspect the **switch-originated** Access-Request/Accept or Reject and CoA Request/ACK, then correlate EAPOL, assigned VLAN, product policy version/rule/decision ID, switch `show dot1x hosts` and endpoint TCP. A CoA ACK or displayed VLAN alone is not a pass. Use `analyze_probe.py` on the continuous JSONL to identify fresh TCP transitions and the held-connection result. In this static one-NIC lab, two source addresses avoid DHCP renewal. In a DHCP deployment, automate a client-side lease renew after successful VLAN transition and record it; a switch port shutdown/no-shutdown is a disruptive fallback that triggers link and authentication changes and must not be counted as CoA-only evidence. Screen deliverables for test keys and shared secrets before sharing. MAB and rejected-EAP fallback require authorized physical Arista hardware with supported MAB; label those cases **BLOCKED** on vEOS-lab 4.29.2F.
