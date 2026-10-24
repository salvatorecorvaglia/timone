import contextlib
import importlib.util
import io
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("benchmarks", Path(__file__).resolve().parents[2] / "scripts/check_benchmarks.py")
benchmarks = importlib.util.module_from_spec(spec)
spec.loader.exec_module(benchmarks)


class BudgetTests(unittest.TestCase):
    def check(self, rows):
        with contextlib.redirect_stdout(io.StringIO()):
            return benchmarks.check(rows, {"BenchmarkFixture": {"bytes": 100, "allocs": 10}})

    def test_median_and_throughput_columns(self):
        rows = "\n".join(f"BenchmarkFixture-2 5 100 ns/op 200 MB/s {value} B/op 8 allocs/op" for value in [90, 95, 200])
        self.assertEqual(self.check(rows), [])

    def test_missing_samples_and_allocation_regressions_fail(self):
        self.assertTrue(self.check(""))
        for allocated, count in [(101, 8), (90, 11)]:
            row = f"BenchmarkFixture-2 5 100 ns/op {allocated} B/op {count} allocs/op"
            self.assertTrue(self.check("\n".join([row] * 3)))


if __name__ == "__main__":
    unittest.main()
