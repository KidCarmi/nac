# NAC pilot: installation guide

This guide installs the pilot on **one Ubuntu 24.04 host** (the "control
node"). The host runs PostgreSQL, the NAC service (`nacd`) and FreeRADIUS
3.2.5. The pilot scope is:
- one site
- Arista EOS **4.29.2F** access switches (the only version tested)
- wired endpoints that already have certificates (EAP-TLS)
- two segments, corp and quarantine, with configurable VLAN IDs

Read [known limitations](known-limitations.md) before installing.

## 0. What this guide was tested with

These are the setups the lab actually ran (ASTRA, EVE-NG, vEOS-lab
4.29.2F). Anything else is untested; see
[qualification status](qualification-status.md).

| Workflow | Tested as | Candidate |
|---|---|---|
| Clean install | Fresh Ubuntu 24.04.5 guest (2 vCPU, 2 GB RAM, 12 GB disk), checksum-verified bundle, `install.sh --test-pki`, packages from the Ubuntu archive. About 44–63 s from extraction to running services | `pilot-rc7`, `pilot-rc9`, `pilot-rc10` |
| In-place upgrade | Installer re-run over the previous candidate: settings kept, nacd restarted, running build checked | `pilot-rc7`, `pilot-rc8`, `pilot-rc9` |
| Reboot / full restart | All services come back automatically. Sessions, policy and switches persist. The switch keeps only what was saved with `write memory` | `pilot-rc8`, `pilot-rc9`, `pilot-rc10` |
| nacd outage | New logins rejected (fail closed), existing switch sessions kept, automatic recovery | `pilot-rc7`, `pilot-rc8`, `pilot-rc10` |
| Password change and forgotten password | API change and `nac-reset-password` (clean `pilot-rc9` instance); console Account page (`pilot-rc10`) | `pilot-rc9`, `pilot-rc10` |
| Operator certificates | Lab disposable PKI only (`--test-pki` on the clean `pilot-rc9` install). Your own CA (`--ca`) follows section 5 and has not been lab-run | — |
| eAPI TLS | Section 7a, verified TLS (`SSL_CERT_FILE`, TLS 1.2 / ECDHE, SAN = switch IP) on vEOS-lab 4.29.2F. A wrong SAN is rejected | clean `pilot-rc9` instance, `pilot-rc10` |
| Switch onboarding, binding, policy, CoA in the console | A clean instance with a real vEOS switch: register switch, bind the EAP-TLS certificate, preview, publish, corp admission, Quarantine and Restore | clean `pilot-rc9` instance; the whole workflow, Account included, on one fresh `pilot-rc10` instance ([`af46f02`](https://github.com/KidCarmi/nac/tree/af46f02eb1f9f5f3bb07ef6ac441f12b6840465d/lab/evidence/clean-ui-rc10-20261107)) |
| Endpoints | One-NIC Linux and Windows Server 2025 Desktop Experience (machine EAP-TLS, DHCP) | `pilot-rc10` |

## 1. What you need

| Item | Detail |
|---|---|
| Control node | Ubuntu 24.04 LTS (24.04.5 tested), 2 vCPU, 2 GB RAM, 10 GB disk, root access |
| Release bundle | `nac-pilot-<version>.tar.gz` and its `.sha256` file |
| Certificates | The CA that issued your endpoint certificates (PEM), plus a FreeRADIUS server certificate and unencrypted key issued for this host. For a lab only, use `--test-pki` instead. |
| Switch | Arista EOS 4.29.2F with a routed interface that can reach the control node, and **ZeroTouch Provisioning cancelled** (otherwise `write memory` fails and the configuration is lost on reboot; see the walkthrough, step 1) |
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

On a workstation with Go and the repository, check out the candidate commit
named in the release notes, then build with an **explicit** version:

```bash
git checkout <candidate-commit>
```

```bash
make pilot-bundle VERSION=<candidate-name>
```

The output is `dist/nac-pilot-<version>.tar.gz` plus `.sha256`.

- `nacd` is reproducible: the same commit and `VERSION` always give the same
  SHA-256 (pinned go1.25.0, `-trimpath`, build date taken from the commit).
  Compare it with the `nacd_sha256` in `BUNDLE-MANIFEST.txt` and the release
  notes.
- Without `VERSION`, the version comes from `git describe` and depends on the
  tags in your clone, so the hash differs.
- The tarball's own hash differs between builds (archive timestamps). Verify
  the tarball with the `.sha256` produced next to it, and verify `nacd` by its
  hash.

Building from source on the control node instead needs `golang-go` from
Ubuntu, a copy of the source, and access to `proxy.golang.org` and
`sum.golang.org`. go.mod requires Go 1.25, so the Ubuntu Go 1.22 downloads
the go1.25 toolchain from `proxy.golang.org` (`GOTOOLCHAIN=auto`).

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
database and your configuration: the settings in `/etc/nac/nacd.env` keep
their current values, and lines you added are carried over. It restarts nacd
and FreeRADIUS, and fails unless the **running** nacd reports the commit of
the installed binary (recorded as `running nacd:` in the install report).

> Candidates up to `pilot-rc6` do not restart a running nacd on re-install.
> After installing one of them over an existing installation, run
> `sudo systemctl restart nacd`, then check that
> `curl -s http://127.0.0.1:8080/version` shows the candidate's commit. It finishes by printing the console address.

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
   - `nac-freeradius-reload.path`, which runs
     `/usr/local/sbin/nac-freeradius-reload` when nacd rewrites the switch
     list. The helper checks the configuration with `freeradius -C`,
     restarts FreeRADIUS, and records the outcome in
     `/var/lib/nac/freeradius-reload.status`. nacd shows a switch as
     **Configured** only from that record, while a `freeradius` process is
     running.
   - `freeradius.service`
9. Writes `/etc/nac/install-report.txt`.

## 4. First sign-in

The address, email and generated password are in `/root/nac-initial-admin.txt`, which only root can read:

```bash
sudo cat /root/nac-initial-admin.txt
```

Open the console at `https://<control-node>:8443/` and sign in. With the
self-signed certificate, the browser warns once.

**Change the password right away:** open **Account** (your email, top
right), enter the current password and a new one (at least 12 characters),
and save. This signs out every other session of the account. Then delete the
initial password file:

```bash
sudo rm /root/nac-initial-admin.txt
```

`NAC_BOOTSTRAP_ADMIN_PASSWORD` in `/etc/nac/nacd.env` is only used to create
the account on first start. Restarts and re-running the installer do not
reset a changed password.

**Forgotten password:** on the control node, as root, run the command below.
It asks for the new password twice without showing it, signs out every
session of that account, and records the reset in the audit log.

```bash
sudo nac-reset-password <admin-email>
```

The pilot has one administrator account. Operator management (more accounts,
roles) is not in the pilot; see [known limitations](known-limitations.md).

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
cat /var/lib/nac/freeradius-reload.status
```

After you start FreeRADIUS by hand (or after it was stopped while a switch
changed), the console shows "Loaded by FreeRADIUS: no" until the next reload.
Record the load with:

```bash
systemctl start nac-freeradius-reload.service
```

If FreeRADIUS runs on another host than nacd, set
`NAC_FREERADIUS_PROCESS_CHECK=off` in `/etc/nac/nacd.env`. That deployment is
not part of the pilot.

```bash
curl -fs http://127.0.0.1:8080/readyz
```

```bash
sudo cat /etc/nac/install-report.txt
```

The console's **Overview** shows the RADIUS integration status. On a fresh
install, "FreeRADIUS client file" reads "0 switch(es) written".

## 7. Restart and recovery

The lab run on `fe7c981` measured these
([qualification status](qualification-status.md)):
- the documented install took about 63 s
- after a full restart of all nodes, valid login, quarantine and restore
  worked again
- after a nacd outage, the endpoint's automatic retry restored access about
  30 s after nacd came back

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

## 7a. Optional: session observation (eAPI and accounting)

Without either, NAC knows what it *requested* for a session but not whether
the endpoint is still connected. The session view then shows observation
**unavailable** and liveness **unknown**. Both sources are optional and
independent.

> eAPI observation and RADIUS accounting passed on `pilot-rc7` in the lab
> (vEOS-lab 4.29.2F; see [qualification status](qualification-status.md)).
> Verified eAPI TLS (step 2, setup A) passed on vEOS-lab 4.29.2F.

**eAPI (switch state, polled every 30 s):**

1. On the switch, create a read-only account and enable eAPI. If the address
   NAC reaches is in a VRF, add `vrf <name>` under
   `management api http-commands`. Then `write memory`.

   ```
   username nac-observer role network-operator secret <PASSWORD>
   management api http-commands
      no shutdown
   ```

2. **TLS.** Choose one of the two setups below. Both settings are global
   for all switches, re-running the installer keeps them, and nacd must be
   restarted after a change (command below).

   **A. Verified TLS (recommended).** nacd checks the switch certificate
   chain and that the certificate names the exact host in the eAPI URL.
   - nacd's eAPI client uses the trust store given by `SSL_CERT_FILE`.
     eAPI is nacd's only outbound TLS connection, so that file is the
     complete set of CAs nacd trusts.
   - On the build environment, with the `pilot-rc9` server code, a valid
     chain with a matching IP SAN gives *Reachable* **yes**. An unknown CA
     or a wrong SAN gives **no**, and the detail says why.
   - **Lab-validated on vEOS-lab 4.29.2F** (ASTRA): with this setup,
     Probe, Refresh and Verify succeeded (switch SUCCESS, VLAN 10). A
     certificate from the same CA with a wrong IP SAN was rejected with an
     x509 hostname mismatch.

   1. Issue the switch's eAPI certificate from a CA you control. The SAN
      must contain the host used in the eAPI URL, for example
      `IP:10.70.99.1`, with extended key usage serverAuth. On the control
      node, with OpenSSL, set `IP` and `SWITCH` first:

      ```bash
      openssl req -x509 -newkey rsa:2048 -nodes -keyout eapi-ca.key -out eapi-ca.pem -days 3650 -subj "/CN=NAC eAPI CA" -addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign"
      ```

      ```bash
      openssl req -newkey rsa:2048 -nodes -keyout "$SWITCH-eapi.key" -out "$SWITCH-eapi.csr" -subj "/CN=$SWITCH"
      ```

      ```bash
      openssl x509 -req -in "$SWITCH-eapi.csr" -CA eapi-ca.pem -CAkey eapi-ca.key -CAcreateserial -days 825 -out "$SWITCH-eapi.pem" -extfile <(printf 'subjectAltName=IP:%s\nextendedKeyUsage=serverAuth\n' "$IP")
      ```

      Keep `eapi-ca.key` offline. It is not needed on the control node.
   2. Copy `$SWITCH-eapi.pem` and `$SWITCH-eapi.key` to the switch's
      `flash:` as `nac-eapi.crt` and `nac-eapi.key` (for example with `scp`).
      Then **import** them into the switch's certificate and key stores. EOS
      4.29.2F requires this step: the SSL profile refers only to bare names
      in `certificate:` and `sslkey:`.

      ```
      copy flash:nac-eapi.crt certificate:nac-eapi.crt
      copy flash:nac-eapi.key sslkey:nac-eapi.key
      ```

      Use them in an SSL profile with ECDHE ciphers for eAPI:

      ```
      management security
         ssl profile nac-eapi
            tls versions 1.2
            cipher-list ECDHE-RSA-AES256-GCM-SHA384:ECDHE-RSA-AES128-GCM-SHA256
            certificate nac-eapi.crt key nac-eapi.key
      management api http-commands
         protocol https ssl profile nac-eapi
      ```

      Check it with `show management security ssl profile nac-eapi`
      (state *valid*), then `write memory`.
   3. On the control node:

      ```bash
      sudo install -m 0640 -o root -g nac eapi-ca.pem /etc/nac/eapi-ca.pem
      ```

      In `/etc/nac/nacd.env` set:

      ```
      SSL_CERT_FILE=/etc/nac/eapi-ca.pem
      NAC_EAPI_INSECURE_SKIP_VERIFY=false
      NAC_EAPI_TLS_ALLOW_RSA_KEX=false
      ```

      Then restart nacd.

   **B. Unverified TLS (lab or isolated management network only).**
   - Stock EOS 4.29.2F eAPI uses a self-signed certificate and offers only
     TLS 1.2 **RSA key-exchange** ciphers (for example `AES256-SHA`), which
     nacd does not offer by default. Without a fix, **Probe** reports a TLS
     handshake failure that names the fix.
   - Set `NAC_EAPI_INSECURE_SKIP_VERIFY=true` and
     `NAC_EAPI_TLS_ALLOW_RSA_KEX=true`. Both affect the eAPI client
     **only**, and ECDHE stays preferred.
   - The switch's identity is **not** checked, and RSA key exchange has no
     forward secrecy. Anyone on the management path can impersonate the
     switch to nacd and read the eAPI password.
   - Do **not** use `GODEBUG=tlsrsakex=1`. It widens TLS for every outbound
     connection of nacd.

3. In the console, open **Switches → (switch) → Edit**, set the eAPI URL
   (for example `https://10.70.99.1`), username and password, and save. Click
   **Probe**: *Reachable* should turn **yes**. **Refresh session observation**
   polls immediately.

```bash
sudo systemctl restart nacd
```

Observations older than `NAC_OBSERVATION_MAX_AGE` (default 5 minutes) are
shown as **stale**. A port/MAC that the switch no longer lists is shown as
**absent**.

**RADIUS accounting (session end):**

1. On the switch:

   ```
   aaa accounting dot1x default start-stop group radius
   ```

2. Nothing changes on the control node: FreeRADIUS already listens on UDP
   1813 with the same switch secret and forwards accounting to nacd.

How sessions are matched:
- Access-Accept carries `Class = nac:<decision id>`, which the switch echoes
  in accounting.
- A Start binds the switch's `Acct-Session-Id`, and a Stop ends the session
  (liveness **ended**).
- A packet whose Class or session id belongs to an earlier connection is
  ignored, and the session timeline says why.
- Accounting NAC cannot attribute (no `NAS-Port-Id`, a `Calling-Station-Id`
  that is not a MAC, or no NAC session for that port and MAC) is also
  ignored, and shown under **Events** (stage `accounting`) with the reason.
  If sessions never end, look there first.

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
| Read the first admin password from `/root/nac-initial-admin.txt`, then delete the file after changing the password in the console | Shell, once | It is never shown over the network |
| Reset a forgotten password with `sudo nac-reset-password <email>` | Shell, when needed | Recovery must not be possible over the network |
| Enable eAPI and RADIUS accounting on the switch. For verified eAPI TLS, issue and import the switch certificate and set `SSL_CERT_FILE` (section 7a) | Switch CLI, `/etc/nac/nacd.env` | Optional session observation |
| Cancel ZeroTouch Provisioning on a factory-fresh switch, apply the generated EOS configuration, `write memory` | Switch CLI | NAC does not write switch configuration |
| Renew the endpoint's IP address after a VLAN change, if its DHCP client does not | Endpoint | See known limitations |
| Replace certificates or enable a CRL | Shell (section 5) | Not in the console in the pilot |

Everything else is done in the console:
- registering switches and VLAN mappings
- binding identities
- troubleshooting
- policy changes
- quarantine and restore
