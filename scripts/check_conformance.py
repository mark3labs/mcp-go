#!/usr/bin/env python3
"""Run MCP server conformance checks with a retry-aware failure baseline."""

from __future__ import annotations

import argparse
import json
import subprocess
import tempfile
from pathlib import Path

CLI_VERSION = "0.1.16"
BASELINE_RETRIES = 10


def read_server_baseline(path: Path) -> list[str]:
    scenarios = []
    in_server = False
    for line in path.read_text(encoding="utf-8").splitlines():
        if line and not line[0].isspace():
            in_server = line.strip() == "server:"
        elif in_server and line.strip().startswith("- "):
            scenarios.append(line.strip()[2:])
    if not scenarios:
        raise ValueError(f"no server scenarios found in {path}")
    return scenarios


def run_cli(url: str, output_dir: Path, scenario: str | None = None) -> tuple[int, str]:
    command = [
        "npx",
        "--yes",
        f"@modelcontextprotocol/conformance@{CLI_VERSION}",
        "server",
        "--url",
        url,
        "--output-dir",
        str(output_dir),
    ]
    if scenario:
        command.extend(["--scenario", scenario])
    result = subprocess.run(command, check=False, capture_output=True, text=True)
    return result.returncode, result.stdout + result.stderr


def read_suite_results(output_dir: Path) -> dict[str, bool]:
    results = {}
    for checks_file in output_dir.glob("server-*/checks.json"):
        checks = json.loads(checks_file.read_text(encoding="utf-8"))
        scenario = checks_file.parent.name.removeprefix("server-").rsplit("-202", maxsplit=1)[0]
        results[scenario] = bool(checks) and all(check.get("status") in {"SUCCESS", "INFO"} for check in checks)
    return results


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", required=True)
    parser.add_argument("--baseline", type=Path, required=True)
    args = parser.parse_args()
    baseline = set(read_server_baseline(args.baseline))

    with tempfile.TemporaryDirectory(prefix="mcp-conformance-suite-") as output:
        suite_dir = Path(output)
        print("Running the official active server conformance suite")
        suite_status, suite_output = run_cli(args.url, suite_dir)
        suite_results = read_suite_results(suite_dir)
        if not suite_results:
            print("The conformance runner produced no scenario results")
            return 1
        unexpected = sorted(name for name, passed in suite_results.items() if not passed and name not in baseline)
        missing = sorted(baseline - suite_results.keys())
        print(f"Active suite: {sum(suite_results.values())}/{len(suite_results)} scenarios passed")
        if unexpected or missing:
            if unexpected:
                print(f"Unexpected failures: {', '.join(unexpected)}")
            if missing:
                print(f"Baseline scenarios missing from the suite: {', '.join(missing)}")
            print(suite_output[-2000:])
            return 1
        if suite_status and all(suite_results.values()):
            print("The suite returned a failure status without a failed scenario")
            print(suite_output[-2000:])
            return 1

    print("\nChecking known intermittent notification scenarios for a stale baseline")
    stale = []
    for scenario in sorted(baseline):
        failures = 0
        for _ in range(BASELINE_RETRIES):
            with tempfile.TemporaryDirectory(prefix="mcp-conformance-case-") as output:
                output_dir = Path(output)
                run_cli(args.url, output_dir, scenario)
                results = read_suite_results(output_dir)
                if results.get(scenario) is not True:
                    failures += 1
        print(f"{scenario}: failed {failures}/{BASELINE_RETRIES} repeated runs")
        if failures == 0:
            stale.append(scenario)
    if stale:
        print(f"Baseline is stale; remove consistently passing scenarios: {', '.join(stale)}")
        return 1

    print("Conformance suite passed; only baseline notification failures were observed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
