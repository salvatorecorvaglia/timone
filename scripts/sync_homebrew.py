#!/usr/bin/env python3
"""Pull a verified formula from the latest stable Timone release into its tap."""

import hashlib
import os
from pathlib import Path
import re
import subprocess
import tempfile

import publish_homebrew as publisher


SOURCE = "salvatorecorvaglia/timone"


def verify_formula(tag, manifest, formula):
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError("Expected a stable release tag")
    matches = [line.split()[0] for line in manifest.splitlines()
               if len(line.split()) == 2 and line.split()[1] == "timone.rb"]
    if len(matches) != 1 or not re.fullmatch(r"[0-9a-f]{64}", matches[0]):
        raise ValueError("Missing, duplicate, or invalid formula checksum")
    if hashlib.sha256(formula).hexdigest() != matches[0]:
        raise ValueError("Formula checksum mismatch")
    text = formula.decode()
    if publisher.version_of(text) != tuple(map(int, tag[1:].split("."))):
        raise ValueError("Formula version does not match the release")
    return text


def sync():
    release = publisher.api(f"repos/{SOURCE}/releases/latest")
    tag = release["tag_name"]
    if release.get("draft") or release.get("prerelease"):
        raise ValueError("Expected a published stable release")
    assets = {asset["name"] for asset in release["assets"]}
    if "timone.rb" not in assets:
        print(f"{tag} has no Homebrew formula yet; waiting for the next release.")
        return False, False
    if "checksums.txt" not in assets:
        raise ValueError("Release is missing checksums.txt")
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError("Expected a stable release tag")
    with tempfile.TemporaryDirectory(prefix="timone-tap-sync-") as directory:
        subprocess.run(
            ["gh", "release", "download", tag, "--repo", SOURCE, "--dir", directory,
             "--pattern", "timone.rb", "--pattern", "checksums.txt"], check=True,
        )
        root = Path(directory)
        formula = verify_formula(tag, (root / "checksums.txt").read_text(), (root / "timone.rb").read_bytes())
    message = publisher.publish(formula)
    print(message)
    return True, message.startswith("Updated ")


def main():
    ready, changed = sync()
    output = os.environ.get("GITHUB_OUTPUT")
    if output:
        with open(output, "a") as destination:
            destination.write(f"ready={str(ready).lower()}\nchanged={str(changed).lower()}\n")


if __name__ == "__main__":
    main()
