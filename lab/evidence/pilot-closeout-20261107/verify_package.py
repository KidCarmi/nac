"""Verify sanitized NAC closeout package hashes, links and excluded files."""
from pathlib import Path
import argparse
import hashlib
import json
import re

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('directory',type=Path)
    parser.add_argument('--write',action='store_true')
    args=parser.parse_args()
    root=args.directory.resolve()
    if root.name!='pilot-closeout-20261107': parser.error('wrong closeout package')
    manifest=root/'sha256-manifest.json'
    files=[]
    for path in sorted(root.rglob('*')):
        if path.is_file() and path!=manifest:
            data=path.read_bytes()
            files.append({'path':path.relative_to(root).as_posix(),
                          'bytes':len(data),'sha256':hashlib.sha256(data).hexdigest()})
    if args.write: manifest.write_text(json.dumps({'files':files},indent=2)+'\n')
    if files!=json.loads(manifest.read_text())['files']:
        raise SystemExit('SHA-256 manifest mismatch')
    forbidden_suffix={'.key','.pfx','.qcow2','.vmdk','.iso','.bundle','.tar','.gz'}
    forbidden=[item['path'] for item in files if Path(item['path']).suffix.lower() in forbidden_suffix
               or re.search(r'(secret|password|private)',item['path'],re.I)]
    if forbidden: raise SystemExit('forbidden package filename: '+', '.join(forbidden))
    broken=[]
    for path in root.rglob('*.md'):
        for target in re.findall(r'\]\(([^)]+)\)',path.read_text(encoding='utf-8')):
            if target.startswith(('http://','https://','#')):continue
            relative=target.split('#',1)[0]
            if relative and not (path.parent/relative).exists():
                broken.append(str(path.relative_to(root))+' -> '+target)
    if broken: raise SystemExit('broken local links:\n'+'\n'.join(broken))
    for item in files:
        path=root/item['path']
        if re.search(rb'(?m)^-----BEGIN (?:RSA |OPENSSH )?PRIVATE KEY-----\s*$',
                     path.read_bytes()):
            raise SystemExit('private key marker in '+item['path'])
    print('VERIFIED_FILES',len(files),'BYTES',sum(item['bytes'] for item in files))

if __name__=='__main__':main()
