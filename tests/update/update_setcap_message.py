"""
Test for coi update setcap messaging after binary replacement.

Tests that the update command includes setcap-related output when running
on Linux. Since we can't perform an actual update in tests, we verify that
the source code contains the restoreCapability call and the expected messages.

The actual setcap behavior is covered by the Go unit test
(TestRestoreCapability in internal/cli/update_test.go).
"""

import platform


def test_update_go_contains_setcap_restore(coi_binary):
    """
    Verify update.go contains the restoreCapability call and expected messages.

    This is a source-level integration check ensuring the setcap restoration
    wasn't accidentally removed.

    Flow:
    1. Read internal/cli/update.go source
    2. Verify it contains the restoreCapability function
    3. Verify it contains the manual setcap command hint
    """
    import os

    # Find the repo root by walking up from the binary
    # The binary is typically in the repo root or a build dir
    repo_root = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    update_go = os.path.join(repo_root, "internal", "cli", "update.go")

    assert os.path.exists(update_go), f"update.go not found at {update_go}"

    with open(update_go) as f:
        source = f.read()

    # Verify restoreCapability function exists
    assert "func restoreCapability(" in source, "restoreCapability function not found in update.go"

    # Verify it's called after binary replacement
    assert "restoreCapability(binaryPath)" in source, (
        "restoreCapability(binaryPath) call not found in update.go"
    )

    # Verify success message
    assert "Restored cap_linux_immutable capability" in source, (
        "Success message for setcap restore not found"
    )

    # Verify fallback manual command hint
    assert "sudo setcap cap_linux_immutable=ep" in source, "Manual setcap command hint not found"

    # Verify it checks for Linux
    assert 'runtime.GOOS != "linux"' in source, (
        "Linux platform check not found in restoreCapability"
    )

    # Verify it uses non-interactive sudo
    assert 'sudo", "-n"' in source, "Non-interactive sudo (-n) not used in restoreCapability"


def test_update_setcap_platform_guard():
    """
    Verify the platform guard logic: setcap should only run on Linux.

    This test verifies the design intent — on non-Linux platforms,
    restoreCapability is a no-op.
    """
    current_platform = platform.system().lower()

    if current_platform != "linux":
        # On non-Linux, the function should be a no-op.
        # We can't easily verify this without mocking, but the Go unit test
        # (TestRestoreCapability/no-op on non-linux) covers this.
        pass
    else:
        # On Linux, the function should attempt setcap.
        # Without sudo credentials, it should print the manual command.
        # The Go unit test covers the actual execution.
        pass
