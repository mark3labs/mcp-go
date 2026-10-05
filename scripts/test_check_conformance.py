import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import check_conformance


class CheckConformanceTests(unittest.TestCase):
    def test_active_scenarios_are_counted_from_cli_output(self):
        output = "Running active suite (2 scenarios) against http://localhost\n\n=== Running scenario: ping ===\n=== Running scenario: tools-list ==="

        self.assertEqual(
            check_conformance.read_active_scenarios(output),
            (2, ["ping", "tools-list"]),
        )

    def test_runner_error_is_not_treated_as_a_scenario_failure(self):
        with tempfile.TemporaryDirectory() as output:
            result_dir = Path(output) / "server-ping-2026-10-05T00-00-00-000Z"
            result_dir.mkdir()
            (result_dir / "checks.json").write_text(
                json.dumps(
                    [
                        {
                            "status": "FAILURE",
                            "description": "Failed to run scenario",
                            "errorMessage": "connection refused",
                        }
                    ]
                ),
                encoding="utf-8",
            )

            self.assertEqual(
                check_conformance.read_suite_results(Path(output)),
                {"ping": (False, True)},
            )

    def test_failed_assertion_is_not_a_runner_error(self):
        with tempfile.TemporaryDirectory() as output:
            result_dir = Path(output) / "server-ping-2026-10-05T00-00-00-000Z"
            result_dir.mkdir()
            (result_dir / "checks.json").write_text(
                json.dumps(
                    [
                        {
                            "status": "FAILURE",
                            "description": "Expected check failed",
                            "errorMessage": "notification was not received",
                        }
                    ]
                ),
                encoding="utf-8",
            )

            self.assertEqual(
                check_conformance.read_suite_results(Path(output)),
                {"ping": (False, False)},
            )

    @patch("check_conformance.subprocess.run", side_effect=check_conformance.subprocess.TimeoutExpired("npx", 120))
    def test_cli_timeout_is_a_runner_error(self, run):
        with self.assertRaisesRegex(check_conformance.ConformanceRunnerError, "timed out"):
            check_conformance.run_cli("http://localhost", Path("/tmp/results"))

        self.assertEqual(run.call_args.kwargs["timeout"], check_conformance.CLI_TIMEOUT_SECONDS)


if __name__ == "__main__":
    unittest.main()
