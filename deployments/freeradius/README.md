# NAC FreeRADIUS adapter (FreeRADIUS 3.2.5)

The supported RADIUS integration for the pilot. FreeRADIUS validates EAP-TLS
and asks nacd for the authorization decision over HTTP (`rlm_rest`). It
replaces the lab's per-request `exec` adapter (`lab/nac_radius_adapter.py`):
the identity mapping that adapter read from `identity.json` now lives in nacd
as versioned, audited identity bindings.

Supported version: **FreeRADIUS 3.2.5** (Ubuntu 24.04 `freeradius`,
`freeradius-rest`). 3.0.26, ASTRA's earlier lab version, is not supported.

| File (installed under `/etc/freeradius/3.0/`) | Purpose |
|---|---|
| `sites-available/nac` | The only virtual server: EAP-TLS, the NAC post-auth decision, reject recording, accounting |
| `mods-available/nac_rest` | Three `rest` instances: `nac_authorize`, `nac_reject`, `nac_accounting` |
| `mods-available/nac_outage` | `linelog` writing outage rejects as JSON lines (nacd imports them) |
| `mods-available/nac_eap` | EAP with **only** TLS; TLS session cache disabled (no authorization reuse) |
| `mods-available/nac_json` | `json_encode` for the outage log |
| `nac/nac-token.conf` | `password = "<NAC_SERVICE_TOKEN>"` (mode 0640 root:freerad) |
| `nac/nac-url.conf` | `connect_uri = "http://127.0.0.1:8080"` |

Clients come from `/var/lib/nac/freeradius-clients.conf`, which nacd rewrites
on every switch change. `nac-freeradius-reload.path` restarts FreeRADIUS when
that file changes, because FreeRADIUS reads clients only at start.

Outage semantics (no cache): if nacd cannot answer, the post-auth section
rejects with `Reply-Message "NAC decision service unavailable"` and logs a
JSON line to `/var/log/freeradius/nac-outage.log`.
