# Reproduce the pilot-rc9 DHCP qualification

Use the existing EVE VM 102 and the [isolated topology](NAC-Pilot-DHCP.unl). The package [configuration](configs.sanitized.md) records the one-NIC endpoint, VLANs, DHCP scopes, service targets and EOS intent. Preserve the [rc8 baseline](baseline-rc8.md). Use only authorized vEOS-lab 4.29.2F. Keep Proxmox bridges/host and unrelated VMs unchanged; verify EVE `net.ipv4.ip_forward=0` and the topology hash `8b69cf973f3cc545082aac0334aaf0aaa2e9c7b6563a3f66d5d4ec86c62ea935`.

## Install and pin

Check out exact KidCarmi/nac commit `3e92642110f97d3f42a43652d33479e82cfbcc35` in a separate clean checkout. With Go 1.25.0, run `go test ./...` and `make pilot-bundle VERSION=pilot-rc9`. Verify bundled `bin/nacd` SHA-256 `d2d93e3ddc40a7eadcddfd3275f3d8a7aa5610ca07b43eb219bb9d59267a3749` and full commit in `BUNDLE-MANIFEST.txt`. Follow that commit's `docs/pilot/installation.md` and `docs/pilot/operator-walkthrough.md`; record install duration. The package includes the [build](build_rc9_guest.py), [bundle serving](serve_rc9_guest.py), [upgrade](rc9_upgrade_guest.py) and [service](rc9_service_guest.py) lab helpers as reproducible source, not as a replacement for product documentation. Verify `/version`, `/usr/local/bin/nacd`, `/proc/<nacd MainPID>/exe`, FreeRADIUS, PostgreSQL, and the unchanged secret-env-file digest. Do not export that file.

The exact rc9 [clean-install run](runs/candidate-3e92642-clean-rc9-20261107/findings.md) used a separate recoverable overlay of EVE's Ubuntu 24.04.5 `linux-nac24-base/virtioa.qcow2`; it did not alter the existing NAC control node. On EVE, create a **new** overlay path, grow its virtual disk to 12 GB, and boot it with 2 vCPUs/2 GB RAM and QEMU user-mode NAT. The [DHCP networkd file](qualification/clean_rc9_10_nac_clean.network) and [SSH lab drop-in](qualification/clean_rc9_01_ssh.conf) were copied only into the overlay with `virt-copy-in`; a disposable root credential was set with `virt-customize --root-password file:<private lab credential path>`. Generate SSH host keys inside the guest and start SSH over EVE's loopback-only QEMU forward. The [nested SSH helper](qualification/clean_rc9_vm.py) uses existing lab host-key and credential files outside this package; adapt those paths if reproducing elsewhere. Inside the fresh guest, require that `/etc/nac` is absent, grow `/dev/vda1` with `growpart` and `resize2fs`, verify direct Ubuntu archive access, then run the documented commands:

```bash
cd /root
sha256sum -c nac-pilot-pilot-rc9.tar.gz.sha256
tar xzf nac-pilot-pilot-rc9.tar.gz
cd nac-pilot-pilot-rc9
deployments/pilot/install.sh --test-pki
```

The release bundle and checksum were copied from EVE's pinned build to the clean guest over SSH and are not in this package. [Installer output](runs/candidate-3e92642-clean-rc9-20261107/installer-output.txt) and the [verification script](qualification/clean_rc9_verify_guest.sh) record the exact result. Verify both installed and running nacd SHA-256, `systemctl is-active` for nacd/FreeRADIUS/PostgreSQL, `/readyz`, `/version` and HTTPS `/ui/`, then reboot the clean guest and repeat the checks. Its source Ubuntu image lacked persistent SSH host-key/runtime setup, so the recorded post-reboot product check used serial; NAC services and console came up without intervention. The [tmpfiles drop-in](qualification/clean_rc9_ssh_tmpfiles.conf) was added to the isolated overlay afterward for repeatable SSH management. On that disposable clean VM, the [account test](qualification/clean_rc9_account_guest.py) exercises wrong/current password behavior, token revocation, the documented `nac-reset-password` command and recovered login; it prints only status codes and retains the final credential only inside the VM. Stop only the separate QEMU process when finished and retain its overlay inside EVE; never export it because it contains generated test secrets.

## Run admission and DHCP transitions

On the authorized Windows lab workstation, from PowerShell:

```powershell
$py = 'D:\AI\.venv-culvcenter\Scripts\python.exe'
$q = 'D:\AI\nac-wired-lab\qualification'
$sha = '3e92642110f97d3f42a43652d33479e82cfbcc35'
$run = 'rc9-rerun-01'
& $py "$q\run_dhcp_candidate.py" topology --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" health --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" capture-start --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" probe-start --candidate $sha --run $run --seconds 900 --hold-corp
& $py "$q\run_dhcp_candidate.py" endpoint-stop --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" switch-clear --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" endpoint-probe --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" endpoint-valid --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" switch-status --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" endpoint-probe --candidate $sha --run $run
```

First prove both services return HTTP 200 from their own segments. For invalid-certificate admission, stop the valid supplicant, clear Et1, start the runner's `endpoint-invalid` action, and require Access-Reject, EAPOL Failure, Et1 FAILED/no VLAN, and blocked fresh corp TCP. Restore the valid supplicant before policy/CoA work. The runner lists supported actions with `--help`.

The package's [qualification source directory](qualification) and [EVE connection helper](eve_guest.py) are an audit snapshot of the runner and its dependencies. The command above uses the original lab workspace because its SSH host-key files and existing isolated EVE guest are already configured there. If moving the snapshot, configure host-key paths and EVE access for the new workstation before running it; credentials remain outside the package. Before `probe-start`, check the endpoint root filesystem has free space with `df -h /` over serial; the rc9 console run lost continuous probe data when its 2 GB disk filled. The updated runner starts the probe with `systemd-run` and confirms the first JSON record was flushed before reporting `PROBE_STARTED`. Stop on any startup error and diagnose endpoint storage before testing policy.

For the primary **console** workflow, ensure no obsolete native browser prompt is open, open a local tunnel to the rc9 console, sign in with the existing disposable credential, and use the product's in-page confirmations. Keep a **verified, nonempty** continuous probe and synchronized endpoint, RADIUS and both DHCP captures running. Record action, dialog confirmation and CoA timestamps. Use the UI to Quarantine and Restore the active session, preview and publish only the `bound-eap-corp.segment` change, Re-authenticate, then restore policy v2 and Re-authenticate. After a publish, an existing operator quarantine override remains until the individual session Restore action clears it. The [rc9 UI run](runs/candidate-3e92642-dhcp-rc9-ui-20261107/findings.md) passed the access changes using one-shot TCP and packet evidence, but its continuous probe was blocked by endpoint disk space; rerun with the preflight to obtain exact console-run interruption times.

For separately labeled **API diagnostics**, the [policy helper](nac_policy_api_rc9_guest.py) accepts `NAC_POLICY_ACTION=read|draft-quarantine|preview|publish|restore-v2`; the existing `nac_lab_api_guest.py` accepts `NAC_API_ACTION=details|switch|quarantine|restore|reauthenticate`. Pin a new run ID and save each request and response. CoA should trigger switch reauthentication; policy publish alone takes effect at the next authentication. Wait **at least 150 s** per VLAN transition for natural Linux DHCP. Do not renew DHCP, reconnect the endpoint or bounce Et1 in the primary run. Save the lease/address, route, DNS, carrier, EAPOL, DHCP, RADIUS VLAN attributes, switch host state, NAC decision ID/rule/policy version and fresh TCP probes. To test an established connection, explicitly release the runner's held-flow marker (`probe-release`) after transition and record the result; this package does not contain such a result.

```powershell
& $py "$q\run_dhcp_candidate.py" probe-fetch --candidate $sha --run $run
& $py "$q\analyze_dhcp_probe.py" "$q\runs\candidate-3e92642-dhcp-$run\endpoint-probe.jsonl" --action-at 'YYYY-MM-DDTHH:MM:SSZ' --before corp --after quarantine
& $py "$q\run_dhcp_candidate.py" probe-stop --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" probe-fetch --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" capture-stop --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" capture-download --candidate $sha --run $run
& $py "$q\pcap_timeline.py" "$q\runs\candidate-3e92642-dhcp-$run\radius.pcap"
```

For outage, stop **only nacd** while the existing session probes continue, then tear down the supplicant, clear Et1 and start a fresh valid-certificate login. Expect existing switch forwarding until teardown, fresh Access-Reject and blocked corp, then automatic supplicant retry after nacd recovery. For restart, stop/start **only** EVE nodes 31–35 with the existing `dhcp_transition_guest.py`, verify saved EOS configuration and both services, then rerun admission, rule-only quarantine and policy restoration. An early nacd readiness check can race startup; use a settled `/readyz` and running-binary check. Scan all captures for known plaintext credentials before handoff. Keep vendor disk, source bundle, keys, shared secrets and admin credential out of exported artifacts.

The [core](runs/candidate-3e92642-dhcp-rc9-core-20261107/findings.md), [outage](runs/candidate-3e92642-dhcp-rc9-outage-20261107/findings.md), and [restart](runs/candidate-3e92642-dhcp-rc9-restart-20261107/findings.md) folders show expected evidence and result labels for comparison.

## Observer and accounting diagnostics

For an eAPI outage, use `SWITCH_ACTION=disable_eapi_diagnostic` with `dhcp_switch_guest.py` on the isolated switch. Confirm Et1 still shows SUCCESS VLAN10; query `NAC_API_ACTION=switch|details` and save reachability and observation age. Wait past the configured **5m0s** maximum while continuous fresh TCP probes run. Require `freshness=stale`, liveness `unknown`, and continued endpoint corp access. Always use `SWITCH_ACTION=restore_eapi_diagnostic`, then wait for a fresh `reachable=yes`, SUCCESS VLAN10 observation. [Short](runs/candidate-3e92642-dhcp-rc9-observer-20261107/findings.md) and [long](runs/candidate-3e92642-dhcp-rc9-stale-20261107/findings.md) rc9 runs passed.

For an old Accounting Stop, first pin the current active session ID and decision. Inspect the unique old frame with [replay helper](qualification/replay_stale_accounting_rc7_guest.py) using `REPLAY_ACTION=inspect`; it requires exactly one unique historical Stop and checks EVE forwarding `0`. With RADIUS capture and fresh TCP probes running, send **one** frame with `REPLAY_ACTION=send` on the isolated control bridge. Require an accounting event explaining the ignored old Class, an unchanged active session ID/decision and continued endpoint TCP. [Exact rc9 result](runs/candidate-3e92642-dhcp-rc9-accounting-20261107/findings.md). The helper reads no RADIUS secret; do not export the historical full lab capture.

For link-down, the [guarded node helper](qualification/dhcp_linkcycle_guest.py) can stop/start only EVE endpoint node32 while checking topology hashes, other labs stopped and switch/control/services running. In this virtual topology, stopping node32 **did not** lower vEOS Et1, so it cannot qualify physical unplug. The separate `SWITCH_ACTION=port_bounce` diagnostic administratively shuts/no-shuts only isolated Et1 for two seconds. Record that operator action, RADIUS Accounting Stop, zero active sessions, blocked TCP despite any stale lease, then explicitly start the valid supplicant and require a new session/decision, Accounting Start and corp TCP. The [rc9 link-cycle result](runs/candidate-3e92642-dhcp-rc9-linkcycle-20261107/findings.md) records these manual actions. Physical unplug requires authorized Arista hardware with the corp/quarantine networks isolated.
