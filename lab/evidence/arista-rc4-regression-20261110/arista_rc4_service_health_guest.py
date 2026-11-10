"""Probe each Arista DHCP service through its own isolated EVE bridge."""

import json
import subprocess
import urllib.request


def run(*args):
    return subprocess.run(args, check=True, text=True, capture_output=True).stdout.strip()


if run('sysctl', '-n', 'net.ipv4.ip_forward') != '0':
    raise SystemExit('EVE forwarding unexpectedly enabled')

results = {}
for name, bridge, target, address in (
    ('corp', 'vnet0_32', '10.70.10.10', '10.70.10.254/24'),
    ('quarantine', 'vnet0_33', '10.70.20.10', '10.70.20.254/24'),
):
    namespace = 'astra-rc4-health'
    host = 'astra-rc4-h'
    peer = 'astra-rc4-p'
    run('ip', 'netns', 'add', namespace)
    try:
        run('ip', 'link', 'add', host, 'type', 'veth', 'peer', 'name', peer)
        try:
            run('ip', 'link', 'set', host, 'master', bridge)
            run('ip', 'link', 'set', host, 'up')
            run('ip', 'link', 'set', peer, 'netns', namespace)
            run('ip', '-n', namespace, 'addr', 'add', address, 'dev', peer)
            run('ip', '-n', namespace, 'link', 'set', peer, 'up')
            result = subprocess.run(['ip', 'netns', 'exec', namespace, 'curl', '-fsS',
                '--max-time', '5', '-o', '/dev/null', '-w', '%{http_code}',
                f'http://{target}:8080/'], capture_output=True, text=True)
            results[name] = {'bridge': bridge, 'target': target,
                             'http_status': result.stdout.strip(), 'curl_exit': result.returncode}
        finally:
            run('ip', 'link', 'delete', host)
    finally:
        run('ip', 'netns', 'delete', namespace)

print(json.dumps({'results': results, 'forwarding': 0}, sort_keys=True))
if any(x['http_status'] != '200' or x['curl_exit'] != 0 for x in results.values()):
    raise SystemExit('service health failed')
