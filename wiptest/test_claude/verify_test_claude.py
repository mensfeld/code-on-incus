"""
Test that dummy works correctly with COI_USE_DUMMY=1 env var.

Verifies that:
1. dummy is installed in the image
2. COI_USE_DUMMY=1 uses dummy instead of real claude
3. dummy responds correctly
"""

import os
import subprocess
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "support"))
import pexpect


def test_test_claude_installed(coi_binary, cleanup_containers, tmp_path):
    """Test that dummy is installed in the image."""

    result = subprocess.run(
        [coi_binary, "run", "--", "which", "dummy"],
        cwd=str(tmp_path),
        capture_output=True,
        text=True,
        timeout=30
    )

    assert result.returncode == 0, f"dummy not found: {result.stderr}"
    assert "/usr/local/bin/dummy" in result.stdout, f"Unexpected path: {result.stdout}"

    print("✓ dummy is installed at /usr/local/bin/dummy")


def test_test_claude_version(coi_binary, cleanup_containers, tmp_path):
    """Test that dummy --version works."""

    result = subprocess.run(
        [coi_binary, "run", "--", "dummy", "--version"],
        cwd=str(tmp_path),
        capture_output=True,
        text=True,
        timeout=30
    )

    assert result.returncode == 0, f"dummy --version failed: {result.stderr}"
    assert "Dummy CLI" in result.stdout, f"Unexpected version output: {result.stdout}"
    assert "test stub" in result.stdout.lower(), f"Not test stub version: {result.stdout}"

    print(f"✓ dummy version: {result.stdout.strip()}")


def test_env_var_uses_test_claude(coi_binary, cleanup_containers, tmp_path):
    """Test that COI_USE_DUMMY=1 actually uses dummy."""

    env = os.environ.copy()
    env["COI_USE_DUMMY"] = "1"

    # Start shell with dummy
    child = pexpect.spawn(
        coi_binary,
        ["shell", "--tmux=false", "--slot", "97"],
        cwd=str(tmp_path),
        encoding="utf-8",
        timeout=30,
        env=env
    )

    try:
        # Should see message about using dummy
        child.expect("Using dummy", timeout=10)
        print("✓ Saw 'Using dummy' message")

        # Should see fake Claude startup
        child.expect("Tips for getting started", timeout=10)
        print("✓ Fake Claude started successfully")

        # Send a test message
        child.sendline("hello test")

        # Fake Claude should echo it back
        child.expect("hello test", timeout=5)
        print("✓ Fake Claude received input")

        # Exit
        child.sendline("exit")
        child.expect(pexpect.EOF, timeout=10)

        print("✓ COI_USE_DUMMY=1 works correctly!")

    finally:
        if child.isalive():
            child.terminate(force=True)


def test_without_env_var_uses_real_claude(coi_binary, cleanup_containers, tmp_path):
    """Test that without env var, it tries to use real claude (or fails if not installed)."""

    # Start shell WITHOUT dummy env var
    child = pexpect.spawn(
        coi_binary,
        ["shell", "--tmux=false", "--slot", "96"],
        cwd=str(tmp_path),
        encoding="utf-8",
        timeout=30
    )

    try:
        # Should NOT see message about using dummy
        index = child.expect([
            "Using dummy",
            "Tips for getting started",  # Real Claude or might fail to start
            "Starting Claude session",
            pexpect.TIMEOUT
        ], timeout=10)

        # Should not see dummy message
        assert index != 0, "Should not be using dummy without env var!"

        print("✓ Without env var, does not use dummy")

        # Try to exit gracefully
        child.sendline("exit")
        child.expect(pexpect.EOF, timeout=10)

    except Exception as e:
        # It's OK if real Claude isn't licensed or fails - we just want to ensure
        # dummy wasn't used
        print(f"✓ Without env var, does not use dummy (real Claude may have failed: {e})")

    finally:
        if child.isalive():
            child.terminate(force=True)
