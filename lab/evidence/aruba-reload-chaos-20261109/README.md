# FreeRADIUS reload-path chaos qualification (isolated rc10 clone)

**Result: FAIL for malformed switch registration; recovery PASS.** This is a
product status and availability defect, independent of the Aruba switch. The
NAC API accepted `::ffff:10.250.99.200` as a RADIUS source and reported
`configured: yes` even though FreeRADIUS rejected the generated client file.
The reload unit failed its configuration check. A forced FreeRADIUS restart
then failed, leaving RADIUS unavailable until that switch was deleted.

## Scope and identity

- Date/time: 2026-11-09, 18:04–18:07 UTC.
- Product: `pilot-rc10`, commit `b39649f138e126ed6689a53b590855061042e341`;
  installed `nacd` SHA-256 `35699eac162cc2a7315efced101bee3ec18ed87ab1cb108f06f9bb1a7b48195e`.
- FreeRADIUS 3.2.5 on Ubuntu 24.04.5 LTS. Reload path unit SHA-256
  `fc1dd669de91bda5d3a4c709f5e61dd702b081c46e43e7cf39dfcb6b03e74495`;
  reload service unit SHA-256
  `f1226bb3f97738d0a9e93ba320aa8e9cd75f4031229311cea73ca73babdef7d7`.
- Test script: OPUS [`597a0c7`](https://github.com/KidCarmi/nac/blob/597a0c7/scripts/chaos-freeradius-reload.sh),
  copied onto the guest with SHA-256
  `f2dcd23a259e17d683cce113b4983ecc726afe129dd8af2e96b4fe9810651534`.
  The script is linked, not included here, because it embeds disposable test
  shared-secret values.
- Infrastructure: copy-on-write child of the frozen rc10 control overlay in
  EVE-NG VM 102. The child had 2 GiB RAM and one QEMU user-mode NIC with a
  loopback-only SSH forward. It was **not attached** to the Aruba lab bridges.
  The parent rc10 overlay and original lab were not modified.

## Expected and observed

| Case | Expected | Observed | Status |
|---|---|---|---|
| C1, one registration | Client file valid, FreeRADIUS active and loaded after last write | All three checks passed after 12 s | PASS |
| C2, eight rapid registrations | Final eight clients loaded; reload path remains active | 8/8 in file; FreeRADIUS active and restarted after last write at +12 s | PASS |
| C3, eight rapid secret rotations | Final value loaded | Final value in file; FreeRADIUS active and restarted after last write at +12 s | PASS |
| C4, mapped IPv4 source | Reject invalid source before writing, or accurately report pending/failed without disabling RADIUS | API accepted it and said `configured: yes`; `freeradius -C` rejected the file, reload service failed. Forced restart failed; deleting the entry restored service | FAIL |
| C5, kill during reload | Running server eventually loads final file; record any interruption | At +12 s in run 1 it was active/current. At +5 s in run 2 it was inactive; at +10 s more, cleanup check found it active/current. `systemctl kill` also emitted an auxiliary-process error; this does not isolate a missed-reload defect | OBSERVED |
| Cleanup | No test switch remains; FreeRADIUS active | `nac-chaos-` count 0; NAC, FreeRADIUS and path unit active | PASS |

The C2/C3 passes describe **final settled state**, not uninterrupted service or
every intermediate write. C5 does not establish a recovery-time guarantee.
The forced C4 restart on this disposable clone demonstrates the RADIUS outage;
no endpoint was authenticated during that outage. This package does not claim
an endpoint probe result.

## Reproduce

On a disposable Ubuntu control node with rc10 installed and a separate NAC
database, run the linked script as root. Set `NAC_URL`, `NAC_EMAIL` and
`NAC_PASSWORD` from local protected sources. Do not point it at a production
node. For the first log use `CHAOS_BURST=8`, `CHAOS_SETTLE_SECONDS=12`. For the
forced-outage log use `CHAOS_BURST=1`, `CHAOS_SETTLE_SECONDS=5`,
`CHAOS_OUTAGE=1`. The script deletes its `chaos-*` entries; verify
`systemctl is-active nacd freeradius nac-freeradius-reload.path` and that the
generated clients file has no `nac-chaos-` entries afterward.

The reload path watches `/var/lib/nac/freeradius-clients.conf`; its one-shot
service runs `freeradius -C -d /etc/freeradius/3.0` and then
`systemctl try-restart freeradius.service`. A status of `configured: yes`
currently means registration/file writing, not confirmed loading by the live
FreeRADIUS process. The operator-facing status should distinguish those facts.

## Files

- `burst-8.log`: eight-registration and eight-rotation run, with malformed
  source and recovery.
- `outage-recovery.log`: single-registration run with a forced FreeRADIUS
  restart while the malformed source exists.
- `sha256-manifest.json`: hashes of the two logs and this report.

No vendor image, test key, admin password, service token or RADIUS shared
secret is in this package.
