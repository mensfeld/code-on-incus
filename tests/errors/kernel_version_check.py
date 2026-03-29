"""
Test for kernel version check — warning on old kernels.

Tests that:
1. coi run succeeds on a modern kernel without kernel warnings in stderr
"""

import subprocess


def test_no_kernel_warning_on_modern_kernel(coi_binary):
    """
    Verify coi run echo hello succeeds without kernel version warnings.

    On any system running kernel >= 5.15 (which CI and modern distros do),
    there should be no kernel warning in stderr.
    """
    result = subprocess.run(
        [coi_binary, "run", "echo", "hello"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, (
        f"coi run echo hello should succeed. Exit code: {result.returncode}\n"
        f"stderr: {result.stderr}"
    )

    stderr_lower = result.stderr.lower()
    assert not ("kernel" in stderr_lower and "warning" in stderr_lower), (
        f"Should not show kernel warning on modern kernel. Got:\n{result.stderr}"
    )
