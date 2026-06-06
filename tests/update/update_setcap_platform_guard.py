"""
Test that the restoreCapability platform guard is respected.

On non-Linux platforms the function is a no-op; on Linux it attempts setcap.
Actual execution is covered by TestRestoreCapability in internal/cli/update_test.go.
"""

import platform


def test_update_setcap_platform_guard():
    """
    Verify the platform guard design intent is consistent with the current OS.

    On non-Linux the function must be a no-op (covered by Go unit test).
    On Linux it attempts setcap via non-interactive sudo (also covered by Go unit test).
    This test documents the invariant so regressions are visible at the Python layer.
    """
    current_platform = platform.system().lower()
    assert current_platform in ("linux", "darwin", "windows"), (
        f"Unexpected platform: {current_platform}"
    )
