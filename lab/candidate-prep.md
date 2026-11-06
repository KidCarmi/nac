# Separate pilot candidate lab preparation

The preserved baseline remains in `NAC-Wired-Qualification.unl`. The candidate topology is [NAC-Pilot-Candidate.unl](NAC-Pilot-Candidate.unl), uploaded inside EVE at `/opt/unetlab/labs/NAC-Pilot-Candidate.unl`. It has separate lab/node UUIDs, **node IDs 11–15** and **bridge IDs 11–14**; EVE reports all candidate nodes stopped after a bounded control-node boot check. The node/bridge IDs are separate from baseline IDs 1–5/1–4 so EVE status and runtime files cannot collide. Both labs use the same `10.70.*` address plan, so stop baseline nodes before assigning the EVE host's control address to the candidate bridge. Do not alter the Proxmox bridge or attach an endpoint management NIC.

The candidate control image is a separate clean Ubuntu 24.04.5 template, `/opt/unetlab/addons/qemu/linux-nac24-control-bios/virtioa.qcow2`, with a 16 GiB virtual disk and 14 GiB root filesystem. It has one isolated control NIC and a root-only console credential stored on EVE at `/root/nac-pilot-control.rootpw`. It contains no installed NAC or FreeRADIUS. The source image is the [official Ubuntu release build dated 2026-10-17](https://cloud-images.ubuntu.com/releases/noble/release-20261017/), SHA-256 `04e8f2461a16a44e78a370284471f29616cdd9baf1893df25807b71f985b6c94` from its [SHA256SUMS](https://cloud-images.ubuntu.com/releases/noble/release-20261017/SHA256SUMS). The image itself stays inside EVE and is not a deliverable. After resizing the cloud image, its BIOS GRUB entry reached rescue mode; a separate copy with BIOS GRUB reinstalled reached the Ubuntu serial login prompt both in a networkless overlay and through an **actual EVE node 15 start**. The [actual EVE boot transcript](candidate-eve-control-boot.log) captures the prompt. The [serial inventory](candidate-control-inventory.json) records `ens3 10.70.99.2/24`, no default route, 13 GiB free root space and no FreeRADIUS. Node 15 was stopped and its smoke-test disk wiped, leaving a clean install target.

To verify the prepared assets from the authorized workstation:

```powershell
$py = 'D:\AI\.venv-culvcenter\Scripts\python.exe'
$lab = 'D:\AI\nac-wired-lab'
& $py "$lab\eve_guest.py" 'sha256sum /opt/unetlab/labs/NAC-Wired-Qualification.unl /opt/unetlab/labs/NAC-Pilot-Candidate.unl; qemu-img info /opt/unetlab/addons/qemu/linux-nac24-control/virtioa.qcow2 | head -7; test -r /dev/kvm -a -w /dev/kvm && echo KVM_OK'
```

Expected baseline topology hash: `659656f9030ce694ac01347db943f67a87bc56f451ef3789d5d3f94e2200fad9`. Expected candidate topology hash with separate node/bridge IDs and BIOS-repaired control image: `e3407fe0f06ec49769fac5dc3578aa436eaab91d700865cbb5e70838a5a7af8a`.

If rebuilding from an empty EVE image directory, run the following in order. `prepare_noble_image.py` refuses a bad checksum or an existing partial image; inspect a partial file before retrying. `make_candidate_topology.py` refuses to overwrite an existing topology. Generated console credentials stay on EVE and must not be exported.

```powershell
& $py "$lab\eve_guest.py" 'python3 -' --script "$lab\qualification\prepare_noble_image.py"
& $py "$lab\eve_guest.py" 'python3 -' --script "$lab\qualification\prepare_candidate_control.py"
& $py "$lab\eve_guest.py" 'python3 -' --script "$lab\qualification\repair_candidate_bios.py"
& $py "$lab\qualification\make_candidate_topology.py"
& $py "$lab\eve_guest.py" --upload "$lab\qualification\NAC-Pilot-Candidate.unl" '/opt/unetlab/labs/NAC-Pilot-Candidate.unl'
```

The next gate is OPUS's candidate register entry with immutable SHA and annotated `pilot-rc<N>` tag, installer, FreeRADIUS 3.2.5 guide and operator workflows. After that handoff, start the clean candidate topology and record the actual install time, RADIUS/adapter hashes, endpoint admission, policy version/decision/session IDs, product CoA and outage behavior. Only node 15 has undergone a bounded OS boot smoke test; it was stopped and wiped. No candidate product has been installed or qualified.

The clean control node has a static address on its isolated control NIC and **no default route**. Read-only inspection of its `ubuntu.sources` found `http://archive.ubuntu.com/ubuntu/` and `http://security.ubuntu.com/ubuntu/`. A temporary [package proxy](candidate_package_proxy.py) is prepared for the documented installer. It accepts only `10.70.99.2`, binds only the candidate control bridge address `10.70.99.254:3128`, and forwards only listed Ubuntu and Go package hosts. Use it only after stopping baseline nodes and moving `10.70.99.254/24` from `vnet0_4` to candidate bridge `vnet0_14`; verify the bridge names from the EVE API before the move. Set the control guest's `http_proxy` and `https_proxy` to `http://10.70.99.254:3128` only during installation. Stop the proxy, remove the guest proxy settings, and restore the EVE host bridge address afterward. The proxy does not enable IP forwarding and the endpoint has no control NIC. The installer still needs to document its exact package and artifact sources; any source outside this allowlist is an explicit prerequisite to review, not an automatic proxy expansion. No candidate installation has used the proxy yet.

The existing disposable PKI is held inside EVE at `/root/nac-pki/`. Read-only path inspection found `ca.crt`, `server.crt` and `server.key`, which are sufficient for OPUS's installer `--ca`, `--server-cert` and `--server-key` inputs while preserving the already tested endpoint certificate. Copy only those three files to the candidate control node through its serial console after starting it; keep the private key root-only and never put it in the evidence bundle. The CA private key does not need to move. Confirm the fresh candidate endpoint really contains the matching client certificate before any admission test; if its cloned template lacks it, stage the existing `endpoint.crt` and `endpoint.key` via that endpoint's serial console and record the setup action. This certificate staging is a lab prerequisite, not a product workflow result.
