#!/usr/bin/env bash
# chaos-freeradius-reload.sh: chaos test for the switch → FreeRADIUS reload path
# (nacd writes /var/lib/nac/freeradius-clients.conf, nac-freeradius-reload.path
# triggers nac-freeradius-reload.service, which runs `freeradius -C` and then
# `systemctl try-restart freeradius`).
#
# RUN ONLY ON A NON-PRODUCTION CONTROL NODE.
#   - It registers, updates and deletes switches named chaos-*.
#   - It restarts FreeRADIUS many times; RADIUS is briefly unavailable each time.
#   - With CHAOS_OUTAGE=1 it deliberately makes FreeRADIUS fail to start.
#
# Usage (root, on the control node):
#   NAC_URL=https://127.0.0.1:8443 NAC_EMAIL=admin@nac.local NAC_PASSWORD=... \
#     scripts/chaos-freeradius-reload.sh
#
# Needs curl, jq, systemctl, stat. Each scenario prints PASS / FAIL / OBSERVED
# and the evidence it used.
#
# From pilot-rc12 nacd reports what FreeRADIUS loaded (status.freeradius_load,
# also in /api/v1/radius/health). After every scenario the script compares it
# with the ground truth below: "yes" while not loaded is a FAIL (known defect
# D1). Against rc10/rc11, which do not report it, the comparison is OBSERVED.
#
# "Loaded" means all of these hold:
#   - freeradius.service is active
#   - it (re)started after the clients file was last written
#   - the file passes `freeradius -C`
# FreeRADIUS reads clients only at start, so this is the observable proof
# that the running server has the current switch list.
set -u

NAC="${NAC_URL:-https://127.0.0.1:8443}"
CLIENTS="${NAC_CLIENTS_FILE:-/var/lib/nac/freeradius-clients.conf}"
SETTLE="${CHAOS_SETTLE_SECONDS:-20}"     # fixed waits (C4, C6)
DEADLINE="${CHAOS_DEADLINE_SECONDS:-60}" # max time for a change to be loaded
BURST="${CHAOS_BURST:-8}"
NET="${CHAOS_NET:-10.250.99}" # test switch source IPs: $NET.10..$NET.(10+BURST)
: "${NAC_EMAIL:?set NAC_EMAIL}" "${NAC_PASSWORD:?set NAC_PASSWORD}"

pass=0; fail=0
say() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*"; }
result() { # result PASS|FAIL|OBSERVED <scenario> <detail>
	case "$1" in PASS) pass=$((pass + 1)) ;; FAIL) fail=$((fail + 1)) ;; esac
	say "$1 [$2] $3"
}

TOK=$(curl -sk "$NAC/api/v1/auth/login" -H 'Content-Type: application/json' \
	-d "$(jq -n --arg e "$NAC_EMAIL" --arg p "$NAC_PASSWORD" '{email:$e,password:$p}')" | jq -r .token)
[ -n "$TOK" ] && [ "$TOK" != null ] || { echo "login failed"; exit 2; }
api() { curl -sk -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' "$@"; }

create_switch() { # create_switch <name> <source-ip> -> prints id (or the error body)
	api -X POST "$NAC/api/v1/switches" -d "$(jq -n --arg n "$1" --arg ip "$2" \
		'{name:$n, radius_source_ip:$ip, radius_secret:"chaos-secret-0123456789",
		  segment_vlans:{corp:10, quarantine:20}, access_ports:["Ethernet1"],
		  radius_server_ip:"10.250.0.1"}')" | jq -r '.id // .'
}
update_secret() { # update_secret <id> <secret>
	local cur
	cur=$(api "$NAC/api/v1/switches/$1")
	api -X PUT "$NAC/api/v1/switches/$1" -d "$(echo "$cur" | jq --arg s "$2" \
		'{name, radius_source_ip, nas_ip, radius_secret:$s, coa_port, segment_vlans,
		  access_ports, radius_server_ip, eapi_url:(.eapi_url // ""), eapi_username:(.eapi_username // ""),
		  eapi_password:"", clear_eapi:false}')" >/dev/null
}
delete_switch() { api -X DELETE "$NAC/api/v1/switches/$1" -o /dev/null; }
configured_state() { api "$NAC/api/v1/switches/$1" | jq -r '.status.configured.state + " (" + .status.configured.detail + ")"'; }
nacd_load() { api "$NAC/api/v1/radius/health" | jq -r '.freeradius_load.state // "not-reported"'; }
check_nacd_agrees() { # check_nacd_agrees <scenario>: nacd's claim vs the ground truth
	local claim truth
	claim=$(nacd_load)
	loaded && truth=yes || truth=no
	case "$claim/$truth" in
	not-reported/*) result OBSERVED "$1 nacd" "nacd does not report freeradius_load (pre-rc12); ground truth loaded=$truth" ;;
	yes/no) result FAIL "$1 nacd" "nacd says loaded=yes but FreeRADIUS does NOT have the current list (D1)" ;;
	yes/yes) result PASS "$1 nacd" "nacd says loaded=yes, and it is" ;;
	*/yes) result FAIL "$1 nacd" "nacd says $claim but FreeRADIUS has the current list: $(api "$NAC/api/v1/radius/health" | jq -r .freeradius_load.detail)" ;;
	*) result PASS "$1 nacd" "nacd says $claim, and FreeRADIUS does not have the current list: $(api "$NAC/api/v1/radius/health" | jq -r .freeradius_load.detail)" ;;
	esac
}

fr_active() { systemctl is-active --quiet freeradius; }
# Ground truth, in nanoseconds and by content (ASTRA, pilot-aruba-rc2 retest:
# whole-second timestamps marked same-second cases as loaded). "Loaded" now
# needs ALL of:
#   - FreeRADIUS active
#   - its main process started after the clients file was last written
#     (systemd microsecond timestamp vs nanosecond file mtime)
#   - the reload record says ok for exactly the SHA-256 of the current file
#   - the file passes freeradius -C
RELOAD_STATUS="${NAC_RELOAD_STATUS:-/var/lib/nac/freeradius-reload.status}"
fr_started_ns() {
	local ts
	ts=$(systemctl show --timestamp=us+utc -p ExecMainStartTimestamp --value freeradius 2>/dev/null)
	[ -n "$ts" ] || ts=$(systemctl show -p ExecMainStartTimestamp --value freeradius)
	date -d "$ts" +%s%N 2>/dev/null || echo 0
}
file_ns() { date -r "$CLIENTS" +%s%N; }
file_sha() { sha256sum "$CLIENTS" | cut -d' ' -f1; }
record_field() { sed -n "s/^$1=//p" "$RELOAD_STATUS" 2>/dev/null; }
file_valid() { freeradius -C -d /etc/freeradius/3.0 >/dev/null 2>&1; }
loaded() {
	fr_active && [ "$(fr_started_ns)" -gt "$(file_ns)" ] &&
		[ "$(record_field result)" = ok ] && [ "$(record_field clients_sha256)" = "$(file_sha)" ] && file_valid
}
truth_detail() {
	printf 'active=%s fr_start_ns=%s file_ns=%s file_sha=%.12s record=%s/%.12s@%s valid=%s' \
		"$(fr_active && echo yes || echo no)" "$(fr_started_ns)" "$(file_ns)" "$(file_sha)" \
		"$(record_field result)" "$(record_field clients_sha256)" "$(record_field at)" "$(file_valid && echo yes || echo no)"
}
units_state() {
	printf 'path=%s/%s service=%s/%s restarts_fr=%s' \
		"$(systemctl show nac-freeradius-reload.path -p ActiveState --value)" \
		"$(systemctl show nac-freeradius-reload.path -p Result --value)" \
		"$(systemctl show nac-freeradius-reload.service -p ActiveState --value)" \
		"$(systemctl show nac-freeradius-reload.service -p Result --value)" \
		"$(systemctl show freeradius -p NRestarts --value)"
}
check_loaded() { # check_loaded <scenario>: poll until loaded, up to DEADLINE seconds
	local t0 waited
	t0=$(date +%s)
	until loaded; do
		waited=$(($(date +%s) - t0))
		if [ "$waited" -ge "$DEADLINE" ]; then
			result FAIL "$1" "FreeRADIUS does NOT have the current clients file after ${waited}s ($(truth_detail)); $(units_state)"
			journalctl -u nac-freeradius-reload.service -u nac-freeradius-reload.path -u freeradius -u nacd --since "-3min" --no-pager | tail -25
			check_nacd_agrees "$1"
			return
		fi
		sleep 1
	done
	result PASS "$1" "loaded after $(($(date +%s) - t0))s: FreeRADIUS restarted after the last write with exactly the current list ($(truth_detail)); $(units_state)"
	check_nacd_agrees "$1"
}

say "baseline: $(units_state)"
loaded && result PASS baseline "FreeRADIUS has the current clients file" || result FAIL baseline "not loaded before starting; fix that first"

# C1: one switch registered.
id1=$(create_switch chaos-one "$NET.9")
check_loaded "C1 single register"
say "C1 nacd says configured: $(configured_state "$id1")"

# C2: a burst of registrations with no pause (bulk onboarding, for example by an AI client).
ids=()
for i in $(seq 1 "$BURST"); do ids+=("$(create_switch "chaos-b$i" "$NET.$((9 + i))")"); done
check_loaded "C2 burst of $BURST registrations"
n=$(grep -c '^client nac-chaos-b' "$CLIENTS"); [ "$n" -eq "$BURST" ] && result PASS "C2 file" "$n/$BURST in clients file" || result FAIL "C2 file" "$n/$BURST in clients file"

# C3: a burst of secret rotations on one switch (risk: unit start limits / coalesced triggers).
for i in $(seq 1 "$BURST"); do update_secret "$id1" "chaos-rotated-secret-$i-abcdef"; done
check_loaded "C3 burst of $BURST secret rotations"
grep -A3 'client nac-chaos-one' "$CLIENTS" | grep -q "chaos-rotated-secret-$BURST-abcdef" \
	&& result PASS "C3 file" "latest secret written" || result FAIL "C3 file" "latest secret not in file"

# C4: an input nacd accepts but FreeRADIUS rejects (IPv4-mapped IPv6 spelling).
bad=$(create_switch chaos-mapped "::ffff:$NET.200")
if [[ "$bad" =~ ^[0-9a-f-]{36}$ ]]; then
	sleep "$SETTLE"
	result OBSERVED "C4 accepted" "nacd accepted ::ffff:$NET.200; nacd says configured: $(configured_state "$bad")"
	file_valid && result PASS "C4 file" "clients file still valid" || result FAIL "C4 file" "clients file now fails freeradius -C; FreeRADIUS kept the OLD list and will FAIL TO START at its next restart"
	loaded || result FAIL "C4 loaded" "running FreeRADIUS no longer matches nacd; $(units_state)"
	if [ "${CHAOS_OUTAGE:-0}" = 1 ]; then
		systemctl restart freeradius; sleep 8
		fr_active && result PASS "C4 outage" "FreeRADIUS restarted" || result FAIL "C4 outage" "FreeRADIUS does not start while the bad switch exists: RADIUS outage"
	fi
	delete_switch "$bad"
	sleep 10 # Restart=on-failure (5 s) or the reload path must bring FreeRADIUS back
	check_loaded "C4 recovery after deleting the bad switch"
else
	result PASS "C4 rejected" "nacd rejected ::ffff:$NET.200: $bad"
fi
# C4b: the hex spelling of the same address (loads, but as an IPv6 client).
IFS=. read -r o1 o2 o3 <<<"$NET"
hex=$(printf '::ffff:%x:%x' $(((o1 << 8) | o2)) $(((o3 << 8) | 200)))
bad=$(create_switch chaos-mapped-hex "$hex")
if [[ "$bad" =~ ^[0-9a-f-]{36}$ ]]; then
	result FAIL "C4b accepted" "nacd accepted $hex (FreeRADIUS treats it as an IPv6 client)"
	delete_switch "$bad"
else
	result PASS "C4b rejected" "nacd rejected $hex: $bad"
fi

# C5: FreeRADIUS killed right after a change (reload racing a crash).
update_secret "$id1" "chaos-after-kill-secret-abcdef"
sleep 0.3; systemctl kill -s KILL freeradius
check_loaded "C5 kill during reload"

# C6: FreeRADIUS deliberately stopped while a switch changes. The reload must
# not start it, and nacd must not claim the list is loaded. After a start and
# one reload, nacd must say yes again.
systemctl stop freeradius
update_secret "$id1" "chaos-while-stopped-secret-abcdef"
sleep "$SETTLE"
fr_active && result FAIL "C6 stopped" "the reload started a deliberately stopped FreeRADIUS" || result PASS "C6 stopped" "FreeRADIUS stayed stopped"
check_nacd_agrees "C6 stopped"
systemctl start freeradius
systemctl start nac-freeradius-reload.service # records the load for nacd
check_loaded "C6 restarted"

# Cleanup.
for i in "${ids[@]}" "$id1"; do [[ "$i" =~ ^[0-9a-f-]{36}$ ]] && delete_switch "$i"; done
check_loaded "cleanup"
grep -q 'nac-chaos-' "$CLIENTS" && result FAIL cleanup "chaos switches still in clients file" || result PASS cleanup "no chaos switches left"

say "done: $pass PASS, $fail FAIL"
[ "$fail" -eq 0 ]
