"""Measure rc10 TCP and address transitions against each product CoA timestamp."""
import json
from datetime import datetime
from pathlib import Path
import re

root=Path(__file__).resolve().parent/'runs/candidate-b39649f-dhcp-rc10-core-20261107'
events=[json.loads(line) for line in (root/'endpoint-probe.jsonl').read_text().splitlines()]
def at(s): return datetime.fromisoformat(s.replace('Z','+00:00'))
actions=[
    ('operator_quarantine','quarantine-action.txt','corp','quarantine'),
    ('operator_restore','restore-action.txt','quarantine','corp'),
    ('rule_only_quarantine','reauth.txt','corp','quarantine'),
    ('rule_only_restore','restore-reauth.txt','quarantine','corp'),
]
records=[]
for name,filename,old,new in actions:
    raw=(root/filename).read_text()
    payload=json.loads(re.search(r'COA (\{[^\n]+\})',raw).group(1))
    records.append((name,at(payload['created_at']),old,new))

def first(start,end,segment,connected):
    matches=[e for e in events if e.get('event')=='tcp_probe' and e.get('segment')==segment
             and e.get('connected') is connected and start<=at(e['at_utc'])<end]
    if not matches:return None
    item=min(matches,key=lambda e:at(e['at_utc']))
    return {'at_utc':item['at_utc'],'seconds_after_action':round((at(item['at_utc'])-start).total_seconds(),3)}

out={'candidate':'b39649f138e126ed6689a53b590855061042e341',
     'observation_timeout_seconds':600,
     'probe_interval_note':'fresh TCP connections sampled sequentially',
     'transitions':[],'network_states':[e for e in events if e.get('event')=='network_state']}
for index,(name,start,old,new) in enumerate(records):
    end=records[index+1][1] if index+1<len(records) else max(at(e['at_utc']) for e in events)
    out['transitions'].append({'name':name,'action_at_utc':start.isoformat(),
        'old_segment':old,'new_segment':new,
        'old_denied':first(start,end,old,False),
        'new_reachable':first(start,end,new,True)})
target=root/'probe-summary.json'
target.write_text(json.dumps(out,indent=2)+'\n')
print(target)
for item in out['transitions']:
    print(item['name'],'old_denied_s',item['old_denied']['seconds_after_action'] if item['old_denied'] else None,
          'new_reachable_s',item['new_reachable']['seconds_after_action'] if item['new_reachable'] else None)
