"""Print same-clock endpoint TCP packets around each CoA request."""
from datetime import datetime, timedelta
from pathlib import Path
import re
import subprocess

root = Path('/root/nac-qualification/arista-rc4-20261110')
actions = {
    'policy-quarantine': '2026-11-10 08:31:32.095767',
    'policy-restore': '2026-11-10 08:33:42.713748',
    'coa-quarantine': '2026-11-10 08:44:31.010589',
    'coa-restore': '2026-11-10 08:46:11.318649',
}
for phase, stamp in actions.items():
    when = datetime.fromisoformat(stamp)
    output = subprocess.run(['tcpdump', '-tttt', '-nn', '-r',
        str(root / f'{phase}-endpoint.pcap'), 'tcp port 8080'],
        text=True, capture_output=True, check=True).stdout
    packets = []
    for line in output.splitlines():
        try:
            seen = datetime.fromisoformat(line[:26])
        except ValueError:
            continue
        packets.append((seen, line))
    old = '10.70.10.10' if 'quarantine' in phase else '10.70.20.10'
    new = '10.70.20.10' if 'quarantine' in phase else '10.70.10.10'
    before = [p for p in packets if p[0] < when and
              f'{old}.8080 >' in p[1] and 'HTTP/1.0 200 OK' in p[1]]
    after = [p for p in packets if p[0] >= when]
    old_syn = [p for p in after if f'> {old}.8080:' in p[1] and 'Flags [S]' in p[1]]
    old_response = [p for p in after if f'{old}.8080 >' in p[1] and 'Flags [S.]' in p[1]]
    old_http_after = [p for p in after if f'{old}.8080 >' in p[1] and 'HTTP/1.0 200 OK' in p[1]]
    unanswered = []
    for packet in old_syn:
        match = re.search(r'IP (\d+\.\d+\.\d+\.\d+\.\d+) > ', packet[1])
        if match and not any(f'{old}.8080 > {match.group(1)}:' in reply[1]
                             for reply in old_response):
            unanswered.append(packet)
    new_success = [p for p in after if f'{new}.8080 >' in p[1] and 'HTTP/1.0 200 OK' in p[1]]
    def show(p):
        return None if not p else {'time': p[0].isoformat(),
            'seconds_from_coa': round((p[0] - when).total_seconds(), 3),
            'packet': p[1][:150]}
    print('PHASE', phase, 'old', old, 'new', new)
    print('last_old_http_200', show(before[-1] if before else None))
    print('first_old_syn_after', show(old_syn[0] if old_syn else None))
    print('last_old_http_200_after', show(old_http_after[-1] if old_http_after else None))
    print('first_unanswered_old_syn', show(unanswered[0] if unanswered else None))
    print('old_synack_after_count', len(old_response))
    print('first_new_http_200', show(new_success[0] if new_success else None))
