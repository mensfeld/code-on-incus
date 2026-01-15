"""
Test for coi completion fish - fish completion generation.

Tests that:
1. Run coi completion fish
2. Verify it generates valid fish completion script
3. Verify exit code is 0
"""

import subprocess


def test_completion_fish(coi_binary):
    """
    Test fish completion script generation.

    Flow:
    1. Run coi completion fish
    2. Verify exit code is 0
    3. Verify output contains fish completion directives
    """
    result = subprocess.run(
        [coi_binary, "completion", "fish"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Completion fish should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain fish completion directives (complete -c is the fish completion command)
    assert "complete -c" in output or "fish completion" in output.lower(), (
        f"Should contain fish completion code. Got:\n{output[:200]}..."
    )

    # Should be a substantial script
    lines = [line for line in output.split("\n") if line.strip()]
    assert len(lines) > 5, f"Should generate completion script. Got {len(lines)} lines"

    # Should mention the binary name
    assert "coi" in output, f"Should mention coi binary. Got:\n{output[:200]}..."
