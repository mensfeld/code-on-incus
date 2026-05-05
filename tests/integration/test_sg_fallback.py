"""
Integration tests for sg fallback behavior.

Tests that COI works correctly when `sg` is unavailable, falling back to
running incus directly (same as the macOS code path).

These tests verify:
1. `coi container launch` works without `sg` (via PATH manipulation)
2. `coi container exec` works without `sg`
3. `coi image list` works without `sg`
4. Normal operation with `sg` available is unaffected
"""

import os
import subprocess
import tempfile
import time

import pytest

from support.helpers import (
    calculate_container_name,
)


def _env_without_sg():
    """Return an environment where `sg` is not discoverable via PATH.

    Creates a temporary directory with symlinks to all PATH entries'
    binaries EXCEPT `sg`, effectively hiding it from exec.LookPath.
    We achieve this more simply by prepending a directory that shadows
    `sg` with a non-executable file, but the cleanest approach is just
    to remove dirs that contain `sg` ... except that's fragile.

    Instead we set a special env var and use a wrapper: we simply
    restrict PATH to not include the directory containing `sg`.
    """
    import shutil

    sg_path = shutil.which("sg")
    if sg_path is None:
        pytest.skip("sg is not available on this system — fallback is already the default path")

    # Remove the directory containing sg from PATH
    sg_dir = os.path.dirname(sg_path)
    env = os.environ.copy()
    path_dirs = env.get("PATH", "").split(os.pathsep)
    filtered = [d for d in path_dirs if os.path.normpath(d) != os.path.normpath(sg_dir)]

    # Make sure we didn't accidentally remove ALL of PATH
    assert len(filtered) > 0, "Filtering sg from PATH removed all entries"

    # Verify incus is still reachable on the filtered PATH
    incus_found = any(
        os.path.isfile(os.path.join(d, "incus")) and os.access(os.path.join(d, "incus"), os.X_OK)
        for d in filtered
    )
    if not incus_found:
        pytest.skip("incus lives in the same directory as sg — cannot test independently")

    env["PATH"] = os.pathsep.join(filtered)
    return env


def test_launch_without_sg(coi_binary, cleanup_containers, workspace_dir):
    """Container launch succeeds when sg is not on PATH."""
    env = _env_without_sg()
    container_name = calculate_container_name(workspace_dir, 1)

    # Launch container without sg
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
        env=env,
    )

    assert result.returncode == 0, f"Launch should succeed without sg. stderr: {result.stderr}"

    time.sleep(3)

    # Verify container is running (use normal env for checking)
    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Container {container_name} should be running"

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_exec_without_sg(coi_binary, cleanup_containers, workspace_dir):
    """Command execution works when sg is not on PATH."""
    env = _env_without_sg()
    container_name = calculate_container_name(workspace_dir, 1)

    # Launch container (with sg so we know it works)
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Launch failed. stderr: {result.stderr}"
    time.sleep(3)

    # Exec command without sg
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "echo", "sg-fallback-test"],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )

    assert result.returncode == 0, f"Exec should succeed without sg. stderr: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "sg-fallback-test" in combined, f"Output should contain echo text. Got:\n{combined}"

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_image_list_without_sg(coi_binary):
    """Image listing works when sg is not on PATH."""
    env = _env_without_sg()

    result = subprocess.run(
        [coi_binary, "image", "list", "--all"],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )

    # Should succeed (exit 0) regardless of whether images exist
    assert result.returncode == 0, f"Image list should succeed without sg. stderr: {result.stderr}"


def test_launch_with_sg(coi_binary, cleanup_containers, workspace_dir):
    """Container launch still works normally when sg IS available (regression check)."""
    import shutil

    if shutil.which("sg") is None:
        pytest.skip("sg is not available — nothing to regression-test")

    container_name = calculate_container_name(workspace_dir, 1)

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Launch with sg should succeed. stderr: {result.stderr}"

    time.sleep(3)

    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Container {container_name} should be running"

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
