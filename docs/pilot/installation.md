# NAC pilot: installation guide

This guide installs the pilot on **one Ubuntu 24.04 host** (the "control
node"). The host runs PostgreSQL, the NAC service (`nacd`) and FreeRADIUS
3.2.5. The pilot scope is:
- one site
- Arista EOS **4.29.2F** access switches (the only version tested)
- wired endpoints that already have certificates (EAP-TLS)
- two segments, corp and quarantine, with configurable VLAN IDs

Read [known limitations](known-limitations.md) before installing.

## 1. What you need

| Item | Detail |
|---|---|
| Control node | Ubuntu 24.04 LTS (24.04.5 tested), 2 vCPU, 2 GB RAM, 10 GB disk, root access |
| Release bundle | `nac-pilot-<version>.tar.gz` and its `.sha256` file |
| Certificates | The CA that issued your endpoint certificates (PEM), plus a FreeRADIUS server certificate and unencrypted key issued for this host. For a lab only, use `--test-pki` instead. |
| Switch | Arista EOS 4.29.2F with a routed interface that can reach the control node |
| Package source | The Ubuntu 24.04 archive (`archive.ubuntu.com`, `security.ubuntu.com`), directly or through an HTTP proxy |

### Network flows

| From | To | Port | Purpose |
|---|---|---|---|
| Switch | Control node | UDP 1812, 1813 | RADIUS authentication and accounting |
| Control node | Switch | UDP 3799 | CoA (dynamic authorization) |
| Operator browser | Control node | TCP 8443 | Console and API (HTTPS) |
| Control node | Switch | TCP 443 | Optional: eAPI session observation |

nacd's plain-HTTP listener is bound to `127.0.0.1:8080` and is only for FreeRADIUS on the same host.

## 2. Exact sources

| Artifact | Source |
|---|---|
| `nacd` | Prebuilt `linux/amd64` binary inside the bundle (`bin/nacd`). Its SHA-256 is in `BUNDLE-MANIFEST.txt`. |
| FreeRADIUS adapter, installer, docs | Inside the bundle (`deployments/`, `docs/`, `scripts/`) |
| `postgresql`, `freeradius`, `freeradius-rest`, `freeradius-utils`, `openssl`, `curl` | Ubuntu 24.04 archive, through `apt` |

The installer downloads nothing else. Versions validated for this candidate:

| Package | Version |
|---|---|
| freeradius, freeradius-rest, freeradius-utils, freeradius-common | `3.2.5+dfsg-3~ubuntu24.04.3` |
| postgresql-16 | `16.15-0ubuntu0.24.04.1` |
| openssl | `3.0.13-0ubuntu3.16` |

The installer records the versions it actually installed in
`/etc/nac/install-report.txt`.

### Building the bundle

On a workstation with Go and the repository:

```bash
git checkout pilot-rc1
```

```bash
make pilot-bundle
```

The output is `dist/nac-pilot-<version>.tar.gz` plus `.sha256`. Building from
source on the control node instead needs `golang-go` from Ubuntu, a copy of the
source, and access to `proxy.golang.org` and `sum.golang.org`. go.mod requires
Go 1.25, so the Ubuntu Go 1.22 downloads the go1.25 toolchain from
`proxy.golang.org` (`GOTOOLCHAIN=auto`).

## 3. Install

Copy the bundle and its checksum to the control node, for example with `scp`. Then verify and unpack it:

```bash
sha256sum -c nac-pilot-<version>.tar.gz.sha256
```

```bash
tar xzf nac-pilot-<version>.tar.gz && cd nac-pilot-<version>
```

With your own CA and server certificate:

```bash
sudo deployments/pilot/install.sh --ca /path/ca.pem --server-cert /path/radius.pem --server-key /path/radius.key
```

Through an HTTP proxy, for example the pilot lab's package proxy. The proxy is used by `apt` for this run only and is not saved:

```bash
sudo deployments/pilot/install.sh --proxy http://10.70.99.254:3128 --ca /path/ca.pem --server-cert /path/radius.pem --server-key /path/radius.key
```

Lab only, with a disposable test PKI. The endpoint test certificate is written to `/etc/nac/test-pki/client.pem` and `client.key`:

```bash
sudo deployments/pilot/install.sh --test-pki
```

The installer is idempotent. Re-running it keeps the generated secrets, the
database and your configuration. It finishes by printing the console address.

### What the installer does

1. Checks the OS (Ubuntu 24.04), the bundle and the certificates.
2. Installs the Ubuntu packages, optionally through `--proxy`.
3. Creates the system user `nac`. It also creates `/var/lib/nac`, a setgid
   directory shared with FreeRADIUS, and `/etc/nac`.
4. Generates random secrets: database password, JWT key, FreeRADIUS service
   token, the key that encrypts switch secrets, and the first admin password.
   They are kept on re-runs.
5. Creates the PostgreSQL role and database, and applies the schema.
6. Creates the operator HTTPS certificate. It is self-signed unless you pass
   `--https-cert`/`--https-key`.
7. Installs the FreeRADIUS adapter (`deployments/pilot/freeradius-setup.sh`):
   - EAP-TLS only, with your CA
   - the stock `clients.conf` replaced (its localhost client uses the public
     secret `testing123`); the original is saved as `clients.conf.pre-nac`
   - the configuration checked with `freeradius -C`
8. Installs and starts the systemd units:
   - `nacd.service`
   - `nac-freeradius-reload.path`, which restarts FreeRADIUS when nacd
     rewrites the switch list
   - `freeradius.service`
9. Writes `/etc/nac/install-report.txt`.

## 4. First sign-in

The address, email and generated password are in `/root/nac-initial-admin.txt`, which only root can read:

```bash
sudo cat /root/nac-initial-admin.txt
```

Open the console at `https://<control-node>:8443/` and sign in. With the
self-signed certificate, the browser warns once.

**The password cannot be changed in the pilot.** There is no password-change
page or API, and only this one administrator account exists (see
[known limitations](known-limitations.md)). Keep the file root-only. To
replace the password:
1. Set a new `NAC_BOOTSTRAP_ADMIN_PASSWORD` in `/etc/nac/nacd.env`.
2. Delete the account row in PostgreSQL.
3. Restart nacd, which re-creates the account.

That is a shell procedure, not a product workflow.

Then follow the [operator walkthrough](operator-walkthrough.md).

## 5. Certificates

| File on the control node | Purpose |
|---|---|
| `/etc/freeradius/3.0/nac/certs/ca.pem` | CA that issued endpoint certificates. Only certificates chaining to it pass EAP-TLS. |
| `/etc/freeradius/3.0/nac/certs/server.pem`, `server.key` | Certificate FreeRADIUS presents to endpoints. Endpoints must trust its issuer. |
| `/etc/nac/https.pem`, `https.key` | Console HTTPS |

**Lab validation with the existing test CA:** pass the lab's test CA
certificate as `--ca`, and a server certificate/key issued by that CA as
`--server-cert`/`--server-key`. The CA private key is never needed on the
control node. Endpoints keep their existing certificates. With the pilot lab's
disposable PKI (PEM files with `.crt` names are fine):

```bash
sudo deployments/pilot/install.sh --proxy http://10.70.99.254:3128 --ca /root/nac-pki/ca.crt --server-cert /root/nac-pki/server.crt --server-key /root/nac-pki/server.key
```

**Your own CA:** pass your issuing CA (or the chain, concatenated PEM) as
`--ca`, and a server certificate from a CA your endpoints trust. To replace
certificates later, run this, then restart FreeRADIUS:

```bash
sudo NAC_SERVICE_TOKEN="$(sudo sed -n 's/^NAC_SERVICE_TOKEN=//p' /etc/nac/nacd.env)" NAC_CA_FILE=/path/ca.pem NAC_SERVER_CERT=/path/radius.pem NAC_SERVER_KEY=/path/radius.key /opt/nac/deployments/pilot/freeradius-setup.sh
```

```bash
sudo systemctl restart freeradius
```

**Revocation:** set `check_crl = yes` in
`/etc/freeradius/3.0/mods-available/nac_eap`, put the CA's CRL (PEM) in
`/etc/freeradius/3.0/nac/certs/capath`, run `c_rehash` on that directory, and
restart FreeRADIUS. Removing a device's identity binding in the console also
denies it at its next authentication, without a CRL.

**Serial numbers:** NAC shows certificate serials in upper-case hex without
leading zeros. `openssl x509 -serial` may show a leading `0`.

## 6. Checks after installation

```bash
systemctl status nacd freeradius nac-freeradius-reload.path
```

```bash
curl -fs http://127.0.0.1:8080/readyz
```

```bash
sudo cat /etc/nac/install-report.txt
```

The console's **Overview** shows the RADIUS integration status. On a fresh
install, "FreeRADIUS client file" reads "0 switch(es) written".

## 7. Restart and recovery

All state lives in PostgreSQL: switches, bindings, sessions, decisions,
policy versions and CoA history. A restart of nacd, FreeRADIUS or the host
loses nothing. If nacd restarts during a CoA exchange, that CoA is marked
`failed` with an explanation; retry it from the session page.

While nacd is down, FreeRADIUS rejects new logins and re-authentications with
"NAC decision service unavailable", and existing switch sessions stay up
(see [known limitations](known-limitations.md)). After recovery, nacd imports
those rejects into the timeline.

Back up the database (`pg_dump nac`) and `/etc/nac` (it contains the secrets,
including `NAC_SECRETS_KEY`, without which stored switch secrets cannot be
decrypted).

## 8. Upgrading from the lab baseline (FreeRADIUS 3.0.26)

The candidate does **not** run on FreeRADIUS 3.0.26, and `freeradius-setup.sh`
refuses it. Install the candidate on a clean Ubuntu 24.04 node (FreeRADIUS
3.2.5). Then point the switch's `radius-server host` at the new node, using
the configuration the console generates. Nothing from the lab adapter
(`exec nac_lab`, `identity.json`) is reused: identity mappings become identity
bindings in the console.

## 9. Manual steps that remain

| Step | Who / where | Why it is manual |
|---|---|---|
| Copy the bundle and run `install.sh` | Shell on the control node, once | Initial setup |
| Read the first admin password from `/root/nac-initial-admin.txt` | Shell, once | It is never shown over the network |
| Apply the generated EOS configuration | Switch CLI | NAC does not write switch configuration |
| Renew the endpoint's IP address after a VLAN change, if its DHCP client does not | Endpoint | See known limitations |
| Replace certificates or enable a CRL | Shell (section 5) | Not in the console in the pilot |

Everything else is done in the console:
- registering switches and VLAN mappings
- binding identities
- troubleshooting
- policy changes
- quarantine and restore
