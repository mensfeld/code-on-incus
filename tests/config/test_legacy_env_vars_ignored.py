"""
0.10 removed the env-var config layer entirely (#559 A8). `COI_LIMIT_*` is
already covered (tests/limits/test_limits_flags.py); the legacy
`CLAUDE_ON_INCUS_*` overrides were not. These prove they are now IGNORED, not
honored as a hidden config source.
"""

import os
import subprocess

from support.helpers import calculate_container_name, get_container_list


def _run(coi_binary, workspace_dir, extra_env, argv, timeout=180):
    env = os.environ.copy()
    env.update(extra_env)
    return subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, *argv],
        capture_output=True,
        text=True,
        timeout=timeout,
        env=env,
    )


def test_claude_on_incus_image_ignored(coi_binary, cleanup_containers, workspace_dir):
    """CLAUDE_ON_INCUS_IMAGE must be ignored: the run uses the default image and
    succeeds. If the env var were still honored the run would try a nonexistent
    image and fail."""
    r = _run(
        coi_binary,
        workspace_dir,
        {"CLAUDE_ON_INCUS_IMAGE": "coi-legacy-env-nonexistent-xyz"},
        ["--", "echo", "IMG-ENV-IGNORED"],
    )
    combined = r.stdout + r.stderr
    assert r.returncode == 0, (
        f"CLAUDE_ON_INCUS_IMAGE must be ignored (default image used), but the run "
        f"failed — the legacy env override may still be honored.\n{combined}"
    )
    assert "IMG-ENV-IGNORED" in combined, combined


def test_claude_on_incus_persistent_ignored(coi_binary, cleanup_containers, workspace_dir):
    """CLAUDE_ON_INCUS_PERSISTENT=true must be ignored: the run stays ephemeral,
    so no container for this workspace survives afterwards. If the env var were
    honored the container would be kept (stopped) and show up in `incus list`."""
    r = _run(
        coi_binary,
        workspace_dir,
        {"CLAUDE_ON_INCUS_PERSISTENT": "true"},
        ["--", "echo", "PERSIST-ENV-IGNORED"],
    )
    combined = r.stdout + r.stderr
    assert r.returncode == 0, combined
    assert "PERSIST-ENV-IGNORED" in combined, combined

    live = get_container_list()  # all containers (running + stopped)
    survivors = [calculate_container_name(workspace_dir, slot) for slot in range(1, 11)]
    leaked = [name for name in survivors if name in live]
    assert not leaked, (
        f"CLAUDE_ON_INCUS_PERSISTENT=true must be ignored (run stays ephemeral), "
        f"but a container survived: {leaked}\nlive containers:\n{live}"
    )
