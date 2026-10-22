import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]


def load_script(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / f"scripts/{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


generator = load_script("homebrew")
publisher = load_script("publish_homebrew")
with patch.dict("sys.modules", {"publish_homebrew": publisher}):
    synchronizer = load_script("sync_homebrew")


class FormulaTests(unittest.TestCase):
    def setUp(self):
        self.checksums = "".join(
            f"{index:064x}  timone_1.0.0_{target}.tar.gz\n"
            for index, target in enumerate(generator.TARGETS, 1)
        )

    def test_formula_selects_correct_archive_and_checksum_on_each_platform(self):
        formula = generator.render("v1.0.0", self.checksums)
        harness = '''require "json"
class Formula
  class << self
    attr_accessor :selected_url, :selected_sha
    def desc(*) end
    def homepage(*) end
    def version(*) end
    def license(*) end
    def on_macos; yield if ENV.fetch("OS") == "darwin"; end
    def on_linux; yield if ENV.fetch("OS") == "linux"; end
    def on_arm; yield if ENV.fetch("ARCH") == "arm64"; end
    def on_intel; yield if ENV.fetch("ARCH") == "amd64"; end
    def url(value); self.selected_url=value; end
    def sha256(value); self.selected_sha=value; end
    def test; end
  end
end
load ARGV.fetch(0)
puts JSON.generate({url: Timone.selected_url, sha: Timone.selected_sha})
'''
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "timone.rb"
            path.write_text(formula)
            for index, target in enumerate(generator.TARGETS, 1):
                os_name, arch = target.split("_")
                result = subprocess.run(
                    ["ruby", "-e", harness, str(path)],
                    env={**os.environ, "OS": os_name, "ARCH": arch},
                    capture_output=True, text=True, check=True,
                )
                selected = json.loads(result.stdout)
                self.assertEqual(selected["sha"], f"{index:064x}")
                self.assertEqual(selected["url"], f"https://github.com/salvatorecorvaglia/timone/releases/download/v1.0.0/timone_1.0.0_{target}.tar.gz")
        self.assertIn('bin.install_symlink "timone" => "kubectl-timone"', formula)
        self.assertIn('shell_output("#{bin}/kubectl-timone --version")', formula)

    @unittest.skipUnless(shutil.which("brew"), "Homebrew not installed")
    def test_formula_loads_in_native_homebrew(self):
        with tempfile.TemporaryDirectory() as directory:
            formula = Path(directory) / "timone.rb"
            formula.write_text(generator.render("1.0.0", self.checksums))
            harness = Path(directory) / "check.rb"
            # Load the generated formula through Homebrew itself, without installing
            # anything or downloading the example-version release archives.
            harness.write_text(
                'require "formula"\nrequire "json"\n'
                f'f = Formulary.factory({json.dumps(str(formula))})\n'
                'puts JSON.generate({version: f.version.to_s, url: f.stable.url})\n'
            )
            result = subprocess.run(
                ["brew", "ruby", str(harness)],
                env={**os.environ, "HOMEBREW_NO_AUTO_UPDATE": "1", "HOMEBREW_NO_ANALYTICS": "1", "HOMEBREW_DEVELOPER": "0"},
                capture_output=True, text=True, check=True, timeout=60,
            )
            loaded = json.loads(result.stdout)
            os_name = "darwin" if platform.system() == "Darwin" else "linux"
            arch = "arm64" if platform.machine() in ("arm64", "aarch64") else "amd64"
            self.assertEqual(loaded["version"], "1.0.0")
            self.assertTrue(loaded["url"].endswith(f"_{os_name}_{arch}.tar.gz"))

    def test_rejects_prereleases_and_invalid_versions(self):
        for version in ["1.0.0-preview.1", "1.0.0+build", "01.0.0", "1.0", '1.0.0";system("bad")', "../../1.0.0"]:
            with self.subTest(version=version), self.assertRaises(ValueError):
                generator.render(version, self.checksums)

    def test_rejects_missing_duplicate_and_malformed_checksums(self):
        for checksums in ["", self.checksums.splitlines()[0], self.checksums + self.checksums, self.checksums.replace("0000", "zzzz")]:
            with self.subTest(checksums=checksums), self.assertRaises(ValueError):
                generator.render("1.0.0", checksums)

    def test_cli_does_not_replace_output_on_invalid_manifest(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = Path(directory) / "checksums.txt"
            formula = Path(directory) / "timone.rb"
            manifest.write_text("")
            formula.write_text("existing")
            result = subprocess.run(
                ["python3", str(ROOT / "scripts/homebrew.py"), "--version", "1.0.0", "--checksums", str(manifest), "--output", str(formula)],
                capture_output=True, text=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(formula.read_text(), "existing")


class SyncTests(unittest.TestCase):
    formula = b'class Timone < Formula\n  version "1.0.0"\nend\n'

    def manifest(self):
        return hashlib.sha256(self.formula).hexdigest() + "  timone.rb\n"

    def test_verify_formula_checksum_and_version(self):
        self.assertEqual(synchronizer.verify_formula("v1.0.0", self.manifest(), self.formula), self.formula.decode())
        for tag, manifest, formula in [
            ("v0.9.0", self.manifest(), self.formula),
            ("v1.0.0-preview.1", self.manifest(), self.formula),
            ("v1.0.0", self.manifest(), self.formula + b"corruption"),
            ("v1.0.0", self.manifest() * 2, self.formula),
            ("v1.0.0", "", self.formula),
        ]:
            with self.subTest(tag=tag, manifest=manifest), self.assertRaises(ValueError):
                synchronizer.verify_formula(tag, manifest, formula)

    def test_legacy_release_is_skipped_without_writes(self):
        with patch.object(publisher, "api", return_value={"tag_name": "v0.8.0", "assets": []}), patch.object(publisher, "publish") as publish:
            self.assertEqual(synchronizer.sync(), (False, False))
        publish.assert_not_called()

    def test_downloads_from_one_tag_and_publishes_verified_formula(self):
        release = {"tag_name": "v1.0.0", "assets": [{"name": name} for name in ["timone.rb", "checksums.txt"]]}

        def download(command, **kwargs):
            self.assertEqual(command[3], "v1.0.0")
            directory = Path(command[command.index("--dir") + 1])
            (directory / "timone.rb").write_bytes(self.formula)
            (directory / "checksums.txt").write_text(self.manifest())

        with patch.object(publisher, "api", return_value=release), patch.object(synchronizer.subprocess, "run", side_effect=download), patch.object(publisher, "publish", return_value="Updated tap") as publish:
            self.assertEqual(synchronizer.sync(), (True, True))
        publish.assert_called_once_with(self.formula.decode())


class PublisherTests(unittest.TestCase):
    formula = 'class Timone < Formula\n  version "1.0.0"\nend\n'

    def existing(self, formula):
        return {"sha": "previous-commit", "content": base64.b64encode(formula.encode()).decode()}

    def test_bootstrap_creates_only_expected_formula(self):
        with patch.object(publisher, "api", side_effect=[{"default_branch": "main"}, None, {}]) as api:
            publisher.publish(self.formula)
        endpoint = api.call_args.args[0]
        payload = api.call_args.kwargs["payload"]
        self.assertEqual(endpoint, publisher.ENDPOINT)
        self.assertEqual(payload["branch"], "main")
        self.assertNotIn("sha", payload)
        self.assertEqual(base64.b64decode(payload["content"]).decode(), self.formula)

    def test_upgrade_uses_existing_sha(self):
        old = self.formula.replace("1.0.0", "0.8.0")
        with patch.object(publisher, "api", side_effect=[{"default_branch": "main"}, self.existing(old), {}]) as api:
            publisher.publish(self.formula)
        self.assertEqual(api.call_args.kwargs["payload"]["sha"], "previous-commit")

    def test_no_change_or_downgrade_performs_no_write(self):
        for previous in [self.formula, self.formula.replace("1.0.0", "1.0.1")]:
            with self.subTest(previous=previous):
                with patch.object(publisher, "api", side_effect=[{"default_branch": "main"}, self.existing(previous)]) as api:
                    publisher.publish(self.formula)
                self.assertEqual(api.call_count, 2)

    def test_unknown_existing_formula_is_not_overwritten(self):
        with patch.object(publisher, "api", side_effect=[{"default_branch": "main"}, self.existing("unrelated")]) as api:
            with self.assertRaises(ValueError):
                publisher.publish(self.formula)
        self.assertEqual(api.call_count, 2)

    def test_api_failure_is_not_treated_as_missing_formula(self):
        result = subprocess.CompletedProcess([], 1, "", "authentication failed (HTTP 401)")
        with patch.object(publisher.subprocess, "run", return_value=result):
            with self.assertRaises(RuntimeError):
                publisher.api(publisher.ENDPOINT, allow_missing=True)


if __name__ == "__main__":
    unittest.main()
