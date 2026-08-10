"""
Named sessions: `[container] session_name` keys the session identity instead
of the workspace path, so the same persistent session continues from a
different workspace location.

Flow:
1. Trusted config sets session_name + persistent.
2. Session 1 from workspace A: container name derives from the NAME (not the
   path); leave a marker in the container's home.
3. Session 2 from workspace B (different path, same name): the SAME container
   is reused (marker still present) and /workspace now shows B's content —
   the persisted workspace device was remounted from the new location.
"""

import subprocess
import uuid

from support.helpers import calculate_container_name, write_trusted_coi_config


def test_named_session_continues_across_workspaces(coi_binary, tmp_path):
    session_name = f"namedsess-{uuid.uuid4().hex[:8]}"
    # workspace_dir is unused when session_name keys the identity
    container = calculate_container_name("", 1, session_name=session_name)

    ws_a = tmp_path / "checkout-a"
    ws_a.mkdir()
    (ws_a / "A_SENTINEL").write_text("a\n")
    ws_b = tmp_path / "checkout-b"
    ws_b.mkdir()
    (ws_b / "B_SENTINEL").write_text("b\n")

    env = write_trusted_coi_config(
        f"""
[container]
image = "coi-default"
persistent = true
session_name = "{session_name}"
"""
    )

    def run_session(workspace, command):
        return subprocess.run(
            # "--" ends coi's flag parsing so bash's -c isn't eaten by cobra;
            # coi run execs the argv directly (no implicit shell), so the
            # shell must be explicit for the && chains below.
            [
                coi_binary,
                "run",
                "--workspace",
                str(workspace),
                "--slot",
                "1",
                "--",
                "bash",
                "-c",
                command,
            ],
            capture_output=True,
            text=True,
            timeout=240,
            env=env,
        )

    try:
        # Session 1 from workspace A: prove the mount and leave a marker
        # outside /workspace (the container home persists with the container).
        r1 = run_session(ws_a, "ls /workspace && touch /home/code/named-session-marker")
        assert r1.returncode == 0, f"session 1 failed. stderr: {r1.stderr}"
        assert "A_SENTINEL" in r1.stdout, f"workspace A not mounted: {r1.stdout}"
        assert container in r1.stderr, (
            f"container name should derive from session_name ({container}). stderr: {r1.stderr}"
        )

        # Session 2 from workspace B: same name -> same container, workspace
        # remounted from the new location.
        r2 = run_session(
            ws_b,
            "ls /workspace && test -f /home/code/named-session-marker && echo MARKER_OK",
        )
        assert r2.returncode == 0, f"session 2 failed. stderr: {r2.stderr}"
        assert "B_SENTINEL" in r2.stdout, (
            f"workspace should be remounted from B. stdout: {r2.stdout}\nstderr: {r2.stderr}"
        )
        assert "A_SENTINEL" not in r2.stdout, (
            f"old workspace A must not still be mounted: {r2.stdout}"
        )
        assert "MARKER_OK" in r2.stdout, (
            f"session 2 should reuse the SAME container (marker missing). "
            f"stdout: {r2.stdout}\nstderr: {r2.stderr}"
        )
    finally:
        subprocess.run(
            ["incus", "--project", "default", "delete", container, "--force"],
            capture_output=True,
            timeout=60,
        )
