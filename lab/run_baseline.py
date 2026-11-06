"""One-command launcher and evidence collector for the preserved EVE baseline.

Requires the existing pinned-host-key eve_guest.py helper and its authorized
Proxmox/EVE credentials. Does not copy any credential or vendor image.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys

HERE=Path(__file__).resolve().parent
LAB=HERE.parent
GUEST=LAB/'eve_guest.py'
RUNNER=HERE/'runner_guest.py'
FILES=('events.jsonl','results.json','metadata.json','nac-decisions.jsonl','endpoint-eapol.pcap','radius.pcap')

def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()

def guest(*args,timeout=600):
    return subprocess.run([sys.executable,str(GUEST),*map(str,args)],text=True,capture_output=True,timeout=timeout)

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--collect-existing',metavar='RUN_ID',help='collect an already completed guest run without rerunning tests')
    args=parser.parse_args()
    if args.collect_existing:
        run_id=args.collect_existing
        transcript='Collected existing guest run; no lab actions performed.\n'
        exit_code=0
    else:
        completed=guest('RUN_MODE=baseline python3 -','--script',RUNNER,timeout=900)
        transcript=completed.stdout+completed.stderr
        exit_code=completed.returncode
        matches=re.findall(r'^RESULT_DIR=/root/nac-qualification/([^\r\n]+)',transcript,re.MULTILINE)
        if not matches:
            print(transcript[-4000:])
            raise SystemExit('runner did not produce a result directory')
        run_id=matches[-1]
    if not re.fullmatch(r'baseline-[0-9]{8}T[0-9]{6}Z',run_id):
        raise SystemExit('invalid run ID')
    dest=HERE/'runs'/run_id
    dest.mkdir(parents=True,exist_ok=False)
    (dest/'launcher-output.txt').write_text(transcript)
    shutil.copy2(RUNNER,dest/'runner_guest.py')
    hashes={}
    for name in FILES:
        local=dest/name
        result=guest('--download',f'/root/nac-qualification/{run_id}/{name}',local,timeout=120)
        if result.returncode:
            raise RuntimeError(f'download failed for {name}: {result.stderr[-500:]}')
        hashes[name]=sha(local)
    summary=json.loads((dest/'results.json').read_text())
    manifest={
        'run_id':run_id,
        'kind':'baseline-regression',
        'runner_sha256':sha(dest/'runner_guest.py'),
        'launcher_exit_code':exit_code,
        'nac_commit':summary['nac_commit'],
        'files_sha256':hashes,
        'results':[{k:r[k] for k in ('test','status','expected','actual')} for r in summary['results']],
    }
    (dest/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
    print(dest)
    for result in summary['results']:
        print(result['status'],result['test'],result['actual'])
    if exit_code:
        raise SystemExit(exit_code)

if __name__=='__main__':
    main()
