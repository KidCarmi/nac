#!/usr/bin/env bash
# NAC pilot installer: one Ubuntu 24.04 host running PostgreSQL, nacd and
# FreeRADIUS 3.2.5. Idempotent: re-running keeps existing secrets and data.
#
#   sudo ./install.sh --ca ca.pem --server-cert radius.pem --server-key radius.key
#   sudo ./install.sh --test-pki        # LAB ONLY: generate a disposable PKI
#
# Options:
#   --ca FILE            CA (PEM) that issued the endpoint certificates
#   --server-cert FILE   FreeRADIUS server certificate (PEM)
#   --server-key FILE    its unencrypted private key (PEM)
#   --test-pki           generate a disposable test CA/server/client (lab only)
#   --https-cert/--https-key FILE   TLS certificate for the operator console
#                        (default: a self-signed certificate is generated)
#   --admin-email EMAIL  first administrator (default admin@nac.local)
#   --proxy URL          HTTP proxy for apt only (e.g. http://10.70.99.254:3128);
#                        not persisted. Needs only archive.ubuntu.com and
#                        security.ubuntu.com.
#   --no-start           install and configure, but do not start services
#
# Network: with a release bundle this script downloads ONLY Ubuntu packages
# (postgresql, freeradius, freeradius-rest, freeradius-utils, openssl, curl)
# from the host's configured Ubuntu 24.04 archive. nacd is the prebuilt binary
# in the bundle. Exact installed versions are written to
# /etc/nac/install-report.txt.
set -euo pipefail

BUNDLE=$(cd "$(dirname "$0")/../.." && pwd)
CA= SCERT= SKEY= TESTPKI=0 HCERT= HKEY= ADMIN=admin@nac.local START=1 PROXY=
while [ $# -gt 0 ]; do
  case "$1" in
    --ca) CA=$2; shift 2 ;;
    --server-cert) SCERT=$2; shift 2 ;;
    --server-key) SKEY=$2; shift 2 ;;
    --test-pki) TESTPKI=1; shift ;;
    --https-cert) HCERT=$2; shift 2 ;;
    --https-key) HKEY=$2; shift 2 ;;
    --admin-email) ADMIN=$2; shift 2 ;;
    --no-start) START=0; shift ;;
    --proxy) PROXY=$2; shift 2 ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "unknown option $1 (see --help)" >&2; exit 2 ;;
  esac
done

step() { printf '\n==> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
# Random alphanumeric secret of N hex characters (openssl reads all its output
# itself, so no early-closing pipe can trip pipefail).
rand() { openssl rand -hex "$1" | cut -c "1-$1"; }

[ "$(id -u)" = 0 ] || die "run as root (sudo)"
. /etc/os-release
[ "$ID" = ubuntu ] && [ "$VERSION_ID" = "24.04" ] || die "supported OS is Ubuntu 24.04 (found $PRETTY_NAME)"
if [ "$TESTPKI" = 0 ] && { [ -z "$CA" ] || [ -z "$SCERT" ] || [ -z "$SKEY" ]; }; then
  die "certificates required: --ca, --server-cert and --server-key (or --test-pki for a lab)"
fi
for f in "$CA" "$SCERT" "$SKEY"; do [ -z "$f" ] || [ -r "$f" ] || die "cannot read $f"; done
NACD_BIN=""
for c in "$BUNDLE/nacd" "$BUNDLE/bin/nacd"; do [ -x "$c" ] && NACD_BIN=$c && break; done
[ -n "$NACD_BIN" ] || die "nacd binary not found in $BUNDLE (build with 'make build' or use a release bundle)"

step "Installing packages (PostgreSQL, FreeRADIUS 3.2 with rest)"
export DEBIAN_FRONTEND=noninteractive
APT_OPTS=()
if [ -n "$PROXY" ]; then
  APT_OPTS=(-o "Acquire::http::Proxy=$PROXY" -o "Acquire::https::Proxy=$PROXY")
  echo "apt proxy for this run only: $PROXY"
fi
apt-get "${APT_OPTS[@]}" update -qq || die "apt update failed (check the Ubuntu archive or --proxy)"
apt-get "${APT_OPTS[@]}" install -y -qq postgresql freeradius freeradius-rest freeradius-utils openssl curl >/dev/null \
  || die "package installation failed (check the Ubuntu archive or --proxy)"
FRVER=$(freeradius -v | sed -n 's/.*Version \([0-9.]*\).*/\1/p' | head -1)
echo "FreeRADIUS $FRVER"

step "Users and directories"
id nac >/dev/null 2>&1 || useradd --system --home /var/lib/nac --shell /usr/sbin/nologin nac
usermod -aG freerad nac   # read the FreeRADIUS outage log
install -d -m 2750 -o nac -g freerad /var/lib/nac   # setgid: files nacd writes stay readable by FreeRADIUS
install -d -m 0750 -o root -g nac /etc/nac
install -d -m 0755 /opt/nac
cp -r "$BUNDLE/deployments" "$BUNDLE/docs" /opt/nac/ 2>/dev/null || true
install -m 0755 "$NACD_BIN" /usr/local/bin/nacd

step "Secrets (generated once, kept on re-install)"
ENVF=/etc/nac/nacd.env
get() { [ -f "$ENVF" ] || return 0; sed -n "s/^$1=//p" "$ENVF" | sed -n 1p; }
DBPW=$(get NAC_DB_PASSWORD_HINT); DBPW=${DBPW:-$(rand 32)}
JWT=$(get NAC_JWT_SIGNING_KEY); JWT=${JWT:-$(rand 48)}
TOKEN=$(get NAC_SERVICE_TOKEN); TOKEN=${TOKEN:-$(rand 40)}
SKEYS=$(get NAC_SECRETS_KEY); SKEYS=${SKEYS:-$(rand 48)}
ADMINPW_FILE=/root/nac-initial-admin.txt
if [ -f "$ADMINPW_FILE" ]; then ADMINPW=$(sed -n 's/^password: //p' "$ADMINPW_FILE"); else ADMINPW=$(rand 20); fi

step "PostgreSQL database"
systemctl enable --now postgresql >/dev/null 2>&1 || service postgresql start >/dev/null
su postgres -c "psql -qtAc \"SELECT 1 FROM pg_roles WHERE rolname='nac'\"" | grep -q 1 \
  || su postgres -c "psql -qc \"CREATE ROLE nac LOGIN PASSWORD '$DBPW'\""
su postgres -c "psql -qc \"ALTER ROLE nac PASSWORD '$DBPW'\""
su postgres -c "psql -qtAc \"SELECT 1 FROM pg_database WHERE datname='nac'\"" | grep -q 1 \
  || su postgres -c "createdb -O nac nac"

step "Operator console TLS certificate"
if [ -n "$HCERT" ]; then
  install -m 0640 -o root -g nac "$HCERT" /etc/nac/https.pem
  install -m 0640 -o root -g nac "$HKEY" /etc/nac/https.key
elif [ ! -f /etc/nac/https.pem ]; then
  openssl req -x509 -newkey rsa:2048 -nodes -days 825 -subj "/CN=$(hostname -f 2>/dev/null || hostname)" \
    -addext "subjectAltName=DNS:$(hostname),IP:$(hostname -I | awk '{print $1}')" \
    -keyout /etc/nac/https.key -out /etc/nac/https.pem 2>/dev/null
  chgrp nac /etc/nac/https.key /etc/nac/https.pem; chmod 0640 /etc/nac/https.key
  echo "self-signed certificate generated (browsers will warn; supply --https-cert/--https-key to replace it)"
fi

step "nacd configuration ($ENVF)"
umask 027
cat > "$ENVF" <<ENV
# NAC pilot configuration. See docs/operations/configuration.md.
NAC_ENV=production
# Loopback HTTP for FreeRADIUS; HTTPS for operators.
NAC_HTTP_ADDR=127.0.0.1:8080
NAC_HTTPS_ADDR=:8443
NAC_TLS_CERT_FILE=/etc/nac/https.pem
NAC_TLS_KEY_FILE=/etc/nac/https.key
NAC_DB_DSN=postgres://nac:$DBPW@127.0.0.1:5432/nac?sslmode=disable
NAC_DB_PASSWORD_HINT=$DBPW
NAC_JWT_SIGNING_KEY=$JWT
NAC_SERVICE_TOKEN=$TOKEN
NAC_SECRETS_KEY=$SKEYS
NAC_DEFAULT_ACTION=deny
NAC_POSTURE_MAX_AGE=24h
NAC_OBSERVATION_MAX_AGE=5m
NAC_COA_TIMEOUT=3s
NAC_COA_RETRIES=2
NAC_FREERADIUS_CLIENTS_FILE=/var/lib/nac/freeradius-clients.conf
NAC_FREERADIUS_OUTAGE_LOG=/var/log/freeradius/nac-outage.log
NAC_EAPI_INSECURE_SKIP_VERIFY=false
NAC_LOG_FORMAT=json
NAC_LOG_LEVEL=info
NAC_BOOTSTRAP_ADMIN_EMAIL=$ADMIN
NAC_BOOTSTRAP_ADMIN_PASSWORD=$ADMINPW
ENV
chown root:nac "$ENVF"; chmod 0640 "$ENVF"
umask 077
printf 'console: https://%s:8443/\nemail: %s\npassword: %s\n' "$(hostname -I | awk '{print $1}')" "$ADMIN" "$ADMINPW" > "$ADMINPW_FILE"
umask 022

step "Database schema"
runuser -u nac -- bash -c 'set -a; . /etc/nac/nacd.env; exec /usr/local/bin/nacd -migrate' >/dev/null

step "FreeRADIUS adapter"
if [ "$TESTPKI" = 1 ]; then
  [ -f /etc/nac/test-pki/ca.pem ] || "$BUNDLE/scripts/test-pki.sh" /etc/nac/test-pki >/dev/null
  CA=/etc/nac/test-pki/ca.pem SCERT=/etc/nac/test-pki/server.pem SKEY=/etc/nac/test-pki/server.key
  echo "LAB ONLY: disposable test PKI in /etc/nac/test-pki (client.pem/client.key for a test endpoint)"
fi
NAC_SERVICE_TOKEN=$TOKEN NAC_URL=http://127.0.0.1:8080 NAC_CA_FILE=$CA NAC_SERVER_CERT=$SCERT NAC_SERVER_KEY=$SKEY \
  NAC_CLIENTS_FILE=/var/lib/nac/freeradius-clients.conf "$BUNDLE/deployments/pilot/freeradius-setup.sh"
chown nac:freerad /var/lib/nac/freeradius-clients.conf; chmod 0640 /var/lib/nac/freeradius-clients.conf
touch /var/log/freeradius/nac-outage.log; chown freerad:freerad /var/log/freeradius/nac-outage.log; chmod 0640 /var/log/freeradius/nac-outage.log

step "Install report (/etc/nac/install-report.txt)"
{
  echo "installed_at: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "os: $PRETTY_NAME"
  echo "nacd: $(/usr/local/bin/nacd -version 2>/dev/null || echo unknown)"
  echo "nacd_sha256: $(sha256sum /usr/local/bin/nacd | cut -d' ' -f1)"
  [ -f "$BUNDLE/BUNDLE-MANIFEST.txt" ] && sed 's/^/bundle: /' "$BUNDLE/BUNDLE-MANIFEST.txt"
  dpkg-query -W -f='package: ${Package} ${Version}\n' postgresql postgresql-16 freeradius freeradius-rest freeradius-utils freeradius-common openssl 2>/dev/null
  echo "freeradius_adapter_sha256: $(cat /etc/freeradius/3.0/sites-available/nac /etc/freeradius/3.0/mods-available/nac_* | sha256sum | cut -d' ' -f1)"
  echo "pki: $([ "$TESTPKI" = 1 ] && echo 'disposable test PKI (lab only)' || echo "operator-supplied CA $(openssl x509 -in "$CA" -noout -subject -fingerprint -sha256 | tr '\n' ' ')")"
} > /etc/nac/install-report.txt
cat /etc/nac/install-report.txt

step "Services"
install -m 0644 "$BUNDLE/deployments/pilot/systemd/"*.service "$BUNDLE/deployments/pilot/systemd/"*.path /etc/systemd/system/
if [ "$START" = 1 ]; then
  systemctl daemon-reload
  systemctl enable --now nacd.service nac-freeradius-reload.path
  systemctl enable freeradius.service
  systemctl restart freeradius.service
  for _ in $(seq 1 60); do curl -fs http://127.0.0.1:8080/readyz >/dev/null && break; sleep 1; done
  curl -fs http://127.0.0.1:8080/readyz >/dev/null || die "nacd did not become ready: journalctl -u nacd"
  systemctl is-active --quiet freeradius || die "FreeRADIUS is not running: journalctl -u freeradius"
fi

cat <<DONE

NAC pilot installed.
  Console:        https://$(hostname -I | awk '{print $1}'):8443/
  First admin:    see $ADMINPW_FILE (root only); keep it secret (the pilot has no password-change page)
  FreeRADIUS:     $FRVER, UDP 1812/1813; outage log /var/log/freeradius/nac-outage.log
  Configuration:  $ENVF
Next: open the console, register the switch (Switches), apply the generated EOS configuration.
Firewall: allow UDP 1812-1813 from switches, TCP 8443 from operators, UDP 3799 from this host to switches.
DONE
