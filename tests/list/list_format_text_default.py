"""Test that coi list defaults to text format"""

import subprocess

from support.helpers import calculate_container_name


def test_list_format_text_default(coi_binary, cleanup_containers, workspace_dir):
    """Test that coi list without --format outputs human-readable text."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0

    # Run list without format flag
    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0

    # Verify text output
    output = result.stdout
    assert "Active Containers:" in output, "Should have text header"
    assert container_name in output, "Should show container name"
    assert "Running" in output or "Status:" in output, "Should show status information"

    # Should NOT be JSON
    assert not output.strip().startswith("{"), "Should not output JSON by default"

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
