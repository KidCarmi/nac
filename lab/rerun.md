# Rerun from the preserved EVE lab

Run these from the existing `D:\AI` workspace on the authorized administration workstation. The helper connects through the pinned Proxmox and EVE SSH host keys; generated passwords, service token and RADIUS secret stay in root-only files on Proxmox/EVE/control. The five EVE nodes and NAC service are running at handoff.

```powershell
$py = 'D:\AI\.venv-culvcenter\Scripts\python.exe'
$lab = 'D:\AI\nac-wired-lab'
& $py "$lab\eve_guest.py" 'dpkg-query -W eve-ng; test -r /dev/kvm && test -w /dev/kvm && echo KVM_OK; curl -fsS http://10.70.99.254:18080/readyz'
& $py "$lab\eve_guest.py" 'ip addr replace 10.70.99.254/24 dev vnet0_4; sysctl net.ipv4.ip_forward'
```

If the EVE VM has rebooted, start VM 102 through Proxmox and the five nodes in the `NAC-Wired-Qualification` EVE web topology, then restore the control bridge address and start `nacd`:

```powershell
& $py "$lab\eve_guest.py" 'ip addr replace 10.70.99.254/24 dev vnet0_4'
& $py "$lab\eve_guest.py" --script "$lab\eve_start_nacd.py"
```

For the main rule pair, use the same endpoint certificate and switch port. These commands change only the rule `segment` in KidCarmi/nac. After each rule change, clear the previous vEOS session and ask the same wired supplicant to authenticate again. A new VLAN requires refreshing the endpoint IP.

```powershell
& $py "$lab\eve_guest.py" 'TARGET_SEGMENT=corp python3 -' --script "$lab\deliverables\change_nac_policy.py"
& $py "$lab\eve_guest.py" "EOS_COMMAND='clear dot1x host all' python3 -" --script "$lab\eve_eos_console_command.py"
& $py "$lab\eve_guest.py" "EVE_CONSOLE_PORT=32770 LINUX_COMMAND='wpa_cli -i ens3 reauthenticate' python3 -" --script "$lab\eve_linux_console_command.py"
& $py "$lab\eve_guest.py" "EVE_CONSOLE_PORT=32770 LINUX_COMMAND='ip addr flush dev ens3; ip addr add 10.70.10.50/24 dev ens3; nc -vz -w3 10.70.10.10 8080; ip route replace 10.70.20.10/32 dev ens3; nc -vz -w3 10.70.20.10 8080' python3 -" --script "$lab\eve_linux_console_command.py"
& $py "$lab\eve_guest.py" "EOS_COMMAND='show dot1x hosts' python3 -" --script "$lab\eve_eos_console_command.py"

& $py "$lab\eve_guest.py" 'TARGET_SEGMENT=quarantine python3 -' --script "$lab\deliverables\change_nac_policy.py"
& $py "$lab\eve_guest.py" "EOS_COMMAND='clear dot1x host all' python3 -" --script "$lab\eve_eos_console_command.py"
& $py "$lab\eve_guest.py" "EVE_CONSOLE_PORT=32770 LINUX_COMMAND='wpa_cli -i ens3 reauthenticate' python3 -" --script "$lab\eve_linux_console_command.py"
& $py "$lab\eve_guest.py" "EVE_CONSOLE_PORT=32770 LINUX_COMMAND='ip addr flush dev ens3; ip addr add 10.70.20.50/24 dev ens3; nc -vz -w3 10.70.20.10 8080; ip route replace 10.70.10.10/32 dev ens3; nc -vz -w3 10.70.10.10 8080' python3 -" --script "$lab\eve_linux_console_command.py"
& $py "$lab\eve_guest.py" "EOS_COMMAND='show dot1x hosts' python3 -" --script "$lab\eve_eos_console_command.py"
```

The `nc` command must connect for the allowed service and time out for the other. Inspect adapter decisions and RADIUS on the control serial console (`EVE_CONSOLE_PORT=32773`):

```text
tail -f /var/log/freeradius/nac-adapter.jsonl
tcpdump -nn -r /root/radius.pcap -vvv | grep -E 'Access-(Accept|Reject)|Tunnel-Private-Group-ID|CoA-(ACK|NAK)'
```

For a new-login NAC outage test, stop `nacd` inside EVE with `pkill -x nacd-lab`, clear the switch session and reauthenticate. Both TCP probes must time out. Restore with `eve_start_nacd.py` as above. For CoA, first establish an active VLAN 20 session, change the rule to corp, then run `eve_send_coa.py` through `eve_guest.py --script` without manually forcing reauthentication. The script reads the control node's root-only RADIUS secret file and sends a CoA with `User-Name`, `Calling-Station-Id`, `NAS-Port-Id`, `NAS-IP-Address` and VLAN 10; a CoA-ACK and endpoint TCP change are required.

The printer and EAP-TLS-to-MAB fallback cases cannot be rerun on this vEOS-lab image: its CLI rejects `dot1x mac based authentication` as unsupported on the virtual hardware platform. Use an authorized physical Arista access switch that supports MAB for those two cases. Keep the same isolated control/service topology and allowlist a disposable printer MAC only to quarantine; never treat a MAC by itself as proof of the EAP-TLS endpoint's identity.
