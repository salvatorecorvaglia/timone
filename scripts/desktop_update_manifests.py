#!/usr/bin/env python3
"""Merge and validate the desktop update feeds before a release is published.

electron-builder writes one `latest-mac.yml` per macOS architecture. The
release job downloads both, so merging them into one file is what lets a Mac
pick the zip for its own architecture. The other platforms already produce a
distinct file per architecture (`latest.yml`, `latest-linux.yml`,
`latest-linux-arm64.yml`).

Every manifest is then checked against the installers actually present: the
release would otherwise go public with a feed pointing at a missing or
truncated asset, and every installed copy would fail its next update.
"""

import argparse
import sys
from pathlib import Path

import yaml

MAC_MANIFESTS = ('latest-mac-arm64.yml', 'latest-mac-x64.yml')
REQUIRED_MANIFESTS = (
    'latest.yml',
    'latest-mac.yml',
    'latest-linux.yml',
    'latest-linux-arm64.yml',
)


def load_manifest(path):
    try:
        data = yaml.safe_load(path.read_text())
    except yaml.YAMLError as error:
        raise SystemExit(f'{path.name}: invalid YAML: {error}') from error
    if not isinstance(data, dict):
        raise SystemExit(f'{path.name}: not a mapping')
    return data


def validate_manifest(manifest, version, directory):
    """Checks one feed against the version and the files next to it."""
    name = manifest.get('_name', 'manifest')
    if str(manifest.get('version', '')) != version:
        raise SystemExit(
            f'{name}: version {manifest.get("version")!r} does not match {version!r}'
        )

    files = manifest.get('files')
    if not isinstance(files, list) or not files:
        raise SystemExit(f'{name}: no files listed')

    urls = set()
    for entry in files:
        if not isinstance(entry, dict):
            raise SystemExit(f'{name}: file entry is not a mapping')
        url = entry.get('url')
        if not isinstance(url, str) or not url or '/' in url or '\\' in url or url in ('.', '..'):
            raise SystemExit(f'{name}: unsafe asset name {url!r}')
        if url in urls:
            raise SystemExit(f'{name}: duplicate asset {url}')
        urls.add(url)

        sha512 = entry.get('sha512')
        if not isinstance(sha512, str) or len(sha512) != 88:
            raise SystemExit(f'{name}: invalid sha512 for {url}')

        size = entry.get('size')
        if not isinstance(size, int) or size <= 0:
            raise SystemExit(f'{name}: invalid size for {url}')
        asset = directory / url
        if not asset.is_file():
            raise SystemExit(f'{name}: missing asset {url}')
        actual = asset.stat().st_size
        if actual != size:
            raise SystemExit(f'{name}: {url} is {actual} bytes, feed says {size}')

    path = manifest.get('path')
    if path is not None and path not in urls:
        raise SystemExit(f'{name}: path {path!r} is not one of the listed files')


def merge_mac(manifests, version, directory):
    """Merges per-architecture feeds, keeping every zip exactly once."""
    merged = dict(manifests[0])
    files = []
    seen = {}
    for manifest in manifests:
        validate_manifest(manifest, version, directory)
        for entry in manifest['files']:
            url = entry['url']
            if url in seen:
                if seen[url]['sha512'] != entry['sha512']:
                    raise SystemExit(f'latest-mac: conflicting checksums for {url}')
                continue
            seen[url] = entry
            files.append(entry)

    merged['files'] = files
    # Legacy electron-updater 1.x fields point at one file; modern versions
    # choose from `files` by architecture, but keeping these consistent costs
    # nothing and avoids a feed that contradicts itself. Prefer the zip because
    # that is what a Mac update can install without mounting a dmg.
    preferred = next((entry for entry in files if entry['url'].endswith('.zip')), files[0])
    merged['path'] = preferred['url']
    merged['sha512'] = preferred['sha512']
    merged['releaseDate'] = max(str(m.get('releaseDate', '')) for m in manifests)
    merged.pop('_name', None)
    return merged


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', required=True, type=Path, help='Downloaded desktop artifacts')
    parser.add_argument('--version', required=True, help='Release version without the v prefix')
    args = parser.parse_args()

    directory = args.directory
    version = args.version.removeprefix('v')

    mac_inputs = [directory / name for name in MAC_MANIFESTS]
    missing = [path.name for path in mac_inputs if not path.is_file()]
    if missing:
        raise SystemExit(f'Missing macOS update manifests: {", ".join(missing)}')

    manifests = []
    for path in mac_inputs:
        manifest = load_manifest(path)
        manifest['_name'] = path.name
        manifests.append(manifest)

    merged = merge_mac(manifests, version, directory)
    validate_manifest({**merged, '_name': 'latest-mac.yml'}, version, directory)
    (directory / 'latest-mac.yml').write_text(yaml.safe_dump(merged, sort_keys=False))
    for path in mac_inputs:
        path.unlink()

    for name in REQUIRED_MANIFESTS:
        path = directory / name
        if not path.is_file():
            raise SystemExit(f'Missing update manifest: {name}')
        manifest = load_manifest(path)
        manifest['_name'] = name
        validate_manifest(manifest, version, directory)

    print(
        'Update feeds verified: '
        + ', '.join(f'{name} ({len(load_manifest(directory / name)["files"])} files)' for name in REQUIRED_MANIFESTS)
    )
    return 0


if __name__ == '__main__':
    sys.exit(main())
