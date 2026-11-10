#!/usr/bin/env bash
# Only for the recoverable node-35 EVE overlay. Keep the existing test CA and DB.
set -euo pipefail
exec > >(tee /root/arista-rc4-overlay-upgrade.log) 2>&1
echo "start_utc=$(date -u +%FT%TZ)"
test "$(hostname)" = nac-pilot-control
test "$(sha256sum /root/pilot-aruba-rc4.tar.gz | cut -d' ' -f1)" = 3520bd152097a7e849b3b134ed2ad4f6ff62f394022a6911913dbe67d3d4cb53
mkdir -p /root/arista-rc4-overlay
tar -xzf /root/pilot-aruba-rc4.tar.gz -C /root/arista-rc4-overlay
bundle=/root/arista-rc4-overlay/nac-pilot-pilot-aruba-rc4
grep -q '^commit: 0148bf88985c5da9086b54aafd002fbfbfe71b75$' "$bundle/BUNDLE-MANIFEST.txt"
test "$(sha256sum "$bundle/bin/nacd" | cut -d' ' -f1)" = dd2b61371585d317bf07684733996c69e265dfdb9e762e354eeca4f7821b49d7
systemctl stop nacd freeradius
install -m 0755 "$bundle/bin/nacd" /usr/local/bin/nacd
install -m 0755 "$bundle/deployments/pilot/nac-reset-password" /usr/local/sbin/nac-reset-password
install -m 0755 "$bundle/deployments/pilot/nac-freeradius-reload" /usr/local/sbin/nac-freeradius-reload
install -m 0644 "$bundle/deployments/pilot/systemd/"*.service "$bundle/deployments/pilot/systemd/"*.path /etc/systemd/system/
runuser -u nac -- bash -c 'set -a; . /etc/nac/nacd.env; exec /usr/local/bin/nacd -migrate'
set -a
. /etc/nac/nacd.env
set +a
NAC_URL=http://127.0.0.1:8080 \
  NAC_CA_FILE=/root/nac-pilot-install/pki/ca.crt \
  NAC_SERVER_CERT=/root/nac-pilot-install/pki/server.crt \
  NAC_SERVER_KEY=/root/nac-pilot-install/pki/server.key \
  NAC_CLIENTS_FILE=/var/lib/nac/freeradius-clients.conf \
  "$bundle/deployments/pilot/freeradius-setup.sh"
systemctl daemon-reload
systemctl enable --now nac-freeradius-reload.path
systemctl restart nacd freeradius
for _ in $(seq 1 30); do curl -fsS http://127.0.0.1:8080/readyz >/dev/null && break; sleep 1; done
curl -fsS http://127.0.0.1:8080/version
systemctl is-active nacd freeradius postgresql nac-freeradius-reload.path
echo "nacd_sha256=$(sha256sum /usr/local/bin/nacd | cut -d' ' -f1)"
echo "adapter_sha256=$(cat /etc/freeradius/3.0/sites-available/nac /etc/freeradius/3.0/mods-available/nac_* | sha256sum | cut -d' ' -f1)"
echo "end_utc=$(date -u +%FT%TZ)"
echo SUCCESS
