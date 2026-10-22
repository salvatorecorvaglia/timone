#!/usr/bin/env python3
"""Generate pinned stable desktop manifests from release checksums."""

import argparse
import json
from pathlib import Path
import re


def version_key(version):
    version = version.removeprefix("v")
    match = re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version)
    if not match:
        raise ValueError("Expected a stable release version such as 1.0.0")
    return tuple(map(int, match.groups()))


def channel_names(version):
    version_key(version)
    return "timone-desktop.rb", "timone-desktop.json"


def render(version, manifest, signed=False):
    version = version.removeprefix("v")
    version_key(version)
    cask_token = "timone-desktop"
    scoop_token = "timone-desktop"
    title = "Timone Desktop"
    shortcut = "Timone"
    checksums = {}
    for line in manifest.splitlines():
        fields = line.split()
        if len(fields) != 2 or not re.fullmatch(r"[0-9a-f]{64}", fields[0]):
            raise ValueError("Invalid release checksum manifest")
        digest, filename = fields
        if filename in checksums:
            raise ValueError(f"Duplicate checksum for {filename}")
        checksums[filename] = digest
    names = {arch: f"timone-desktop_{version}_mac_{arch}.dmg" for arch in ("arm64", "x64")}
    windows = f"timone-desktop_{version}_win_x64.exe"
    for name in [*names.values(), windows]:
        if name not in checksums:
            raise ValueError(f"Missing release checksum: {name}")
    base = f"https://github.com/salvatorecorvaglia/timone/releases/download/v{version}"
    caveat = '' if signed else '''  caveats <<~EOS
    This release has no Apple Developer ID signing or notarization.
    macOS security policies may prevent it from opening.
  EOS
'''
    cask = f'''cask "{cask_token}" do
  arch arm: "arm64", intel: "x64"

  version "{version}"
  sha256 arm:   "{checksums[names['arm64']]}",
         intel: "{checksums[names['x64']]}"

  url "https://github.com/salvatorecorvaglia/timone/releases/download/v#{{version}}/timone-desktop_#{{version}}_mac_#{{arch}}.dmg"
  name "{title}"
  desc "Read-only Kubernetes insights and diagnostics"
  homepage "https://github.com/salvatorecorvaglia/timone"

  livecheck do
    skip "This tap automatically pulls checksum-verified release manifests"
  end

  depends_on macos: ">= :ventura"

  app "Timone.app"

{caveat}end
'''
    scoop = {
        "version": version,
        "description": f"{title} — read-only Kubernetes insights and diagnostics",
        "homepage": "https://github.com/salvatorecorvaglia/timone",
        "license": "MIT",
        "architecture": {"64bit": {"url": f"{base}/{windows}#/dl.7z", "hash": checksums[windows]}},
        # Scoop extracts the NSIS payload using 7-Zip. No installer, registry
        # mutations, administrator access, or security-policy changes are needed.
        "pre_install": [
            'Expand-7zipArchive "$dir\\`$PLUGINSDIR\\app-64.7z" "$dir"',
            'Remove-Item "$dir\\`$*", "$dir\\Uninstall*" -Recurse',
        ],
        "shortcuts": [["timone-desktop.exe", shortcut]],
        "bin": [["timone-desktop.exe", scoop_token]],
        "notes": ("" if signed else "Unsigned desktop release. Windows security policies may block execution. ") + "This bucket automatically pulls checksum-verified release manifests.",
    }
    return cask, json.dumps(scoop, indent=4, ensure_ascii=False) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--checksums", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--signed", action="store_true", help="Use only after required signing and notarization have succeeded")
    args = parser.parse_args()
    try:
        cask, scoop = render(args.version, args.checksums.read_text(), signed=args.signed)
        args.output.mkdir(parents=True, exist_ok=True)
        cask_name, scoop_name = channel_names(args.version)
        (args.output / cask_name).write_text(cask)
        (args.output / scoop_name).write_text(scoop)
    except (OSError, ValueError) as error:
        parser.exit(1, f"desktop channels: {error}\n")


if __name__ == "__main__":
    main()
