import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("version", Path(__file__).resolve().parents[2] / "scripts/version.py")
version = importlib.util.module_from_spec(spec)
spec.loader.exec_module(version)


class VersionTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        for name in ("web/package.json", "desktop/package.json"):
            path = self.root / name
            path.parent.mkdir(parents=True)
            path.write_text('{"version": "1.0.0", "name": "timone"}\n')
        chart = self.root / "deploy/helm/timone/Chart.yaml"
        chart.parent.mkdir(parents=True)
        chart.write_text('name: timone\nversion: "1.0.0"\nappVersion: "1.0.0"\n')

    def run_command(self, *args):
        with patch.object(version, "ROOT", self.root), patch.object(sys, "argv", ["version.py", *args]):
            version.main()

    def test_updates_all_packaging_metadata_and_preserves_other_fields(self):
        self.run_command("--set", "1.0.1")
        for name in ("web/package.json", "desktop/package.json"):
            self.assertEqual(json.loads((self.root / name).read_text()), {"version": "1.0.1", "name": "timone"})
        self.assertEqual((self.root / "deploy/helm/timone/Chart.yaml").read_text(), 'name: timone\nversion: "1.0.1"\nappVersion: "1.0.1"\n')
        self.run_command("--check")

    def test_check_detects_drift_without_writing_and_sync_repairs_it(self):
        path = self.root / "desktop/package.json"
        path.write_text('{"version": "0.9.0"}\n')
        with self.assertRaises(SystemExit):
            self.run_command("--check")
        self.assertEqual(json.loads(path.read_text())["version"], "0.9.0")
        self.run_command()
        self.run_command("--check")

    def test_invalid_version_does_not_modify_files(self):
        before = {p: p.read_bytes() for p in self.root.rglob("*") if p.is_file()}
        with self.assertRaises(SystemExit):
            self.run_command("--set", "invalid")
        self.assertEqual(before, {p: p.read_bytes() for p in before})


if __name__ == "__main__":
    unittest.main()
