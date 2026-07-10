"""
Test for coi list status filters with containers in mixed states.

Tests that:
1. Seed one running and one stopped container
2. --running shows only the running container
3. --stopped shows only the stopped container
4. --status <state> behaves like the matching shorthand (JSON format)
5. No filter flag still shows both (backward compatible)
6. --all keeps the Saved Sessions section even when the filter hides containers
"""

import json
import subprocess
import time

from support.helpers import calculate_container_name


def test_list_status_filter_mixed(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi list status filters narrow output to matching containers.

    Flow:
    1. Launch two containers, stop the second one
    2. Run coi list with --running, --stopped, --status, and no filter
    3. Verify each variant shows exactly the matching containers
    4. Cleanup
    """
    running_name = calculate_container_name(workspace_dir, 1)
    stopped_name = calculate_container_name(workspace_dir, 2)

    # === Phase 1: Seed containers in mixed states ===

    for name in (running_name, stopped_name):
        result = subprocess.run(
            [coi_binary, "container", "launch", "coi-default", name],
            capture_output=True,
            text=True,
            timeout=120,
        )
        assert result.returncode == 0, f"Launch of {name} should succeed. stderr: {result.stderr}"

    time.sleep(3)

    result = subprocess.run(
        [coi_binary, "container", "stop", stopped_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # === Phase 2: --running shows only the running container ===

    result = subprocess.run(
        [coi_binary, "list", "--running"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List --running should succeed. stderr: {result.stderr}"
    assert running_name in result.stdout, (
        f"Running container should appear with --running. Got:\n{result.stdout}"
    )
    assert stopped_name not in result.stdout, (
        f"Stopped container should be hidden with --running. Got:\n{result.stdout}"
    )

    # === Phase 3: --stopped shows only the stopped container ===

    result = subprocess.run(
        [coi_binary, "list", "--stopped"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List --stopped should succeed. stderr: {result.stderr}"
    assert stopped_name in result.stdout, (
        f"Stopped container should appear with --stopped. Got:\n{result.stdout}"
    )
    assert running_name not in result.stdout, (
        f"Running container should be hidden with --stopped. Got:\n{result.stdout}"
    )

    # === Phase 4: --status filters JSON output the same way ===

    result = subprocess.run(
        [coi_binary, "list", "--status", "running", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List --status running should succeed. stderr: {result.stderr}"

    data = json.loads(result.stdout)
    assert "active_containers" in data, "Missing 'active_containers' key"
    names = {c["name"] for c in data["active_containers"]}
    assert running_name in names, f"Running container missing from JSON output. Got: {names}"
    assert stopped_name not in names, f"Stopped container leaked into JSON output. Got: {names}"

    # Every listed container must actually be Running (other test containers
    # may coexist on the host, but none of them may violate the filter)
    for c in data["active_containers"]:
        assert c["status"] == "Running", (
            f"Container {c['name']} has status {c['status']}, filter should only pass Running"
        )

    # === Phase 5: No filter still shows both (backward compatible) ===

    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List should succeed. stderr: {result.stderr}"
    assert running_name in result.stdout, (
        f"Running container should appear without filters. Got:\n{result.stdout}"
    )
    assert stopped_name in result.stdout, (
        f"Stopped container should appear without filters. Got:\n{result.stdout}"
    )

    # === Phase 6: --all keeps the Saved Sessions section under a filter ===

    result = subprocess.run(
        [coi_binary, "list", "--all", "--stopped"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List --all --stopped should succeed. stderr: {result.stderr}"
    assert "Saved Sessions:" in result.stdout, (
        f"Saved Sessions section should survive status filtering. Got:\n{result.stdout}"
    )
    assert running_name not in result.stdout, (
        f"Running container should be hidden with --all --stopped. Got:\n{result.stdout}"
    )

    # === Phase 7: Cleanup ===

    for name in (running_name, stopped_name):
        subprocess.run(
            [coi_binary, "container", "delete", name, "--force"],
            capture_output=True,
            timeout=30,
        )
