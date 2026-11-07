"""Read-only, allowlisted evidence export from a disposable fresh NAC guest.

Run through clean_rc9_vm.py --fresh-ui --script. Never export operator hashes,
RADIUS secrets, eAPI passwords, bearer tokens or private keys.
"""

import json
import subprocess
from datetime import datetime, timezone


QUERIES = {
    'switches': '''SELECT id, name, radius_source_ip, nas_ip, coa_port,
        segment_vlans, access_ports, eos_version, radius_server_ip, eapi_url,
        eapi_username, last_radius_at, last_probe_at, last_probe_ok,
        last_probe_detail, verified_at, verified_detail, verified_session_id
        FROM switches ORDER BY created_at''',
    'endpoints': '''SELECT id, mac, hostname, device_type, posture, created_at
        FROM endpoints ORDER BY created_at''',
    'identity_bindings': '''SELECT id, endpoint_id, issuer, serial, subject_cn,
        revision, created_by, created_at FROM identity_bindings ORDER BY created_at''',
    'policy_versions': '''SELECT id, number, rules, default_action, digest,
        note, created_by, created_at FROM policy_versions ORDER BY number''',
    'policy_activations': '''SELECT id, version_id, kind, previous_version_id,
        activated_by, activated_at, note FROM policy_activations ORDER BY activated_at''',
    'auth_decisions': '''SELECT id, occurred_at, session_id, switch_id, subject,
        policy_version, matched_rule, action, segment, vlan, result, reason
        FROM auth_decisions ORDER BY occurred_at''',
    'radius_sessions': '''SELECT id, switch_id, nas_port_id, mac,
        cert_issuer, cert_serial, cert_cn, endpoint_id, binding_id, state,
        override, override_by, override_at, last_decision_id,
        requested_action, requested_segment, requested_vlan, started_at,
        last_auth_at, ended_at, observed_status, observed_vlan, observed_at,
        observed_source FROM radius_sessions ORDER BY started_at''',
    'coa_requests': '''SELECT id, session_id, switch_id, kind, target,
        requested_segment, requested_vlan, state, attempts, radius_id,
        error_cause, detail, created_by, created_at, sent_at, responded_at,
        observed_at, observed_decision_id FROM coa_requests ORDER BY created_at''',
    'auth_events': '''SELECT id, occurred_at, session_id, switch_id,
        nas_port_id, mac, stage, result, reason, source, decision_id, coa_id,
        detail FROM auth_events ORDER BY occurred_at''',
    'audit_events': '''SELECT id, occurred_at, actor_email, action,
        resource_type, resource_id, outcome FROM audit_events ORDER BY occurred_at''',
    'operators': '''SELECT email, role, disabled, created_at, updated_at,
        token_version, password_changed_at FROM operators ORDER BY created_at''',
}


def fetch(query):
    sql = 'SELECT coalesce(json_agg(row_to_json(t)), \'[]\'::json) FROM (' + query + ') t'
    result = subprocess.run(
        ['sudo', '-u', 'postgres', 'psql', '-X', '-A', '-t', '-d', 'nac', '-c', sql],
        capture_output=True, text=True, check=True, timeout=15,
    )
    return json.loads(result.stdout.strip())


export = {
    'generated_at_utc': datetime.now(timezone.utc).isoformat(),
    'scope': 'isolated fresh rc10 qualification guest',
    'tables': {name: fetch(query) for name, query in QUERIES.items()},
}
print(json.dumps(export, indent=2, sort_keys=True))
