"""
Integration test: a Sigma DB rule is loaded and matched at runtime.

Counterpart to test_gtfobins_db_pattern_detection for the Sigma load path
(internal/monitor/sigma.go), which otherwise has no integration coverage. Sets up
a synthetic Sigma linux/process_creation rule, starts the monitoring daemon with
HOME pointing at that clone, execs a process whose command line contains the
rule's distinctive token, and asserts the daemon raises a threat referencing the
Sigma rule title.

If this fails, the end-to-end Sigma loading/matching path is broken (the daemon
is silently ignoring the rules clone).
"""

import hashlib
import json
import os
import subprocess
import time

import pytest

# Minimal linux/process_creation rule (level high so it is loaded — see
# compileSigmaFile, which only keeps high/critical). The distinctive token
# "coi-sigma-canary-zzz" in CommandLine is what we trigger below.
_CANARY_RULE = """\
title: COI Sigma Canary
id: 00000000-0000-0000-0000-0000000005a1
logsource:
    category: process_creation
    product: linux
detection:
    selection:
        CommandLine|contains: 'coi-sigma-canary-zzz'
    condition: selection
level: high
"""


def _container_name(workspace):
    abs_path = os.path.abspath(workspace)
    h = hashlib.sha256(abs_path.encode()).hexdigest()[:8]
    prefix = os.getenv("COI_CONTAINER_PREFIX", "coi-")
    return f"{prefix}{h}-1"


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


def test_sigma_db_rule_loaded_and_detected(tmp_path, coi_binary):
    """A Sigma DB rule triggers a threat event referencing its title."""
    fake_home = tmp_path / "home"
    coi_dir = fake_home / ".coi"
    rules_dir = coi_dir / "sigma" / "rules" / "linux" / "process_creation"
    rules_dir.mkdir(parents=True)
    (rules_dir / "canary.yml").write_text(_CANARY_RULE)

    coi_dir.mkdir(parents=True, exist_ok=True)
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
    ready = False
    for _ in range(30):
        if _container_state(container_name) == "Running":
            ready = True
            break
        time.sleep(1)

    if not ready:
        proc.terminate()
        proc.wait(timeout=5)
        pytest.skip(f"Container {container_name} did not reach Running state")

    # cmdline contains the rule's CommandLine|contains token → Sigma match.
    subprocess.Popen(
        [
            "incus",
            "exec",
            container_name,
            "--",
            "bash",
            "-c",
            "exec -a 'evilproc coi-sigma-canary-zzz' sleep 30",
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
                text = ev.get("description", "") + ev.get("title", "")
                if "COI Sigma Canary" in text or "Sigma rule matched" in text:
                    detected = True
                    break
        if detected:
            break

    proc.terminate()
    proc.wait(timeout=5)
    _cleanup(container_name, coi_binary)

    assert detected, (
        "No threat event referencing the Sigma canary rule found in "
        f"{log_path} — the Sigma DB rule was not loaded or not matched"
    )
