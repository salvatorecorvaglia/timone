#!/usr/bin/env python3
"""Check allocation budgets using medians of repeated, fixed-work Go benchmarks."""

import json
from pathlib import Path
import re
import statistics
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
BUDGETS = ROOT / "tests/performance/budgets.json"
ROW = re.compile(r"^(Benchmark\S+?)(?:-\d+)?\s+\d+\s+.*?\s(\d+) B/op\s+(\d+) allocs/op$")


def check(output, budgets):
    samples = {name: [] for name in budgets}
    for line in output.splitlines():
        match = ROW.match(line.strip())
        if match and match[1] in samples:
            samples[match[1]].append((int(match[2]), int(match[3])))
    errors = []
    for name, limits in budgets.items():
        rows = samples[name]
        if len(rows) != 3:
            errors.append(f"{name}: expected three samples, found {len(rows)}")
            continue
        allocated = int(statistics.median(row[0] for row in rows))
        count = int(statistics.median(row[1] for row in rows))
        print(f"{name}: {allocated} B/op, {count} allocs/op")
        if allocated > limits["bytes"] or count > limits["allocs"]:
            errors.append(f"{name}: allocation budget exceeded ({limits})")
    return errors


def main():
    budgets = json.loads(BUDGETS.read_text())
    pattern = "^(" + "|".join(re.escape(name) for name in budgets) + ")$"
    result = subprocess.run(
        ["go", "test", "-run", "^$", "-bench", pattern, "-benchmem", "-benchtime=5x",
         "-count=3", "./internal/service", "./internal/provider"],
        cwd=ROOT, text=True, capture_output=True,
    )
    if result.returncode:
        sys.stderr.write(result.stdout + result.stderr)
        return result.returncode
    errors = check(result.stdout, budgets)
    for error in errors:
        print(error, file=sys.stderr)
    return int(bool(errors))


if __name__ == "__main__":
    raise SystemExit(main())
