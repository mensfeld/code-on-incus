"""
Test for coi image list --format json - output in JSON format.

Tests that:
1. Run coi image list --format json
2. Verify output is valid JSON
"""

import json
import subprocess


def test_list_json_format(coi_binary, cleanup_containers):
    """
    Test listing images in JSON format.

    Flow:
    1. Run coi image list --format json --prefix coi
    2. Verify output is valid JSON
    """
    # === Phase 1: Run image list with JSON format ===

    result = subprocess.run(
        [coi_binary, "image", "list", "--format", "json", "--prefix", "coi"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"Image list with JSON format should succeed. stderr: {result.stderr}"
    )

    # === Phase 2: Verify valid JSON ===

    output = result.stdout.strip()

    # Should be valid JSON (either array or null)
    try:
        parsed = json.loads(output)
        # Should be a list (possibly empty)
        assert isinstance(parsed, list) or parsed is None, (
            f"JSON output should be a list or null. Got: {type(parsed)}"
        )
    except json.JSONDecodeError as e:
        # Empty output is also acceptable for no matching images
        if output != "" and output != "null":
            raise AssertionError(f"Output should be valid JSON. Got:\n{output}\nError: {e}")
