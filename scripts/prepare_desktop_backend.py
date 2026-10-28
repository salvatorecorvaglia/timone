#!/usr/bin/env python3
"""Verify a backend from this workflow run before embedding it in the desktop app."""

import argparse
import hashlib
import json
import os
import shutil
from pathlib import Path


def prepare(source, destination, goos, goarch, version, commit):
    build = json.loads((source / "build.json").read_text())
    expected = {"goos": goos, "goarch": goarch, "version": version, "commit": commit}
    if build != expected:
        raise ValueError("Backend build identity does not match this desktop release")
    filename = "timone.exe" if goos == "windows" else "timone"
    digest, name = (source / "checksums.txt").read_text().strip().split()
    if name != filename:
        raise ValueError("Unexpected backend filename")
    with (source / filename).open("rb") as file:
        actual = hashlib.file_digest(file, "sha256").hexdigest()
    if actual != digest:
        raise ValueError("Backend checksum mismatch")
    destination.mkdir(parents=True, exist_ok=True)
    target = destination / filename
    shutil.copyfile(source / filename, target)
    target.chmod(0o755)
    return target


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--goos", required=True)
    parser.add_argument("--goarch", required=True)
    args = parser.parse_args()
    version = json.loads(Path("desktop/package.json").read_text())["version"]
    prepare(
        args.source,
        Path("desktop/resources"),
        args.goos,
        args.goarch,
        version,
        os.environ["GITHUB_SHA"],
    )


if __name__ == "__main__":
    main()
