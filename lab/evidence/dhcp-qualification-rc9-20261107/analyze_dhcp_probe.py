"""Summarize endpoint-only DHCP/VLAN transition evidence without inferring success."""
import argparse
from datetime import datetime
import json
from pathlib import Path

def instant(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00'))

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('log',type=Path)
    parser.add_argument('--action-at',required=True,help='UTC CoA timestamp in ISO 8601')
    parser.add_argument('--before',default='corp',choices=['corp','quarantine'])
    parser.add_argument('--after',default='quarantine',choices=['corp','quarantine'])
    args=parser.parse_args()
    action=instant(args.action_at)
    events=[json.loads(line) for line in args.log.read_text().splitlines() if line.strip()]
    states=[item for item in events if item['event']=='network_state']
    probes={name:[item for item in events if item['event']=='tcp_probe' and item['segment']==name]
            for name in ('corp','quarantine')}
    def first(items,pred):
        return next((item for item in items if instant(item['at_utc'])>=action and pred(item)),None)
    denial=first(probes[args.before],lambda x:not x['connected'])
    permit=first(probes[args.after],lambda x:x['connected'])
    lease=first(states,lambda x:any(addr.startswith('10.70.'+('20' if args.after=='quarantine' else '10')+'.') for addr in x['addresses']))
    result={'log':str(args.log),'action_at':action.isoformat(),
            'initial_state':next((s for s in states if instant(s['at_utc'])<=action),None),
            'denied_at':denial['at_utc'] if denial else None,
            'denied_after_s':round((instant(denial['at_utc'])-action).total_seconds(),3) if denial else None,
            'new_lease_at':lease['at_utc'] if lease else None,
            'new_lease_after_s':round((instant(lease['at_utc'])-action).total_seconds(),3) if lease else None,
            'permitted_at':permit['at_utc'] if permit else None,
            'permitted_after_s':round((instant(permit['at_utc'])-action).total_seconds(),3) if permit else None,
            'address_states':states,
            'held_connections':[x for x in events if x['event'].startswith('held_')],
            'final_probe':{name:items[-1] if items else None for name,items in probes.items()}}
    print(json.dumps(result,indent=2,sort_keys=True))

if __name__=='__main__': main()
