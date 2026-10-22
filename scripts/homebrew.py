#!/usr/bin/env python3
"""Generate a stable Homebrew formula from the release checksum manifest."""

import argparse
from pathlib import Path
import re
from string import Template


TEMPLATE = Path(__file__).resolve().parents[1] / "packaging/homebrew/timone.rb.tmpl"
TARGETS = ("darwin_arm64", "darwin_amd64", "linux_arm64", "linux_amd64")


def render(version, checksums):
    version = version.removeprefix("v")
    if not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version):
        raise ValueError("Homebrew requires a stable version such as v1.0.0")
    entries = {}
    for line in checksums.splitlines():
        fields = line.split()
        if len(fields) != 2 or not re.fullmatch(r"[0-9a-f]{64}", fields[0]):
            raise ValueError("Invalid checksum manifest")
        digest, name = fields
        if name in entries:
            raise ValueError(f"Duplicate checksum: {name}")
        entries[name] = digest
    values = {
        "version": version,
        "base": f"https://github.com/salvatorecorvaglia/timone/releases/download/v{version}",
    }
    for target in TARGETS:
        name = f"timone_{version}_{target}.tar.gz"
        if name not in entries:
            raise ValueError(f"Missing checksum: {name}")
        values[target] = entries[name]
    return Template(TEMPLATE.read_text()).substitute(values)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--checksums", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    try:
        formula = render(args.version, args.checksums.read_text())
    except (ValueError, OSError) as error:
        parser.exit(1, f"homebrew: {error}\n")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(formula)


if __name__ == "__main__":
    main()
