import hashlib
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location(
    "backend", Path(__file__).parents[2] / "scripts/prepare_desktop_backend.py"
)
backend = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backend)


class BackendTests(unittest.TestCase):
    def test_promotes_only_matching_platform_commit_and_checksum(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "timone").write_bytes(b"binary")
            (root / "build.json").write_text(
                json.dumps(
                    {
                        "goos": "darwin",
                        "goarch": "arm64",
                        "version": "1.2.3",
                        "commit": "a" * 40,
                    }
                )
            )
            (root / "checksums.txt").write_text(
                hashlib.sha256(b"binary").hexdigest() + "  timone\n"
            )
            output = root / "resources"
            result = backend.prepare(root, output, "darwin", "arm64", "1.2.3", "a" * 40)
            self.assertEqual(result.read_bytes(), b"binary")
            for goos, goarch, version, commit in [
                ("linux", "arm64", "1.2.3", "a" * 40),
                ("darwin", "amd64", "1.2.3", "a" * 40),
                ("darwin", "arm64", "1.2.4", "a" * 40),
                ("darwin", "arm64", "1.2.3", "b" * 40),
            ]:
                with self.assertRaisesRegex(ValueError, "identity"):
                    backend.prepare(root, output, goos, goarch, version, commit)
            (root / "timone").write_bytes(b"broken")
            with self.assertRaisesRegex(ValueError, "checksum"):
                backend.prepare(root, output, "darwin", "arm64", "1.2.3", "a" * 40)
