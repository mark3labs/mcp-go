#!/usr/bin/env python3
"""Run MCP server conformance checks with a retry-aware failure baseline."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import tempfile
from pathlib import Path

CLI_VERSION = "0.1.16"
BASELINE_RETRIES = 10
CLI_TIMEOUT_SECONDS = 120


class ConformanceRunnerError(RuntimeError):
    """Raised when the conformance CLI cannot complete a scenario run."""


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
    try:
        result = subprocess.run(
            command,
            check=False,
            capture_output=True,
            text=True,
            timeout=CLI_TIMEOUT_SECONDS,
        )
    except subprocess.TimeoutExpired as error:
        raise ConformanceRunnerError(
            f"conformance CLI timed out after {CLI_TIMEOUT_SECONDS} seconds"
        ) from error
    return result.returncode, result.stdout + result.stderr


def read_suite_results(output_dir: Path) -> dict[str, tuple[bool, bool]]:
    """Return scenario pass and runner-error status from the CLI result files."""
    results = {}
    for checks_file in output_dir.glob("server-*/checks.json"):
        checks = json.loads(checks_file.read_text(encoding="utf-8"))
        scenario = scenario_from_result_dir(checks_file.parent)
        passed = bool(checks) and all(check.get("status") in {"SUCCESS", "INFO"} for check in checks)
        runner_error = any(check.get("description") == "Failed to run scenario" for check in checks)
        results[scenario] = (passed, runner_error)
    return results


def scenario_from_result_dir(result_dir: Path) -> str:
    name = result_dir.name.removeprefix("server-")
    match = re.fullmatch(
        r"(?P<scenario>.+)-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}(?:-\d{3})?Z",
        name,
    )
    if match is None:
        raise ValueError(f"unrecognized conformance result directory: {result_dir.name}")
    return match.group("scenario")


def read_active_scenarios(output: str) -> tuple[int | None, list[str]]:
    count_match = re.search(r"Running active suite \((\d+) scenarios?\)", output)
    scenarios = re.findall(r"^=== Running scenario: (.+) ===$", output, re.MULTILINE)
    return (int(count_match.group(1)) if count_match else None, scenarios)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", required=True)
    parser.add_argument("--baseline", type=Path, required=True)
    args = parser.parse_args()
    baseline = set(read_server_baseline(args.baseline))

    with tempfile.TemporaryDirectory(prefix="mcp-conformance-suite-") as output:
        suite_dir = Path(output)
        print("Running the official active server conformance suite")
        try:
            suite_status, suite_output = run_cli(args.url, suite_dir)
            suite_results = read_suite_results(suite_dir)
        except (ConformanceRunnerError, OSError, ValueError) as error:
            print(f"Conformance runner failure: {error}")
            return 1
        if not suite_results:
            print("The conformance runner produced no scenario results")
            return 1
        expected_count, started_scenarios = read_active_scenarios(suite_output)
        if expected_count is None or len(started_scenarios) != expected_count or len(set(started_scenarios)) != expected_count:
            print("The conformance runner did not report starting every active scenario")
            print(suite_output[-2000:])
            return 1
        missing_results = sorted(set(started_scenarios) - suite_results.keys())
        unexpected_results = sorted(suite_results.keys() - set(started_scenarios))
        runner_errors = sorted(name for name, (_, runner_error) in suite_results.items() if runner_error)
        unexpected = sorted(
            name for name, (passed, _) in suite_results.items() if not passed and name not in baseline
        )
        baseline_missing = sorted(baseline - suite_results.keys())
        passed_count = sum(passed for passed, _ in suite_results.values())
        print(f"Active suite: {passed_count}/{expected_count} scenarios passed")
        if missing_results or unexpected_results or runner_errors or unexpected or baseline_missing:
            if missing_results:
                print(f"Scenarios without result files: {', '.join(missing_results)}")
            if unexpected_results:
                print(f"Results for scenarios not started by the active suite: {', '.join(unexpected_results)}")
            if runner_errors:
                print(f"Scenario runner errors: {', '.join(runner_errors)}")
            if unexpected:
                print(f"Unexpected failures: {', '.join(unexpected)}")
            if baseline_missing:
                print(f"Baseline scenarios missing from the suite: {', '.join(baseline_missing)}")
            print(suite_output[-2000:])
            return 1
        has_failed_checks = any(not passed for passed, _ in suite_results.values())
        if suite_status and not has_failed_checks:
            print("The suite returned a failure status without any failed scenario checks")
            print(suite_output[-2000:])
            return 1
        if not suite_status and has_failed_checks:
            print("The suite returned success despite failed scenario checks")
            print(suite_output[-2000:])
            return 1

    print("\nChecking known intermittent notification scenarios for a stale baseline")
    stale = []
    for scenario in sorted(baseline):
        failures = 0
        for _ in range(BASELINE_RETRIES):
            with tempfile.TemporaryDirectory(prefix="mcp-conformance-case-") as output:
                output_dir = Path(output)
                try:
                    status, output_text = run_cli(args.url, output_dir, scenario)
                    results = read_suite_results(output_dir)
                except (ConformanceRunnerError, OSError, ValueError) as error:
                    print(f"Conformance runner failure for {scenario}: {error}")
                    return 1
                result = results.get(scenario)
                if result is None:
                    print(f"Conformance runner produced no result for {scenario}")
                    print(output_text[-2000:])
                    return 1
                passed, runner_error = result
                if runner_error:
                    print(f"Conformance runner failed while executing {scenario}")
                    print(output_text[-2000:])
                    return 1
                if status and passed:
                    print(f"Conformance runner returned failure without failed checks for {scenario}")
                    print(output_text[-2000:])
                    return 1
                if not status and not passed:
                    print(f"Conformance runner returned success despite failed checks for {scenario}")
                    print(output_text[-2000:])
                    return 1
                if not passed:
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
