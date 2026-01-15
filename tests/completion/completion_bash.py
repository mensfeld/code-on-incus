"""
Test for coi completion bash - bash completion generation.

Tests that:
1. Run coi completion bash
2. Verify it generates valid bash completion script
3. Verify exit code is 0
"""

import subprocess


def test_completion_bash(coi_binary):
    """
    Test bash completion script generation.

    Flow:
    1. Run coi completion bash
    2. Verify exit code is 0
    3. Verify output contains bash completion directives
    """
    result = subprocess.run(
        [coi_binary, "completion", "bash"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Completion bash should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain bash completion directives
    assert "# bash completion" in output.lower() or "_coi()" in output, (
        f"Should contain bash completion code. Got:\n{output[:200]}..."
    )

    # Should be a substantial script (more than just a few lines)
    lines = [line for line in output.split("\n") if line.strip()]
    assert len(lines) > 10, f"Should generate substantial completion script. Got {len(lines)} lines"

    # Should mention the binary name
    assert "coi" in output.lower(), f"Should mention coi binary. Got:\n{output[:200]}..."
