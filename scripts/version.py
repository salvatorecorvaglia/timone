#!/usr/bin/env python3
"""Keep application and desktop metadata in sync with web/package.json."""

import argparse
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--set", dest="version", help="Set the application version and synchronize metadata")
    parser.add_argument("--check", action="store_true", help="Fail if generated metadata differs")
    args = parser.parse_args()
    if args.check and args.version:
        parser.error("--check cannot be combined with --set")
    source = ROOT / "web/package.json"
    current = source.read_text()
    version = args.version if args.version is not None else json.loads(current)["version"]
    if not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version):
        parser.error("Expected a stable semantic version")
    outputs = {}
    for path in (source, ROOT / "desktop/package.json"):
        content = path.read_text()
        data = json.loads(content)
        old = json.dumps(data["version"])
        outputs[path] = content.replace('"version": ' + old, '"version": ' + json.dumps(version), 1)
    stale = [path for path, content in outputs.items() if path.read_text() != content]
    if args.check and stale:
        parser.exit(1, "Version metadata is out of sync; run python3 scripts/version.py\n")
    if not args.check:
        for path in stale:
            path.write_text(outputs[path])
    print(version)


if __name__ == "__main__":
    main()
