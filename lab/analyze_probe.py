"""Summarize timestamped endpoint TCP transitions from a CoA probe JSONL."""
import json
from pathlib import Path
import sys

path = Path(sys.argv[1])
last = {}
for line in path.read_text().splitlines():
    item = json.loads(line)
    if "event" in item:
        print(item)
        continue
    segment = item["segment"]
    value = item["connected"]
    if last.get(segment) != value:
        print(item["time"], segment, "OPEN" if value else "BLOCKED", item.get("error"))
        last[segment] = value
