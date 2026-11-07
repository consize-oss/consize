import contextlib
import io
import json
import subprocess
import unittest
from unittest.mock import patch

import preflight


class PreflightTests(unittest.TestCase):
    def test_nonzero_exit_blocks_without_exposing_command_output(self):
        response = subprocess.CompletedProcess(["tool"], 1, b"synthetic-sensitive-output", b"private driver details")
        with patch("preflight.subprocess.run", return_value=response):
            result = preflight.check(["tool"])
        self.assertEqual(result["status"], "blocked")
        self.assertEqual(result["exit_code"], 1)
        self.assertNotIn("synthetic-sensitive-output", json.dumps(result))
        self.assertNotIn("private driver details", json.dumps(result))

    def test_timeout_and_missing_tool_are_blocked(self):
        for error, reason in [(subprocess.TimeoutExpired(["tool"], 15), "timeout"), (FileNotFoundError("private path"), "tool_unavailable")]:
            with self.subTest(reason=reason), patch("preflight.subprocess.run", side_effect=error):
                result = preflight.check(["tool"])
                self.assertEqual(result["status"], "blocked")
                self.assertEqual(result["reason"], reason)

    def test_one_missing_prerequisite_cannot_report_ready(self):
        for statuses, expected in [(["passed", "blocked", "passed"], 2), (["passed"] * 3, 0)]:
            with self.subTest(statuses=statuses), patch("sys.argv", ["preflight.py"]), patch("preflight.check", side_effect=[{"status": s} for s in statuses]):
                output = io.StringIO()
                with contextlib.redirect_stdout(output):
                    actual = preflight.main()
                self.assertEqual(actual, expected)
                self.assertEqual(json.loads(output.getvalue())["status"], "ready" if expected == 0 else "blocked")


if __name__ == "__main__":
    unittest.main()
