#!/usr/bin/env python3
"""Bounded, read-only prerequisites for the isolated CON-56 live lab."""
import argparse
import json
import subprocess
import sys
import time


def check(command, timeout=15):
    start = time.monotonic()
    try:
        result = subprocess.run(command, capture_output=True, timeout=timeout, check=False)
        # Do not publish arbitrary tool output: kubectl/driver errors may contain
        # local connection details. Exit/timeout and selected target are sufficient.
        return {"command": command, "status": "passed" if result.returncode == 0 else "blocked",
                "exit_code": result.returncode, "duration_seconds": round(time.monotonic() - start, 2)}
    except (subprocess.TimeoutExpired, OSError) as error:
        return {"command": command, "status": "blocked",
                "reason": "timeout" if isinstance(error, subprocess.TimeoutExpired) else "tool_unavailable",
                "duration_seconds": round(time.monotonic() - start, 2)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.parse_args()
    results = [
        check(["docker", "version", "--format", "{{.Server.Version}}"]),
        check(["kubectl", "--context", "docker-desktop", "--request-timeout=10s", "get", "deployment",
               "checkout-api", "-n", "consize-demo", "-o", "name"]),
        check(["curl", "--fail", "--silent", "--show-error", "--max-time", "5",
               "http://127.0.0.1:9090/-/ready"]),
    ]
    passed = all(result["status"] == "passed" for result in results)
    print(json.dumps({"status": "ready" if passed else "blocked", "checks": results}, indent=2))
    return 0 if passed else 2


if __name__ == "__main__":
    sys.exit(main())
