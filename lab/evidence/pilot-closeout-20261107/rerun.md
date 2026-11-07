# Reproduce the rc10 pilot closeout

This package extends the frozen [rc9 baseline](../dhcp-qualification-rc9-20261107/rerun.md). Use the [isolated EVE topology](NAC-Pilot-DHCP.unl) on VM 102 and the [sanitized configuration](configs.sanitized.md). Keep the original rc9 topology, evidence, Proxmox bridges, host configuration and unrelated VMs unchanged. The runner requires the existing lab's SSH host-key and credential files; none are distributed here.

## Pin and install

Check out KidCarmi/nac commit `b39649f138e126ed6689a53b590855061042e341`. On the pinned Go 1.25.0 builder, run `go test ./...` and `make pilot-bundle VERSION=pilot-rc10`. Verify `bin/nacd` SHA-256 `35699eac162cc2a7315efced101bee3ec18ed87ab1cb108f06f9bb1a7b48195e` and bundle SHA-256 `488ebfaf77c445d2e0b3bdac0489202f572f81f19b4f5521415c5666e0e29f6a`. The RADIUS adapter SHA-256 was `602c3fc5ff195c818bca6f5a15ca665b588077a07025315362cbb5331826c01b`; FreeRADIUS was 3.2.5 and EOS was vEOS-lab 4.29.2F. Run the candidate's `docs/pilot/installation.md` installer with `deployments/pilot/install.sh --test-pki` on a fresh, separate Ubuntu 24.04.5 overlay. Verify `/version`, the running binary digest, `/readyz`, and FreeRADIUS/PostgreSQL service health. Retain generated credentials and keys inside the disposable guest.

Use the product operator walkthrough to register switch `10.70.99.1`, corp VLAN 10 and quarantine VLAN 20. Bind the verified client certificate issuer and serial to the endpoint record; a MAC match alone is insufficient. Publish the rule requiring both `identity.bound` and `mac.matches_device`, then verify RADIUS Access-Accept VLAN 10, EOS Et1 `SUCCESS VLAN 10`, a DHCP lease in `10.70.10.0/24`, and fresh corp TCP reachability. Prove HTTP 200 from the corp and quarantine services from their own segments first.

## Bounded capture and fresh probes

On the lab workstation in PowerShell, with the existing host-key files configured:

```powershell
$py = 'D:\AI\.venv-culvcenter\Scripts\python.exe'
$q = 'D:\AI\nac-wired-lab\qualification'
$sha = 'b39649f138e126ed6689a53b590855061042e341'
$run = 'rc10-rerun-01'
& $py "$q\run_dhcp_candidate.py" topology --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" health --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" capture-start --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" probe-start --candidate $sha --run $run --seconds 900 --hold-corp
& $py "$q\run_dhcp_candidate.py" endpoint-probe --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" switch-status --candidate $sha --run $run
```

The continuous JSONL probe is capped at 8 MiB; each of the four packet captures rotates at 8 MiB with at most two files. Check endpoint filesystem headroom before each run. Record all action timestamps in UTC. Through the NAC Console, Quarantine the active session, wait up to 150 s for natural DHCP/address recovery, and Restore it. Repeat a corp→quarantine→corp change by modifying only the rule segment, previewing and publishing each version, then using the documented Re-authenticate action. Do not renew DHCP, reconnect the endpoint, or bounce Et1 in the primary run. After each transition record the lease/address, routes, DNS, EAPOL, DHCP, RADIUS VLAN attributes, switch session, NAC decision/rule/policy version and fresh corp/quarantine TCP results. Record established-flow behavior separately if it is tested. An already published rule changes new authorizations; an existing operator quarantine override takes precedence until restored.

```powershell
& $py "$q\run_dhcp_candidate.py" probe-stop --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" probe-fetch --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" capture-stop --candidate $sha --run $run
& $py "$q\run_dhcp_candidate.py" capture-download --candidate $sha --run $run
& $py "$q\analyze_pcap.py" "$q\runs\candidate-b39649f-dhcp-$run\endpoint.pcap0" "$q\runs\candidate-b39649f-dhcp-$run\radius.pcap0" "$q\runs\candidate-b39649f-dhcp-$run\corp-dhcp.pcap0" "$q\runs\candidate-b39649f-dhcp-$run\quarantine-dhcp.pcap0" --out "$q\runs\candidate-b39649f-dhcp-$run\packet-timeline.json"
```

For invalid EAP-TLS, stop the valid supplicant, clear only the isolated Et1 session, start the invalid certificate client, and require Access-Reject, EAPOL FAILED and blocked corp TCP despite any stale address. For outage, stop only nacd while an existing session continues; then clear Et1 to force a new login, require reject/blocked corp, restart nacd and observe retry/recovery. The exact [core](runs/rc10-core/findings.md), [invalid](runs/rc10-invalid/findings.md) and [outage](runs/rc10-outage/findings.md) records are the comparison cases. Keep each run's timing with its own probe file.

For trusted eAPI, install the disposable CA public certificate in the NAC guest, set `SSL_CERT_FILE` to that path and `NAC_EAPI_INSECURE_SKIP_VERIFY=false`, and configure the EOS SSL profile in [the report](report.md). The good switch certificate has IP SAN `10.70.99.1`; test a same-CA cert with IP SAN `10.70.99.9` and require an x509 hostname error, then restore the good certificate. Do not export the private key.

The completed Windows run used Windows Server 2025 Standard Evaluation **Desktop Experience** build 26100, a single e1000 data NIC, DHCP and a machine EAP-TLS profile. Its setup-only NAT NIC and corrective read-only floppy were absent from the primary boot. `windows_profile_fix.ps1` documents the pinned trusted-root CA and `radius.nac.lab` wired profile; replace its disposable CA thumbprint with the new run's CA thumbprint. A profile using Microsoft's HTTPS example XML namespaces failed import on this build; the HTTP protocol namespace imported. The separate initial profile attempt lacked a trusted root and failed EAP-TLS, as [recorded](runs/rc10-windows-profile-diagnostic/findings.md). It is a reproducible endpoint setup diagnostic, not a NAC defect.

Use the supplied `launch_windows_endpoint_guest.py test_start` after installing and staging the disposable Desktop Experience disk and machine certificate. The `windows_schedule_probe.ps1` startup task launches `windows_dhcp_probe.ps1` as SYSTEM, caps JSONL to 8 MiB and runs up to 20 minutes. For the primary run, start bounded captures before Windows boot, clear Et1, confirm both segment services, and allow the endpoint to authenticate and get a corp lease naturally. Take each CoA through the console or documented API; record which one. The run-specific [Windows finding](runs/rc10-windows-one-nic/findings.md) includes two cycles and action times. The cumulative endpoint log can be fetched only over the endpoint's **currently authorized data segment** using `windows_data_vm.py --segment corp --fetch --out <path>` after corp restoration. This helper temporarily adds a same-segment EVE host address for SSH and removes it afterward; EVE forwarding must remain 0. It never gives the endpoint a second NIC. Summarize with `summarize_windows_probe.py <probe.jsonl> name=UTC...`, listing action times in order. `analyze_pcap.py` extracts DHCP lease, T1 and T2 options when present. Record absent options as absent; do not report a protocol default as a measured value. Any manual renewal or reconnect belongs in a separate diagnostic and cannot count as automatic recovery.

Verify the package with `python verify_package.py .` and [the SHA-256 manifest](sha256-manifest.json). The package contains no vendor image, test private key, RADIUS secret or admin password.
