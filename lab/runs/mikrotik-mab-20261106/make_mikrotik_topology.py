"""Create a separate isolated EVE topology for MikroTik CHR wired MAB.

The existing Arista candidate and original baseline XML are read only.
"""

from pathlib import Path
from uuid import uuid4
import xml.etree.ElementTree as ET


ROOT = Path(__file__).resolve().parent
SOURCE = ROOT / "NAC-Pilot-Candidate.unl"
TARGET = ROOT / "NAC-MAB-MikroTik.unl"
if TARGET.exists():
    raise SystemExit(f"Refusing to overwrite {TARGET}")

tree = ET.parse(SOURCE)
lab = tree.getroot()
lab.set("name", "NAC-MAB-MikroTik")
lab.set("id", str(uuid4()))
lab.find("description").text = "Separate MikroTik CHR MAB qualification; Arista pilot preserved"
lab.find("body").text = (
    "Isolated EVE data/control bridges. Run only while baseline and Arista "
    "candidate nodes are stopped because the address plan is reused."
)
for node in lab.findall("./topology/nodes/node"):
    old = int(node.get("id"))
    node.set("id", str(old + 10))
    node.set("uuid", str(uuid4()))
    node.set("name", {
        11: "MikroTik-CHR-access",
        12: "mab-printer-or-supplicant",
        13: "mab-corp-service",
        14: "mab-quarantine-service",
        15: "mab-radius-control",
    }[old])
    if old == 11:
        node.set("template", "mikrotik")
        node.set("image", "mikrotik-7.24.6")
        node.set("ram", "1024")
        node.set("ethernet", "4")
        node.set("qemu_nic", "e1000-82545em")
        node.set("qemu_options", "-machine type=pc,accel=kvm -serial mon:stdio -nographic -no-user-config -nodefaults -display none -vga std -rtc base=utc -boot order=c")
        for index, interface in enumerate(node.findall("interface")):
            interface.set("id", str(index))
            interface.set("name", f"e{index}")
    elif old == 15:
        node.set("image", "linux-nac24-mab-control")
    for interface in node.findall("interface"):
        interface.set("network_id", str(int(interface.get("network_id")) + 10))

for network in lab.findall("./topology/networks/network"):
    network.set("id", str(int(network.get("id")) + 10))
    network.set("name", "mab-" + network.get("name"))

ET.indent(tree, space="  ")
tree.write(TARGET, encoding="utf-8", xml_declaration=True)
print(TARGET)
print("lab UUID", lab.get("id"))
