"""
`coi unfreeze` had Go unit tests but no Python e2e (#559 A9). The full
freeze→unfreeze cycle is covered by internal/cli/unfreeze_integration_test.go;
this adds the cheap CLI error path (nonexistent container) so the command's
user-facing error is regression-guarded end to end.
"""

import subprocess


def test_unfreeze_nonexistent_errors(coi_binary):
    """`coi unfreeze <nonexistent>` fails clearly with a not-found error."""
    result = subprocess.run(
        [coi_binary, "unfreeze", "coi-nonexistent-unfreeze-9f3a2b"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "unfreezing a nonexistent container must fail"
    assert "not found" in (result.stdout + result.stderr).lower(), (
        f"expected a not-found error.\n{result.stdout}{result.stderr}"
    )
