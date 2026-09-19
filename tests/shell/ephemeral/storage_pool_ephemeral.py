"""
Test that `coi shell` honours [container] storage_pool (#726).

`coi run` and `coi build` honored `[container] storage_pool`, but `coi shell` -
the primary interactive command - dropped it and always landed the container on
the Incus default pool (session.SetupOptions had no StoragePool field, so
phases_shell never threaded it into `incus init`).

This launches an ephemeral shell session with `storage_pool` pointed at a
freshly-created pool and asserts the container's root device lives on THAT pool,
not the default. It fails before the fix (container on the default pool) and
passes after.
"""

import json
import os
import subprocess
import time

import pytest
from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    create_storage_pool,
    delete_storage_pool,
    is_incus_permission_error,
    spawn_coi,
    wait_for_container_ready,
)


def _container_pool(coi_binary, workspace_dir, container_name):
    """Return the storage pool of container_name via `coi list --format json`."""
    result = subprocess.run(
        [coi_binary, "list", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, f"coi list failed: {result.stderr}"
    data = json.loads(result.stdout)
    for container in data.get("active_containers", []):
        if container.get("name") == container_name:
            return container.get("pool")
    return None


def test_shell_honors_storage_pool_ephemeral(coi_binary, cleanup_containers, workspace_dir):
    """coi shell should place the container on the configured storage_pool."""
    pool_name = f"coi-test-shellpool-{os.urandom(4).hex()}"
    ok, err = create_storage_pool(pool_name)
    if not ok:
        if is_incus_permission_error(err):
            pytest.skip(f"No permission to create storage pool {pool_name}: {err}")
        pytest.fail(f"Failed to create temp pool {pool_name}: {err}")

    container_name = calculate_container_name(workspace_dir, 1)

    # Project config pointing this workspace's containers at the temp pool.
    coi_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(coi_dir, exist_ok=True)
    with open(os.path.join(coi_dir, "config.toml"), "w") as handle:
        handle.write(f'[container]\nimage = "coi-default"\nstorage_pool = "{pool_name}"\n')

    child = None
    try:
        child = spawn_coi(
            coi_binary,
            ["shell", f"--workspace={workspace_dir}"],
            cwd=workspace_dir,
            env={"COI_USE_DUMMY": "1"},
            timeout=120,
        )
        # Container is created during setup, before the session starts.
        wait_for_container_ready(child, timeout=90)
        time.sleep(2)

        pool = _container_pool(coi_binary, workspace_dir, container_name)
        assert pool == pool_name, (
            f"coi shell should place the container on '{pool_name}', "
            f"but it landed on '{pool}' (#726)"
        )
    finally:
        if child is not None:
            try:
                child.send("exit")
                time.sleep(0.3)
                child.send("\x0d")
                child.expect(EOF, timeout=30)
            except (TIMEOUT, Exception):
                pass
            try:
                child.close(force=True)
            except Exception:
                pass
        time.sleep(2)
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
            check=False,
        )
        delete_storage_pool(pool_name)
