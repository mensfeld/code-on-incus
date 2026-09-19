"""
Test that `coi shell` honors [limits.disk] tmpfs_size (#733).

SetTmpfsSize used an invalid `disk source=tmpfs` device, so /tmp always kept its
default size. It now mounts a sized tmpfs via a raw.lxc mount entry (applied at
boot, which is where the shell path sets it). This launches an ephemeral shell
with tmpfs_size=256MiB, writes the in-container /tmp size to a workspace file
(which persists to the host), and asserts it is ~256MiB — not the default.
"""

import time
from pathlib import Path

from pexpect import EOF, TIMEOUT

from support.helpers import (
    make_workspace_writable,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
)


def test_shell_honors_tmpfs_size_ephemeral(coi_binary, cleanup_containers, workspace_dir):
    """coi shell must size /tmp from [limits.disk] tmpfs_size."""
    make_workspace_writable(workspace_dir)
    coi_dir = Path(workspace_dir) / ".coi"
    coi_dir.mkdir(exist_ok=True)
    (coi_dir / "config.toml").write_text('[limits.disk]\ntmpfs_size = "256MiB"\n')

    result_file = Path(workspace_dir) / "tmpfs_size_kb.txt"

    child = spawn_coi(
        coi_binary,
        ["shell", f"--workspace={workspace_dir}"],
        cwd=workspace_dir,
        env={"COI_USE_DUMMY": "1"},
        timeout=120,
    )
    try:
        wait_for_container_ready(child, timeout=60)
        wait_for_prompt(child, timeout=90)

        # Exit the (dummy) tool to a bash prompt.
        child.send("exit")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(3)

        # Write the /tmp filesystem size (1K-blocks) to a workspace file so the
        # host can read it without screen-scraping.
        child.send("df --output=size /tmp | tail -1 > /workspace/tmpfs_size_kb.txt")
        time.sleep(0.5)
        child.send("\x0d")
        time.sleep(3)

        child.send("exit")
        time.sleep(0.3)
        child.send("\x0d")
        try:
            child.expect(EOF, timeout=30)
        except TIMEOUT:
            pass
    finally:
        try:
            child.close(force=True)
        except Exception:
            pass

    time.sleep(2)
    assert result_file.exists(), "container did not write the /tmp size file to the workspace"
    size_kb = int(result_file.read_text().strip())
    want_kb = 256 * 1024  # 256 MiB
    assert abs(size_kb - want_kb) <= want_kb * 0.05, (
        f"/tmp should be ~256MiB ({want_kb} KB) under coi shell, got {size_kb} KB"
    )
