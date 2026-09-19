"""
#674 follow-up: on a persistent container reused across sessions, coi must
reconcile the auto-context block in ~/.claude/CLAUDE.md WITHOUT growing the file
AND without dropping the user's own content.

The existing no-growth test (claude_md_no_growth_persistent.py) only exercises a
CLAUDE.md that coi itself wrote (block only) — CI has no host ~/.claude. This one
seeds a real host CLAUDE.md (via HOME override), so the reconcile runs against
genuine user content read back through a real `cat`, which the byte-exact unit
fake can't validate: after N sessions the user content survives exactly once and
the coi block appears exactly once.
"""

import os
import subprocess
import time

from support.helpers import (
    calculate_container_name,
    write_workspace_container_config,
)

USER_MARKER = "USER-CLAUDE-MD-CONTENT-708"
CLAUDE_MD = "/home/code/.claude/CLAUDE.md"


def _incus_run(*args):
    return subprocess.run(
        ["incus", "--project", "default", *args],
        capture_output=True,
        text=True,
        timeout=60,
    )


def _container_exists(name):
    return _incus_run("info", name).returncode == 0


def test_claude_md_preserves_host_content_across_persistent_sessions(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    write_workspace_container_config(workspace_dir, persistent=True)

    # Fake host home with a user-authored ~/.claude/CLAUDE.md to seed + reconcile.
    fake_home = tmp_path / "fake_home"
    (fake_home / ".claude").mkdir(parents=True)
    (fake_home / ".claude" / "CLAUDE.md").write_text(
        f"# My instructions\n{USER_MARKER}\nkeep me across sessions\n"
    )
    env = {**os.environ, "HOME": str(fake_home), "COI_USE_DUMMY": "1"}

    container_name = calculate_container_name(workspace_dir, 1)
    try:
        # Session 1: create; host CLAUDE.md seeded + coi block injected.
        first = subprocess.run(
            [coi_binary, "shell", "--background"],
            cwd=workspace_dir,
            capture_output=True,
            text=True,
            timeout=180,
            env=env,
        )
        assert first.returncode == 0, f"session 1 should succeed. stderr: {first.stderr}"
        time.sleep(2)
        assert _container_exists(container_name), f"expected {container_name} after session 1"

        # Sessions 2 & 3: stop, then re-enter the SAME container (reconcile path).
        for i in range(2):
            stop = subprocess.run(
                [coi_binary, "container", "stop", container_name],
                capture_output=True,
                text=True,
                timeout=60,
            )
            assert stop.returncode == 0, f"stop before resume {i + 1}: {stop.stderr}"
            time.sleep(1)
            resumed = subprocess.run(
                [coi_binary, "shell", "--container", container_name, "--background"],
                cwd=workspace_dir,
                capture_output=True,
                text=True,
                timeout=180,
                env=env,
            )
            assert resumed.returncode == 0, f"resume {i + 1}: {resumed.stderr}"
            time.sleep(2)

        assert not _container_exists(calculate_container_name(workspace_dir, 2)), (
            "sessions forked a new slot instead of reusing the same container"
        )

        # `coi container exec` routes the command's stdout to ITS stderr.
        exec_res = subprocess.run(
            [coi_binary, "container", "exec", container_name, "--", "cat", CLAUDE_MD],
            capture_output=True,
            text=True,
            timeout=30,
        )
        content = exec_res.stdout + exec_res.stderr

        user_copies = content.count(USER_MARKER)
        block_copies = content.count("# COI Sandbox Environment")
        assert user_copies == 1, (
            f"user CLAUDE.md content must survive reconcile exactly once, found "
            f"{user_copies}. Content head: {content[:400]!r}"
        )
        assert block_copies == 1, (
            f"the coi sandbox block must appear exactly once after 3 sessions, found "
            f"{block_copies} — it is growing (#674). Content head: {content[:400]!r}"
        )
    finally:
        _incus_run("delete", container_name, "--force")
