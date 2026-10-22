import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("desktop_channels", Path(__file__).resolve().parents[2] / "scripts/desktop_channels.py")
channels = importlib.util.module_from_spec(spec)
spec.loader.exec_module(channels)


class DesktopChannelsTest(unittest.TestCase):
    version = "1.0.0"
    manifest = "\n".join(f"{digest * 64}  timone-desktop_1.0.0_{name}" for digest, name in [("a", "mac_arm64.dmg"), ("b", "mac_x64.dmg"), ("c", "win_x64.exe")])

    def test_pinned_assets_architectures_and_hashes(self):
        cask, raw = channels.render("v" + self.version, self.manifest)
        self.assertIn('arch arm: "arm64", intel: "x64"', cask)
        self.assertIn('"' + "a" * 64 + '"', cask)
        self.assertIn('"' + "b" * 64 + '"', cask)
        self.assertIn('app "Timone.app"', cask)
        scoop = json.loads(raw)
        self.assertEqual(scoop["version"], self.version)
        self.assertEqual(set(scoop["architecture"]), {"64bit"})
        self.assertEqual(scoop["architecture"]["64bit"]["hash"], "c" * 64)
        self.assertIn(f"/v{self.version}/timone-desktop_{self.version}_win_x64.exe#", scoop["architecture"]["64bit"]["url"])
        self.assertNotIn("installer", scoop)
        self.assertNotIn("autoupdate", scoop)

    def test_rejects_missing_duplicate_malformed_or_other_version_assets(self):
        for manifest in ["", self.manifest.replace("1.0.0", "0.9.0"), self.manifest + "\n" + self.manifest.splitlines()[0], self.manifest.replace("a" * 64, "invalid")]:
            with self.subTest(manifest=manifest), self.assertRaises(ValueError):
                channels.render(self.version, manifest)
        for version in ["01.0.0", "1.0.0+invalid", "1.0.0-preview.01", "1.0.0\nmalicious"]:
            with self.subTest(version=version), self.assertRaises(ValueError):
                channels.render(version, self.manifest)

    def test_stable_channel_and_signing(self):
        cask, raw = channels.render(self.version, self.manifest)
        self.assertIn('cask "timone-desktop"', cask)
        self.assertEqual(json.loads(raw)["shortcuts"], [["timone-desktop.exe", "Timone"]])
        self.assertEqual(channels.channel_names("v1.0.0"), ("timone-desktop.rb", "timone-desktop.json"))
        with self.assertRaises(ValueError):
            channels.channel_names("v1.0.0-preview.1")
        signed_cask, signed_scoop = channels.render(self.version, self.manifest, signed=True)
        self.assertNotIn("caveats", signed_cask)
        self.assertNotIn("Unsigned", signed_scoop)


if __name__ == "__main__":
    unittest.main()
