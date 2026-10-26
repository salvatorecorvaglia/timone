import base64
import hashlib
import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "desktop_update_manifests",
    Path(__file__).resolve().parents[2] / "scripts" / "desktop_update_manifests.py",
)
updates = importlib.util.module_from_spec(spec)
spec.loader.exec_module(updates)


class DesktopUpdateManifestsTest(unittest.TestCase):
    version = "1.2.0"

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)

    def asset(self, name, data=b"timone"):
        (self.directory / name).write_bytes(data)
        return {
            "url": name,
            "sha512": base64.b64encode(hashlib.sha512(data).digest()).decode(),
            "size": len(data),
        }

    def manifest(self, files, version=None):
        return {"version": version or self.version, "files": files}

    def test_merges_both_mac_architectures_into_one_feed(self):
        arm = self.asset("timone-desktop_1.2.0_mac_arm64.zip", b"arm")
        arm_dmg = self.asset("timone-desktop_1.2.0_mac_arm64.dmg", b"arm-dmg")
        intel = self.asset("timone-desktop_1.2.0_mac_x64.zip", b"intel")
        merged = updates.merge_mac(
            [
                {
                    **self.manifest([arm_dmg, arm]),
                    "releaseDate": "2026-10-25T10:00:00.000Z",
                },
                {**self.manifest([intel]), "releaseDate": "2026-10-26T10:00:00.000Z"},
            ],
            self.version,
            self.directory,
        )
        self.assertEqual(
            [entry["url"] for entry in merged["files"]],
            [arm_dmg["url"], arm["url"], intel["url"]],
        )
        # The legacy top-level path must stay a zip, even when the first feed
        # entry is a dmg.
        self.assertEqual(merged["path"], arm["url"])
        self.assertEqual(merged["releaseDate"], "2026-10-26T10:00:00.000Z")
        updates.validate_manifest({**merged, "_name": "latest-mac.yml"}, self.version, self.directory)

    def test_rejects_conflicting_checksums_for_the_same_asset(self):
        arm = self.asset("timone-desktop_1.2.0_mac_arm64.zip", b"arm")
        conflicting = {**arm, "sha512": base64.b64encode(hashlib.sha512(b"other").digest()).decode()}
        with self.assertRaises(SystemExit):
            updates.merge_mac(
                [self.manifest([arm]), self.manifest([conflicting])],
                self.version,
                self.directory,
            )

    def test_rejects_a_feed_for_another_version(self):
        arm = self.asset("timone-desktop_1.2.0_mac_arm64.zip", b"arm")
        with self.assertRaises(SystemExit):
            updates.validate_manifest(
                {**self.manifest([arm], version="1.1.0"), "_name": "latest-mac.yml"},
                self.version,
                self.directory,
            )

    def test_rejects_missing_truncated_and_unsafe_assets(self):
        missing = {
            "url": "timone-desktop_1.2.0_mac_arm64.zip",
            "sha512": "a" * 88,
            "size": 5,
        }
        with self.assertRaises(SystemExit):
            updates.validate_manifest(
                {**self.manifest([missing]), "_name": "latest-mac.yml"},
                self.version,
                self.directory,
            )

        present = self.asset("timone-desktop_1.2.0_mac_x64.zip", b"intel")
        (self.directory / present["url"]).write_bytes(b"no")
        with self.assertRaises(SystemExit):
            updates.validate_manifest(
                {**self.manifest([present]), "_name": "latest-mac.yml"},
                self.version,
                self.directory,
            )

        unsafe = {**present, "url": "../escape.zip"}
        with self.assertRaises(SystemExit):
            updates.validate_manifest(
                {**self.manifest([unsafe]), "_name": "latest-mac.yml"},
                self.version,
                self.directory,
            )

    def test_main_merges_and_verifies_a_complete_release(self):
        mac_arm = self.asset("timone-desktop_1.2.0_mac_arm64.zip", b"arm")
        mac_x64 = self.asset("timone-desktop_1.2.0_mac_x64.zip", b"intel")
        win = self.asset("timone-desktop_1.2.0_win_x64.exe", b"win")
        linux = self.asset("timone-desktop_1.2.0_linux_x64.AppImage", b"linux")
        linux_arm = self.asset("timone-desktop_1.2.0_linux_arm64.AppImage", b"linux-arm")

        feeds = {
            "latest-mac-arm64.yml": [mac_arm],
            "latest-mac-x64.yml": [mac_x64],
            "latest.yml": [win],
            "latest-linux.yml": [linux],
            "latest-linux-arm64.yml": [linux_arm],
        }
        for name, files in feeds.items():
            (self.directory / name).write_text(
                updates.yaml.safe_dump(self.manifest(files), sort_keys=False)
            )

        with patch.object(
            sys,
            "argv",
            [
                "desktop_update_manifests.py",
                "--directory",
                str(self.directory),
                "--version",
                "v1.2.0",
            ],
        ):
            self.assertEqual(updates.main(), 0)

        self.assertTrue((self.directory / "latest-mac.yml").is_file())
        self.assertFalse((self.directory / "latest-mac-arm64.yml").exists())
        self.assertFalse((self.directory / "latest-mac-x64.yml").exists())
        merged = updates.load_manifest(self.directory / "latest-mac.yml")
        self.assertEqual(len(merged["files"]), 2)


if __name__ == "__main__":
    unittest.main()
