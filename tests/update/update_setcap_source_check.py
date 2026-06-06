"""
Test that update.go contains the restoreCapability call and expected messages.

This is a source-level check ensuring the setcap restoration logic was not
accidentally removed. The actual execution is covered by the Go unit test
TestRestoreCapability in internal/cli/update_test.go.
"""

import os


def test_update_go_contains_setcap_restore(coi_binary):
    """
    Read internal/cli/update.go and verify setcap-related code is present.

    Flow:
    1. Locate update.go relative to the binary path
    2. Verify restoreCapability function exists and is called
    3. Verify success message and manual fallback hint are present
    4. Verify Linux platform check and non-interactive sudo are used
    """
    repo_root = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    update_go = os.path.join(repo_root, "internal", "cli", "update.go")

    assert os.path.exists(update_go), f"update.go not found at {update_go}"

    with open(update_go) as f:
        source = f.read()

    assert "func restoreCapability(" in source, "restoreCapability function not found in update.go"
    assert "restoreCapability(binaryPath)" in source, (
        "restoreCapability(binaryPath) call not found in update.go"
    )
    assert "Restored cap_linux_immutable capability" in source, (
        "Success message for setcap restore not found"
    )
    assert "sudo setcap cap_linux_immutable=ep" in source, "Manual setcap command hint not found"
    assert 'runtime.GOOS != "linux"' in source, (
        "Linux platform check not found in restoreCapability"
    )
    assert 'sudo", "-n"' in source, "Non-interactive sudo (-n) not used in restoreCapability"
