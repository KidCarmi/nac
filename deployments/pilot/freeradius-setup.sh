#!/usr/bin/env bash
# Installs the NAC FreeRADIUS adapter into an Ubuntu 24.04 FreeRADIUS 3.2.5.
# Idempotent. Called by install.sh; can be re-run after an upgrade.
#
# Environment:
#   NAC_SERVICE_TOKEN   (required) token FreeRADIUS presents to nacd
#   NAC_URL             default http://127.0.0.1:8080
#   NAC_CA_FILE         CA that issued endpoint certificates (PEM)
#   NAC_SERVER_CERT     FreeRADIUS server certificate (PEM)
#   NAC_SERVER_KEY      its private key (PEM, unencrypted)
#   NAC_CLIENTS_FILE    default /var/lib/nac/freeradius-clients.conf
set -euo pipefail
here=$(cd "$(dirname "$0")/../freeradius" && pwd)
conf=/etc/freeradius/3.0
: "${NAC_SERVICE_TOKEN:?NAC_SERVICE_TOKEN is required}"
NAC_URL=${NAC_URL:-http://127.0.0.1:8080}
NAC_CLIENTS_FILE=${NAC_CLIENTS_FILE:-/var/lib/nac/freeradius-clients.conf}

ver=$(freeradius -v 2>/dev/null | sed -n 's/.*Version \([0-9.]*\).*/\1/p' | head -1)
if [ "${ver%.*}" != "3.2" ]; then
  echo "freeradius-setup: FreeRADIUS $ver found; the NAC adapter supports 3.2.5 (Ubuntu 24.04). Aborting." >&2
  exit 1
fi

install -d -m 0750 -o root -g freerad "$conf/nac" "$conf/nac/certs" "$conf/nac/certs/capath"
for f in nac_rest nac_outage nac_eap nac_json; do
  install -m 0640 -o root -g freerad "$here/mods-available/$f" "$conf/mods-available/$f"
  ln -sfn "../mods-available/$f" "$conf/mods-enabled/$f"
done
install -m 0640 -o root -g freerad "$here/sites-available/nac" "$conf/sites-available/nac"
ln -sfn ../sites-available/nac "$conf/sites-enabled/nac"

# The stock eap/json modules and default/inner-tunnel servers would conflict
# (duplicate module names, listeners on 1812/1813).
rm -f "$conf/mods-enabled/eap" "$conf/mods-enabled/json" \
      "$conf/sites-enabled/default" "$conf/sites-enabled/inner-tunnel"

printf 'connect_uri = "%s"\n' "$NAC_URL" > "$conf/nac/nac-url.conf"
umask 027
printf 'password = "%s"\n' "$NAC_SERVICE_TOKEN" > "$conf/nac/nac-token.conf"
chown root:freerad "$conf/nac/nac-url.conf" "$conf/nac/nac-token.conf"
chmod 0640 "$conf/nac/nac-token.conf"

if [ -n "${NAC_CA_FILE:-}" ]; then
  install -m 0640 -o root -g freerad "$NAC_CA_FILE" "$conf/nac/certs/ca.pem"
  install -m 0640 -o root -g freerad "$NAC_SERVER_CERT" "$conf/nac/certs/server.pem"
  install -m 0640 -o root -g freerad "$NAC_SERVER_KEY" "$conf/nac/certs/server.key"
fi

# Clients rendered by nacd; FreeRADIUS needs the file to exist to start.
install -d -m 0750 "$(dirname "$NAC_CLIENTS_FILE")"
[ -f "$NAC_CLIENTS_FILE" ] || install -m 0640 /dev/null "$NAC_CLIENTS_FILE"
# Replace the stock clients.conf: its localhost client uses the well-known
# secret "testing123". Only switches registered in nacd are RADIUS clients.
if ! grep -q "NAC-CLIENTS" "$conf/clients.conf"; then
  cp -p "$conf/clients.conf" "$conf/clients.conf.pre-nac"
fi
cat > "$conf/clients.conf" <<CLIENTS
# NAC-CLIENTS: managed by deployments/pilot/freeradius-setup.sh.
# Switches are registered in nacd (UI: Switches), which renders the file below.
# The original file is saved as clients.conf.pre-nac.
\$INCLUDE $NAC_CLIENTS_FILE
CLIENTS
chown root:freerad "$conf/clients.conf"; chmod 0640 "$conf/clients.conf"

freeradius -C -d "$conf" >/dev/null && echo "freeradius-setup: configuration OK (FreeRADIUS $ver)"
