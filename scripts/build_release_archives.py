#!/usr/bin/env python3
"""Build CLI archives in parallel and retain verified desktop backends."""

import argparse
import hashlib
import json
import os
import re
import subprocess
import tarfile
import zipfile
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

TARGETS = [
    (goos, arch)
    for goos in ("linux", "darwin", "windows")
    for arch in ("amd64", "arm64")
]


def checksum(path):
    with path.open("rb") as file:
        return hashlib.file_digest(file, "sha256").hexdigest()


def build_target(root, output, version, commit, goos, arch):
    directory = output / "backends" / f"{goos}-{arch}"
    directory.mkdir(parents=True, exist_ok=True)
    binary = directory / ("timone.exe" if goos == "windows" else "timone")
    subprocess.run(
        [
            "go",
            "build",
            "-p=2",
            "-trimpath",
            "-ldflags=-s -w",
            "-o",
            str(binary),
            "./cmd/timone",
        ],
        cwd=root,
        env={**os.environ, "CGO_ENABLED": "0", "GOOS": goos, "GOARCH": arch},
        check=True,
    )
    (directory / "checksums.txt").write_text(f"{checksum(binary)}  {binary.name}\n")
    (directory / "build.json").write_text(
        json.dumps({"version": version, "commit": commit, "goos": goos, "goarch": arch})
    )
    members = [
        binary,
        *(root / name for name in ("README.md", "LICENSE", "CHANGELOG.md")),
    ]
    name = f"timone_{version}_{goos}_{arch}"
    if goos == "windows":
        archive = output / f"{name}.zip"
        with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as stream:
            for path in members:
                stream.write(path, path.name)
    else:
        archive = output / f"{name}.tar.gz"
        with tarfile.open(archive, "w:gz") as stream:
            for path in members:
                stream.add(path, arcname=path.name, recursive=False)
    print(f"Built {archive.name}", flush=True)
    return archive


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--jobs", type=int, default=2)
    parser.add_argument("--output", type=Path, default=Path("release"))
    args = parser.parse_args()
    version = args.version.removeprefix("v")
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?", version) or args.jobs < 1:
        parser.error("Expected a semantic version and a positive job count")
    root = Path.cwd()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    commit = (
        os.environ.get("GITHUB_SHA")
        or subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    )
    with ThreadPoolExecutor(max_workers=min(args.jobs, len(TARGETS))) as executor:
        futures = [
            executor.submit(build_target, root, output, version, commit, goos, arch)
            for goos, arch in TARGETS
        ]
        archives = [future.result() for future in futures]
    (output / "checksums.txt").write_text(
        "".join(f"{checksum(path)}  {path.name}\n" for path in sorted(archives))
    )


if __name__ == "__main__":
    main()
