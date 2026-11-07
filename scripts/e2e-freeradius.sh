#!/usr/bin/env bash
# End-to-end test of the supported RADIUS adapter on one host:
#   eapol_test (EAP-TLS supplicant acting as the switch) -> FreeRADIUS 3.2.5
#   (deployments/freeradius) -> nacd -> PostgreSQL.
#
# Proves: unbound identity denied; binding grants corp VLAN; untrusted-CA
# certificate rejected by EAP-TLS and diagnosable in NAC; nacd outage gives
# Access-Reject with no cache and an imported outage-log entry; recovery.
# CoA needs a real switch and is covered by test/integration (fake vEOS).
#
# Requires: root, freeradius 3.2.5 + freeradius-rest, eapoltest, PostgreSQL,
# jq, a built ./bin/nacd. Usage: sudo scripts/e2e-freeradius.sh
set -euo pipefail
cd "$(dirname "$0")/.."

DSN=${NAC_E2E_DB_DSN:-postgres://nac:nac@localhost:5432/nac_e2e?sslmode=disable}
WORK=${NAC_E2E_WORK:-$(mktemp -d)}
TOKEN=e2e-service-token-0123456789abcdef
SECRET=e2e-radius-secret-0123456789
API=http://127.0.0.1:18080
CLIENTS=/var/lib/nac/freeradius-clients.conf
OUTAGE=/var/log/freeradius/nac-outage.log
pass=0
pause() { python3 -c "import time; time.sleep($1)"; }
ok() { pass=$((pass + 1)); echo "PASS $pass: $*"; }
fail() { echo "FAIL: $*" >&2; echo "--- nacd log"; tail -20 "$WORK/nacd.log" >&2; echo "--- freeradius log"; tail -40 "$WORK/radiusd.log" >&2; exit 1; }

cleanup() {
  [ -n "${NACD_PID:-}" ] && kill "$NACD_PID" 2>/dev/null || true
  [ -n "${RAD_PID:-}" ] && kill "$RAD_PID" 2>/dev/null || true
}
trap cleanup EXIT

echo "work dir: $WORK"
scripts/test-pki.sh "$WORK/pki" >/dev/null
SERIAL=$(openssl x509 -in "$WORK/pki/client.pem" -noout -serial | cut -d= -f2 | sed "s/^0*//")  # nacd normalises: no leading zeros

# Fresh database = clean installation.
psql "${DSN%/*}/postgres?sslmode=disable" -qc "DROP DATABASE IF EXISTS nac_e2e" -c "CREATE DATABASE nac_e2e" >/dev/null

start_nacd() {
  NAC_DB_DSN=$DSN NAC_HTTP_ADDR=127.0.0.1:18080 NAC_SERVICE_TOKEN=$TOKEN \
  NAC_SECRETS_KEY=e2e-secrets-key-0123456789abcdefghij NAC_LOG_FORMAT=text \
  NAC_BOOTSTRAP_ADMIN_EMAIL=admin@nac.local NAC_BOOTSTRAP_ADMIN_PASSWORD='ChangeMe123!-e2e' \
  NAC_FREERADIUS_CLIENTS_FILE=$CLIENTS NAC_FREERADIUS_OUTAGE_LOG=$OUTAGE \
    ./bin/nacd >>"$WORK/nacd.log" 2>&1 &
  NACD_PID=$!
  for _ in $(seq 1 50); do curl -fs "$API/readyz" >/dev/null 2>&1 && return 0; pause 0.2; done
  fail "nacd did not become ready"
}
login() {
  TOK=$(curl -fs -XPOST "$API/api/v1/auth/login" -d '{"email":"admin@nac.local","password":"ChangeMe123!-e2e"}' | jq -r .token)
}
api() { curl -fsS -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' "$@"; }

cat >"$WORK/eap-good.conf" <<EOF
network={
	key_mgmt=IEEE8021X
	eap=TLS
	identity="wired-endpoint-01"
	ca_cert="$WORK/pki/ca.pem"
	client_cert="$WORK/pki/client.pem"
	private_key="$WORK/pki/client.key"
	eapol_flags=0
}
EOF
sed -e "s#client.pem#rogue.pem#; s#client.key#rogue.key#" "$WORK/eap-good.conf" >"$WORK/eap-rogue.conf"

eap() { # conf -> 0 on SUCCESS
  eapol_test -c "$1" -a 127.0.0.1 -p 1812 -s "$SECRET" -M 00:50:00:00:02:00 -N87:s:Ethernet1 -t 8 >"$WORK/eapol-$2.log" 2>&1
}

# ---- Install the adapter and start the services ----
mkdir -p /var/lib/nac && rm -f "$CLIENTS" "$OUTAGE"
NAC_SERVICE_TOKEN=$TOKEN NAC_URL=$API NAC_CA_FILE="$WORK/pki/ca.pem" NAC_SERVER_CERT="$WORK/pki/server.pem" \
  NAC_SERVER_KEY="$WORK/pki/server.key" deployments/pilot/freeradius-setup.sh >/dev/null
start_nacd; login
ok "clean install: nacd ready, admin bootstrapped"

SW=$(api -XPOST "$API/api/v1/switches" -d "{\"name\":\"e2e-switch\",\"radius_source_ip\":\"127.0.0.1\",\"radius_secret\":\"$SECRET\",
  \"segment_vlans\":{\"corp\":10,\"quarantine\":20},\"access_ports\":[\"Ethernet1\"],\"radius_server_ip\":\"127.0.0.1\"}" | jq -r .id)
grep -q "ipaddr = 127.0.0.1" "$CLIENTS" || fail "nacd did not render the FreeRADIUS client file"
ok "switch registered; FreeRADIUS client file rendered by nacd"

freeradius -X -d /etc/freeradius/3.0 >"$WORK/radiusd.log" 2>&1 &
RAD_PID=$!
for _ in $(seq 1 50); do grep -q "Ready to process requests" "$WORK/radiusd.log" && break; pause 0.2; done
grep -q "Ready to process requests" "$WORK/radiusd.log" || fail "FreeRADIUS did not start"

api -XPOST "$API/api/v1/policies" -d '{"name":"corp-bound-devices","priority":10,"enabled":true,"action":"allow","segment":"corp",
  "conditions":[{"field":"identity.bound","operator":"eq","values":["true"]},{"field":"mac.matches_device","operator":"eq","values":["true"]}]}' >/dev/null
DIGEST=$(api "$API/api/v1/policy-draft/preview" | jq -r .draft_digest)
api -XPOST "$API/api/v1/policy-draft/publish" -d "{\"expected_digest\":\"$DIGEST\",\"note\":\"e2e\"}" >/dev/null
ok "policy version 2 published via preview digest"

# ---- Valid certificate, not bound: EAP-TLS succeeds, NAC denies ----
if eap "$WORK/eap-good.conf" unbound; then fail "unbound identity must be denied"; fi
SESS=$(api "$API/api/v1/sessions" | jq -r '.sessions[0].id')
[ "$(api "$API/api/v1/sessions/$SESS" | jq -r .cert_serial)" = "$SERIAL" ] || fail "session did not record the validated certificate serial"
api "$API/api/v1/sessions/$SESS" | jq -e '.last_decision.inputs["identity.bound"].value == "false"' >/dev/null || fail "identity.bound should be false"
ok "valid but unbound certificate: Access-Reject, session shows serial $SERIAL and identity.bound=false"

# ---- Bind and authenticate: corp VLAN 10 ----
EP=$(api -XPOST "$API/api/v1/endpoints" -d '{"mac":"00:50:00:00:02:00","hostname":"wired-endpoint-01","device_type":"laptop"}' | jq -r .id)
api -XPOST "$API/api/v1/sessions/$SESS/bind" -d "{\"endpoint_id\":\"$EP\"}" >/dev/null
eap "$WORK/eap-good.conf" bound || fail "bound identity should authenticate"
grep -q 'Tunnel-Private-Group-Id = "10"' "$WORK/radiusd.log" || fail "Access-Accept did not carry VLAN 10"
api "$API/api/v1/sessions/$SESS" | jq -e '.state == "active" and .requested.vlan == 10 and .last_decision.policy_version == 2' >/dev/null \
  || fail "session not active on VLAN 10"
ok "bound certificate: Access-Accept with Tunnel-Private-Group-Id 10, decision on policy version 2"

# ---- MAB (no EAP) with the BOUND endpoint's MAC: rejected before nacd ----
# A switch falling back to MAB (dot1x + mac-auth) sends the MAC as
# User-Name/User-Password. MACs are spoofable, so the pilot must never grant
# access on one: FreeRADIUS rejects it in authorize and nacd is not asked.
NSESS=$(api "$API/api/v1/sessions" | jq '.sessions | length')
LASTDEC=$(api "$API/api/v1/sessions/$SESS" | jq -r .last_decision.id)
printf '%s\n' 'User-Name = "005000000200"' 'User-Password = "005000000200"' 'Service-Type = Call-Check' \
  'Calling-Station-Id = "00-50-00-00-02-00"' 'NAS-IP-Address = 127.0.0.1' 'NAS-Port-Type = Ethernet' 'NAS-Port-Id = "Ethernet1"' \
  | radclient -x -r 1 -t 3 127.0.0.1:1812 auth "$SECRET" >"$WORK/mab.log" 2>&1 || true
grep -q 'Received Access-Reject' "$WORK/mab.log" || { cat "$WORK/mab.log"; fail "MAB request must be rejected"; }
if grep -q 'Tunnel-Private-Group-Id' "$WORK/mab.log"; then fail "MAB reject must carry no VLAN"; fi
[ "$(api "$API/api/v1/sessions" | jq '.sessions | length')" = "$NSESS" ] || fail "MAB request created a NAC session"
[ "$(api "$API/api/v1/sessions/$SESS" | jq -r .last_decision.id)" = "$LASTDEC" ] || fail "MAB request reached the NAC decision path"
ok "MAB request with the bound endpoint's MAC: Access-Reject, no VLAN, no NAC session or decision"

# ---- Untrusted CA: rejected by EAP-TLS, explained in NAC ----
if eap "$WORK/eap-rogue.conf" rogue; then fail "untrusted certificate must be rejected"; fi
pause 0.5
REASON=$(api "$API/api/v1/auth-events?stage=eap-tls" | jq -r '.events[0].reason')
echo "      reason: $REASON"
case "$REASON" in *"not issued by the CA"*|*"unknown ca"*|*"Unknown CA"*|*"verify"*) ;; *) fail "rejected certificate not diagnosable: $REASON";; esac
ok "untrusted-CA certificate: Access-Reject, reason recorded by NAC"

# ---- Outage: nacd down -> Access-Reject, no cache, outage log ----
kill "$NACD_PID"; wait "$NACD_PID" 2>/dev/null || true; NACD_PID=
if eap "$WORK/eap-good.conf" outage; then fail "login during outage must be rejected (no cache)"; fi
grep -q '"event":"outage-reject"' "$OUTAGE" || fail "FreeRADIUS did not write the outage log"
grep -q 'NAC decision service unavailable' "$WORK/radiusd.log" || fail "outage Reply-Message missing"
ok "nacd down: Access-Reject 'NAC decision service unavailable', outage line written"

start_nacd; login
for _ in $(seq 1 50); do
  N=$(api "$API/api/v1/auth-events?stage=outage" | jq '.events | length')
  [ "$N" -ge 1 ] && break; pause 0.2
done
api "$API/api/v1/auth-events?stage=outage" | jq -e --arg sw "$SW" '.events[0] | .source == "freeradius-outage-log" and .mac == "00:50:00:00:02:00" and .switch_id == $sw and .nas_port_id == "Ethernet1"' >/dev/null \
  || { api "$API/api/v1/auth-events?stage=outage" | jq '.events[0]'; fail "outage not imported with switch, port and MAC"; }
eap "$WORK/eap-good.conf" recovered || fail "login after recovery should succeed"
ok "nacd recovered: outage reject imported (source freeradius-outage-log); login succeeds again"

api -XPOST "$API/api/v1/switches/$SW/verify" | jq -e '.status.verified.state == "yes"' >/dev/null || fail "verify"
ok "switch verified (decision-only: no eAPI in this harness)"
echo "ALL $pass CHECKS PASSED (FreeRADIUS $(freeradius -v | sed -n 's/.*Version \([0-9.]*\).*/\1/p' | head -1))"
