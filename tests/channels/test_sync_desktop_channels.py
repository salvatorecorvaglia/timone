import base64
import hashlib
import importlib
import json
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "scripts"))
sync = importlib.import_module("sync_desktop_channels")


class ChannelSyncTest(unittest.TestCase):
    content = b'{"version":"1.0.0"}'
    filename = "timone-desktop.json"

    def manifest(self, content=None):
        return f'{hashlib.sha256(content or self.content).hexdigest()}  {self.filename}\n'

    def test_verified_version_and_integrity(self):
        self.assertEqual(sync.verify("v1.0.0", self.filename, self.manifest(), self.content, "scoop"), self.content.decode())
        for manifest, content in [(self.manifest(), self.content + b' '), (self.manifest() * 2, self.content), ("invalid", self.content), (self.manifest(b'{"version":"0.9.0"}'), b'{"version":"0.9.0"}')]:
            with self.subTest(manifest=manifest), self.assertRaises(ValueError):
                sync.verify("v1.0.0", self.filename, manifest, content, "scoop")
        with self.assertRaises(ValueError):
            sync.verify("v1.0.0", "../evil.json", self.manifest(), self.content, "scoop")

    def test_selection_ignores_drafts_missing_assets_and_channel_mismatches(self):
        def release(tag, prerelease=False, draft=False, asset="timone-desktop.json"):
            return {"tag_name": tag, "prerelease": prerelease, "draft": draft, "assets": [{"name": asset}, {"name": "checksums.txt"}]}
        releases = [release("v0.9.0"), release("v1.0.0"), release("v1.0.0", draft=True), release("v1.0.0", prerelease=True), release("v1.0.0", asset="wrong"), release("v1.0.0-preview.1", True)]
        self.assertEqual([r["tag_name"] for r in sync.select_releases(releases, "scoop")], ["v1.0.0"])

    def test_no_downgrades_or_unnecessary_writes(self):
        for previous in ['{"version":"1.0.1"}', self.content.decode()]:
            with patch.dict(sync.os.environ, {}, clear=True), patch.object(sync, "api", return_value={"content": base64.b64encode(previous.encode()).decode(), "sha": "old"}) as api:
                self.assertFalse(sync.publish("scoop", "v1.0.0", self.filename, self.content.decode()))
                self.assertEqual(api.call_count, 1)

    def test_bootstrap_writes_only_expected_destination(self):
        with patch.dict(sync.os.environ, {"GITHUB_REPOSITORY": "salvatorecorvaglia/scoop-bucket"}, clear=True), patch.object(sync, "api", side_effect=[None, {}]) as api:
            self.assertTrue(sync.publish("scoop", "v1.0.0", self.filename, self.content.decode()))
            endpoint = api.call_args.args[0]
            self.assertEqual(endpoint, "repos/salvatorecorvaglia/scoop-bucket/contents/bucket/timone-desktop.json")
            self.assertEqual(api.call_args.kwargs["payload"]["branch"], "main")
        with patch.dict(sync.os.environ, {"GITHUB_REPOSITORY": "wrong/repo"}, clear=True), self.assertRaises(ValueError):
            sync.publish("scoop", "v1.0.0", self.filename, self.content.decode())


if __name__ == "__main__":
    unittest.main()
