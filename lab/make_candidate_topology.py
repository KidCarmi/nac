"""Clone only the isolated EVE topology definition for a clean pilot run.

The candidate lab is not started by this script. Node disks are separate EVE
instances, and the original baseline .unl remains byte-for-byte unchanged.
"""
from pathlib import Path
import uuid
import xml.etree.ElementTree as ET

root = Path(__file__).resolve().parent
source = root / 'baseline-4911d2d/NAC-Wired-Qualification.unl'
target = root / 'NAC-Pilot-Candidate.unl'
if target.exists():
    raise SystemExit('candidate topology already exists; inspect before replacing')
tree = ET.parse(source)
lab = tree.getroot()
lab.set('name', 'NAC-Pilot-Candidate')
lab.set('id', str(uuid.uuid4()))
lab.find('description').text = 'Separate clean Ubuntu 24.04 pilot candidate qualification; baseline lab preserved'
lab.find('body').text = 'Only isolated internal EVE bridges. Start only after stopping baseline nodes because both use the same lab address plan.'
for node in lab.findall('./topology/nodes/node'):
    node.set('uuid', str(uuid.uuid4()))
    if node.get('name') == 'nac-control':
        node.set('name', 'nac-pilot-control')
        node.set('image', 'linux-nac24-control')
        node.set('ram', '3072')
        node.set('cpu', '2')
ET.indent(tree, space='  ')
tree.write(target, encoding='utf-8', xml_declaration=True)
print('CANDIDATE_TOPOLOGY', target)
