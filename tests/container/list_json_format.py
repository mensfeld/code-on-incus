"""
Test for coi container list --format=json - JSON format output.

Tests that:
1. Container list returns valid JSON with --format=json
2. JSON contains expected structure
"""

import json
import subprocess


def test_container_list_json_format(coi_binary):
    """
    Test container list with JSON format.

    Flow:
    1. Run coi container list --format=json
    2. Verify output is valid JSON
    3. Verify JSON structure is a list
    4. Verify exit code is 0
    """
    # === Test: List containers in JSON format ===

    result = subprocess.run(
        [coi_binary, "container", "list", "--format=json"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"Container list should succeed. Exit code: {result.returncode}, stderr: {result.stderr}"
    )

    # Verify output is valid JSON
    try:
        containers = json.loads(result.stdout)
    except json.JSONDecodeError as e:
        raise AssertionError(f"Output should be valid JSON. Error: {e}\nOutput:\n{result.stdout}")

    # Verify JSON is a list
    assert isinstance(containers, list), (
        f"JSON output should be a list. Got type: {type(containers)}"
    )

    # If containers exist, verify structure
    if len(containers) > 0:
        first_container = containers[0]
        assert "name" in first_container, (
            f"Container object should have 'name' field. Got keys: {first_container.keys()}"
        )
        assert "status" in first_container, (
            f"Container object should have 'status' field. Got keys: {first_container.keys()}"
        )
