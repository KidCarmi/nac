"""Index and verify the local, sanitized rc9 DHCP qualification handoff."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import re


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("directory", type=Path)
    parser.add_argument("--write", action="store_true")
    args = parser.parse_args()
    root = args.directory.resolve()
    manifest_path = root / "sha256-manifest.json"
    if not root.is_dir() or root.name != "dhcp-qualification-rc9-20261107":
        parser.error("expected the rc9 DHCP qualification package")

    actual = []
    for path in sorted(root.rglob("*")):
        if path.is_file() and path != manifest_path:
            data = path.read_bytes()
            actual.append({"path": path.relative_to(root).as_posix(),
                           "bytes": len(data),
                           "sha256": hashlib.sha256(data).hexdigest()})

    if args.write:
        manifest_path.write_text(json.dumps({"files": actual}, indent=2) + "\n")
    expected = json.loads(manifest_path.read_text())["files"]
    if actual != expected:
        raise SystemExit("package SHA-256 manifest mismatch")

    missing = []
    for path in root.rglob("*.md"):
        body = path.read_text(encoding="utf-8")
        for target in re.findall(r"\]\(([^)]+)\)", body):
            if target.startswith(("http://", "https://", "#")):
                continue
            local = target.split("#", 1)[0]
            if local and not (path.parent / local).exists():
                missing.append(f"{path.relative_to(root)} -> {target}")
    if missing:
        raise SystemExit("broken local links:\n" + "\n".join(missing))

    forbidden_names = ("secret", "password", "private", ".qcow2", ".vmdk", ".key", ".bundle", ".tar")
    forbidden = [item["path"] for item in actual
                 if any(part in item["path"].lower() for part in forbidden_names)]
    if forbidden:
        raise SystemExit("forbidden filename in package: " + ", ".join(forbidden))
    print(f"verified {len(actual)} files, {len(list(root.rglob('*.md')))} Markdown files, links and names")


if __name__ == "__main__":
    main()
