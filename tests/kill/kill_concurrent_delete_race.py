"""
Regression test for the `coi kill` concurrent-delete race (#623 CI flake).

Deleting an instance can lose a race to a concurrent delete of the SAME instance
— the session's own ephemeral cleanup, or another `coi kill`. Incus answers the
loser with an operation-collision error (e.g. "A matching non-reusable operation
has now succeeded"). The instance IS gone, but `coi kill` used to report that as
a hard failure ("Warning: Failed to delete ...", non-zero exit) about a container
it had just killed.

This fires several `coi kill` at one container simultaneously to provoke the
race, and asserts that every invocation reports success and the container ends up
gone — none should surface the operation-collision error as a failure. The fix
re-checks the instance's actual existence after a delete error instead of parsing
incus's (version/timing-dependent) error text.
"""

import subprocess
import time

from support.helpers import calculate_container_name

CONCURRENCY = 5


def test_kill_concurrent_delete_race(coi_binary, cleanup_containers, workspace_dir):
    container_name = calculate_container_name(workspace_dir, 1)

    # === Launch a container to kill ===
    launch = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert launch.returncode == 0, f"Container launch should succeed. stderr: {launch.stderr}"
    time.sleep(3)

    # === Fire N kills at the SAME container simultaneously to race the delete ===
    procs = [
        subprocess.Popen(
            [coi_binary, "kill", container_name],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        for _ in range(CONCURRENCY)
    ]
    results = []
    for p in procs:
        out, err = p.communicate(timeout=60)
        results.append((p.returncode, (out or "") + (err or "")))

    # === No kill may report the delete-race as a failure ===
    for i, (rc, output) in enumerate(results):
        assert "matching non-reusable operation" not in output, (
            f"kill #{i} surfaced the concurrent-delete race as a failure:\n{output}"
        )
        assert rc == 0, (
            f"kill #{i} exited {rc}; a concurrent delete of an already-removed "
            f"container must count as killed, not fail:\n{output}"
        )

    # === The container must actually be gone (`exists` exits non-zero when absent) ===
    exists = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert exists.returncode != 0, (
        f"container {container_name} should be gone after kill, but `exists` reported present.\n"
        f"{exists.stdout}{exists.stderr}"
    )
