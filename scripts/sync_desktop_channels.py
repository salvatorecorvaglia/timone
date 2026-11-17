#!/usr/bin/env python3
"""Pull checksum-verified stable desktop manifests into their own repositories."""

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

from desktop_channels import channel_names, version_key

SOURCE = "salvatorecorvaglia/timone"
TARGETS = {"scoop": ("salvatorecorvaglia/scoop-bucket", "bucket")}


def api(endpoint, payload=None, allow_missing=False):
    command = ["gh", "api", endpoint]
    if payload is not None:
        command += ["--method", "PUT", "--input", "-"]
    result = subprocess.run(
        command, input=json.dumps(payload) if payload is not None else None,
        capture_output=True, text=True, check=False,
    )
    if result.returncode:
        if allow_missing and "(HTTP 404)" in result.stderr:
            return None
        raise RuntimeError(f"GitHub API request failed for {endpoint}; check tap access and GH_TOKEN")
    return json.loads(result.stdout)


def manifest_version(content):
    version = json.loads(content)["version"]
    version_key(version)
    return version


def verify(tag, name, manifest, content):
    version_key(tag)
    if name != channel_names(tag):
        raise ValueError("Wrong manifest for release channel")
    entries = [line.split() for line in manifest.splitlines()]
    matches = [fields[0] for fields in entries if len(fields) == 2 and fields[1] == name]
    if len(matches) != 1 or not re.fullmatch(r"[0-9a-f]{64}", matches[0]):
        raise ValueError("Missing, duplicate, or invalid desktop manifest checksum")
    if hashlib.sha256(content).hexdigest() != matches[0]:
        raise ValueError("Desktop manifest checksum mismatch")
    text = content.decode("utf-8")
    if manifest_version(text) != tag.removeprefix("v"):
        raise ValueError("Desktop manifest version differs from release")
    return text


def select_releases(releases):
    selected = {}
    for release in releases:
        if release.get("draft"):
            continue
        tag = release.get("tag_name", "")
        try:
            key = version_key(tag)
        except ValueError:
            continue
        if release.get("prerelease"):
            continue
        name = channel_names(tag)
        assets = {asset["name"] for asset in release.get("assets", [])}
        if name not in assets or "checksums.txt" not in assets:
            continue
        previous = selected.get("stable")
        if previous is None or key > version_key(previous["tag_name"]):
            selected["stable"] = release
    return list(selected.values())


def publish(target, tag, name, content):
    repository, directory = TARGETS[target]
    if name != channel_names(tag) or manifest_version(content) != tag.removeprefix("v"):
        raise ValueError("Manifest destination or version does not match release")
    environment_repo = os.environ.get("GITHUB_REPOSITORY")
    if environment_repo and environment_repo != repository:
        raise ValueError("Sync must execute in the destination repository")
    endpoint = f"repos/{repository}/contents/{directory}/{name}"
    existing = api(endpoint, allow_missing=True)
    payload = {"message": f"Update {name} to {tag}", "content": base64.b64encode(content.encode()).decode(), "branch": "main"}
    if existing:
        previous = base64.b64decode(existing["content"]).decode()
        if version_key(manifest_version(previous)) > version_key(tag):
            print(f"Skipping downgrade of {name}")
            return False
        if previous == content:
            return False
        payload["sha"] = existing["sha"]
    api(endpoint, payload=payload)
    print(f"Updated {repository}/{directory}/{name} to {tag}")
    return True


def sync(target):
    releases = api(f"repos/{SOURCE}/releases?per_page=100")
    changed = False
    ready = False
    for release in select_releases(releases):
        tag = release["tag_name"]
        name = channel_names(tag)
        with tempfile.TemporaryDirectory(prefix="timone-channel-sync-") as temporary:
            subprocess.run(["gh", "release", "download", tag, "--repo", SOURCE, "--dir", temporary,
                            "--pattern", name, "--pattern", "checksums.txt"], check=True)
            root = Path(temporary)
            content = verify(tag, name, (root / "checksums.txt").read_text(), (root / name).read_bytes())
        ready = True
        changed = publish(target, tag, name, content) or changed
    return ready, changed


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", choices=TARGETS, required=True)
    args = parser.parse_args()
    ready, changed = sync(args.target)
    output = os.environ.get("GITHUB_OUTPUT")
    if output:
        with open(output, "a") as destination:
            destination.write(f"ready={str(ready).lower()}\nchanged={str(changed).lower()}\n")


if __name__ == "__main__":
    main()
