"""
Integration test for the managed-settings policy file permissions (PR #606).

coi writes the Claude Code managed-settings policy
(`/etc/claude-code/managed-settings.json`, which carries `disableAutoMode`, #364)
into every container during session setup. It used to push the file with a plain
`incus file push`, which by default PRESERVES the host temp file's owner and mode
(`--uid`/`--gid` default to -1). The policy therefore landed owned by the invoking
host UID with the temp file's 0600 mode. Two consequences:

  - On any host whose UID differs from the container's `code` user (macOS's 501,
    CI runners' 1001), the `code` user could not read its own policy file, and
    Claude Code refused OAuth with EACCES on the unreadable policy.
  - On a host whose UID happened to match the `code` user (Linux 1000), the file
    was owned by the sandboxed agent itself, which could then rewrite or delete
    the very policy meant to constrain it.

The fix pushes the file root-owned `0644` in the push itself, so it lands
readable-by-all / writable-only-by-root with no window where it has the wrong
attributes.

These tests drive the real fix through `coi run` (full session setup → exec →
cleanup), the same seam tests/container/custom_code_uid.py uses. The
`0:0` / `644` assertions catch the regression on ANY host (pre-fix the owner is
the host UID and the mode is 600); the read-as-code assertion is the load-bearing
one — it reproduces the exact EACCES that broke OAuth.
"""

import subprocess

MANAGED_SETTINGS_PATH = "/etc/claude-code/managed-settings.json"


def _coi_run(coi_binary, workspace_dir, command):
    """Run `coi run --workspace <ws> -- <command>` and return the CompletedProcess.

    The inner command runs as the container's default user (`code`), which is
    exactly the unprivileged identity that must be able to read the policy.
    """
    return subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", *command],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )


def test_managed_settings_is_root_owned_world_readable(
    coi_binary, cleanup_containers, workspace_dir
):
    """The policy file must land owned by 0:0 with mode 644.

    A distinctive marker is printed so the assertion is robust against coi's own
    setup logging on the same stream. Pre-fix this line reads the host UID and
    600 (`MSPERM:1001:1001:600` on a CI runner), so the assertion fails.
    """
    result = _coi_run(
        coi_binary,
        workspace_dir,
        ["sh", "-c", f'echo "MSPERM:$(stat -c %u:%g:%a {MANAGED_SETTINGS_PATH})"'],
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "MSPERM:0:0:644" in combined_output, (
        "managed-settings.json must be root-owned (0:0) and mode 644; "
        f"got a different owner/mode. Full output:\n{combined_output}"
    )


def test_managed_settings_readable_by_unprivileged_code_user(
    coi_binary, cleanup_containers, workspace_dir
):
    """The unprivileged `code` user must be able to read the policy file.

    This is the regression that actually broke OAuth: pre-fix, on a host whose
    UID differs from the code user (CI runners are 1001), the `cat` below fails
    with EACCES. The command's own exit status is captured via a marker so the
    read failure is detected regardless of how coi propagates the exit code.
    """
    result = _coi_run(
        coi_binary,
        workspace_dir,
        [
            "sh",
            "-c",
            f'cat {MANAGED_SETTINGS_PATH}; echo "MSREAD_EXIT:$?"',
        ],
    )

    combined_output = result.stdout + result.stderr
    assert "MSREAD_EXIT:0" in combined_output, (
        "the unprivileged 'code' user could not read managed-settings.json "
        f"(the EACCES OAuth regression). Full output:\n{combined_output}"
    )
    assert "Permission denied" not in combined_output, (
        f"reading the policy file must not be denied. Full output:\n{combined_output}"
    )
    assert "disableAutoMode" in combined_output, (
        f"policy file content is unexpected. Full output:\n{combined_output}"
    )
