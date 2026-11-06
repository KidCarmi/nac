# Separate pilot candidate lab preparation

The preserved baseline remains in `NAC-Wired-Qualification.unl`. The candidate topology is [NAC-Pilot-Candidate.unl](NAC-Pilot-Candidate.unl), uploaded inside EVE at `/opt/unetlab/labs/NAC-Pilot-Candidate.unl`. It has separate lab/node UUIDs, **node IDs 11–15** and **bridge IDs 11–14**; EVE reports all candidate nodes stopped after a bounded control-node boot check. The node/bridge IDs are separate from baseline IDs 1–5/1–4 so EVE status and runtime files cannot collide. Both labs use the same `10.70.*` address plan, so stop baseline nodes before assigning the EVE host's control address to the candidate bridge. Do not alter the Proxmox bridge or attach an endpoint management NIC.

The candidate control image is a separate clean Ubuntu 24.04.5 template, `/opt/unetlab/addons/qemu/linux-nac24-control-bios/virtioa.qcow2`, with a 16 GiB virtual disk and 14 GiB root filesystem. It has one isolated control NIC and a root-only console credential stored on EVE at `/root/nac-pilot-control.rootpw`. It contains no installed NAC or FreeRADIUS. The source image is the [official Ubuntu release build dated 2026-10-17](https://cloud-images.ubuntu.com/releases/noble/release-20261017/), SHA-256 `04e8f2461a16a44e78a370284471f29616cdd9baf1893df25807b71f985b6c94` from its [SHA256SUMS](https://cloud-images.ubuntu.com/releases/noble/release-20261017/SHA256SUMS). The image itself stays inside EVE and is not a deliverable. After resizing the cloud image, its BIOS GRUB entry reached rescue mode; a separate copy with BIOS GRUB reinstalled reached the Ubuntu serial login prompt both in a networkless overlay and through an **actual EVE node 15 start**. The [actual EVE boot transcript](candidate-eve-control-boot.log) captures the prompt. Node 15 was stopped and its smoke-test disk wiped, leaving a clean install target.

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

The clean control node has a static address on its isolated control NIC and **no default route**. The candidate installation guide must specify how FreeRADIUS 3.2.5 and NAC dependencies reach that node through a lab-scoped package source or controlled HTTP proxy on the control bridge. No endpoint management NIC or EVE IP forwarding is part of the qualification setup. This is an installation prerequisite, not a product test result.
