"""
End-to-end tests for `coi top` (#707).

Fast tests exercise help text and argument validation (no container needed).
The container-backed tests launch a real coi container and assert that `coi
top` surfaces it (and its processes) with resource fields, resolved to the
container context.
"""

import json
import subprocess

from support.helpers import calculate_container_name, wait_for_container_started

# --- Fast tests (no container required) -------------------------------------


def test_top_help(coi_binary):
    """`coi top --help` documents the views, key flags, and the kill hint."""
    result = subprocess.run(
        [coi_binary, "top", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"help should exit 0. stderr: {result.stderr}"
    out = result.stdout + result.stderr
    for needle in ("CPU%", "--watch", "--procs", "--sort", "sudo kill"):
        assert needle in out, f"expected {needle!r} in `coi top --help`, got:\n{out}"


def test_top_invalid_sort_rejected(coi_binary):
    """An unknown --sort key is rejected (not silently defaulted to cpu)."""
    result = subprocess.run(
        [coi_binary, "top", "--sort", "bogus"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "invalid --sort should be a usage error"
    assert "invalid --sort" in (result.stdout + result.stderr).lower()


def test_top_procs_sort_disk_rejected(coi_binary):
    """disk/net are container-only sort keys; rejected in the process view."""
    result = subprocess.run(
        [coi_binary, "top", "--procs", "--sort", "disk"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "disk is not a valid process sort key"
    assert "invalid --sort" in (result.stdout + result.stderr).lower()


def test_top_json_with_watch_rejected(coi_binary):
    """--json and --watch are mutually exclusive."""
    result = subprocess.run(
        [coi_binary, "top", "--watch", "2", "--json"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "--json with --watch should be a usage error"
    combined = (result.stdout + result.stderr).lower()
    assert "--json" in combined and "--watch" in combined


def test_top_invalid_interval_rejected(coi_binary):
    """A non-positive --interval is a usage error."""
    result = subprocess.run(
        [coi_binary, "top", "--interval", "0"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "--interval 0 should be a usage error"
    assert "interval" in (result.stdout + result.stderr).lower()


# --- Container-backed tests -------------------------------------------------


def test_top_lists_running_container(coi_binary, cleanup_containers, workspace_dir):
    """`coi top --json` includes a launched container with resource fields."""
    container_name = calculate_container_name(workspace_dir, 1)
    launch = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert launch.returncode == 0, f"launch failed. stderr: {launch.stderr}"
    try:
        wait_for_container_started(coi_binary, container_name)

        result = subprocess.run(
            [coi_binary, "top", "--json", "-i", "0.5"],
            capture_output=True,
            text=True,
            timeout=60,
        )
        assert result.returncode == 0, f"top --json failed. stderr: {result.stderr}"

        rows = json.loads(result.stdout)
        assert isinstance(rows, list), "container view --json must be a JSON array"
        match = next((r for r in rows if r.get("name") == container_name), None)
        assert match is not None, (
            f"launched container {container_name} not in `coi top` output: {rows}"
        )
        # Resource fields present (values may be ~0 on an idle container).
        for field in ("cpu_percent", "memory_mb", "disk_read_mb_per_sec", "net_rx_mb_per_sec"):
            assert field in match, f"missing {field} in row: {match}"
        assert match["memory_mb"] > 0, "a running container should report memory usage"
    finally:
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
            check=False,
        )


def test_top_processes_for_container(coi_binary, cleanup_containers, workspace_dir):
    """`coi top <container> --json` lists processes with host PIDs."""
    container_name = calculate_container_name(workspace_dir, 1)
    launch = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert launch.returncode == 0, f"launch failed. stderr: {launch.stderr}"
    try:
        wait_for_container_started(coi_binary, container_name)

        result = subprocess.run(
            [coi_binary, "top", container_name, "--json", "-i", "0.5"],
            capture_output=True,
            text=True,
            timeout=60,
        )
        assert result.returncode == 0, f"top <container> --json failed. stderr: {result.stderr}"

        rows = json.loads(result.stdout)
        assert isinstance(rows, list) and len(rows) > 0, (
            f"a running container must have at least its init process: {rows}"
        )
        first = rows[0]
        for field in ("pid", "cpu_percent", "memory_mb", "command"):
            assert field in first, f"missing {field} in process row: {first}"
        assert first["pid"] > 0, "process rows must carry a (host-side) PID"
        assert first["container"] == container_name, "process row should name its container"
    finally:
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
            check=False,
        )
