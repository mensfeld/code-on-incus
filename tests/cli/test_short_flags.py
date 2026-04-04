"""
Test that short flags -a (--all) and -f (--force) are available on all commands.

Issue #6: -a existed only on image list/images, -f only on snapshot restore/delete.
"""

import subprocess


def test_list_short_all(coi_binary):
    """coi list -a should work the same as --all."""
    result = subprocess.run(
        [coi_binary, "list", "-a"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should succeed — listing with -a is valid even if no containers exist
    assert result.returncode == 0, (
        f"Expected exit 0 for 'list -a', got {result.returncode}: {result.stderr}"
    )


def test_kill_short_flags(coi_binary):
    """coi kill -a -f should accept both short flags."""
    result = subprocess.run(
        [coi_binary, "kill", "-a", "-f"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    combined = result.stdout + result.stderr
    # Flags should be recognized (no "unknown shorthand flag" error)
    assert "unknown shorthand flag" not in combined.lower(), (
        f"Short flags should be recognized, got: {combined}"
    )


def test_clean_short_flags(coi_binary):
    """coi clean -a -f should accept both short flags."""
    result = subprocess.run(
        [coi_binary, "clean", "-a", "-f"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    combined = result.stdout + result.stderr
    assert "unknown shorthand flag" not in combined.lower(), (
        f"Short flags should be recognized, got: {combined}"
    )


def test_shutdown_short_flags(coi_binary):
    """coi shutdown -a -f should accept both short flags."""
    result = subprocess.run(
        [coi_binary, "shutdown", "-a", "-f"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    combined = result.stdout + result.stderr
    assert "unknown shorthand flag" not in combined.lower(), (
        f"Short flags should be recognized, got: {combined}"
    )


def test_persist_short_flags(coi_binary):
    """coi persist -a -f should accept both short flags."""
    result = subprocess.run(
        [coi_binary, "persist", "-a", "-f"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    combined = result.stdout + result.stderr
    assert "unknown shorthand flag" not in combined.lower(), (
        f"Short flags should be recognized, got: {combined}"
    )


def test_build_short_force(coi_binary):
    """coi build -f --help should recognize -f as --force."""
    result = subprocess.run(
        [coi_binary, "build", "-f", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    combined = result.stdout + result.stderr
    assert result.returncode == 0, (
        f"Expected exit 0 for 'build -f --help', got {result.returncode}: {combined}"
    )
    assert "unknown shorthand flag" not in combined.lower(), (
        f"-f should be recognized on build, got: {combined}"
    )


def test_update_short_force(coi_binary):
    """coi update --help should show -f as short form for --force."""
    result = subprocess.run(
        [coi_binary, "update", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert "-f, --force" in result.stdout, (
        f"Expected '-f, --force' in help output, got: {result.stdout}"
    )


def test_container_stop_short_force(coi_binary):
    """coi container stop -f nonexistent should recognize -f flag."""
    result = subprocess.run(
        [coi_binary, "container", "stop", "-f", "nonexistent-container-xyz"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    combined = result.stdout + result.stderr
    # Should NOT fail with "unknown shorthand flag"
    assert "unknown shorthand flag" not in combined.lower(), (
        f"-f should be recognized on container stop, got: {combined}"
    )


def test_container_delete_short_force(coi_binary):
    """coi container delete -f nonexistent should recognize -f flag."""
    result = subprocess.run(
        [coi_binary, "container", "delete", "-f", "nonexistent-container-xyz"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    combined = result.stdout + result.stderr
    # Should NOT fail with "unknown shorthand flag"
    assert "unknown shorthand flag" not in combined.lower(), (
        f"-f should be recognized on container delete, got: {combined}"
    )


def test_snapshot_list_short_all(coi_binary):
    """coi snapshot list --help should show -a for --all."""
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert "-a, --all" in result.stdout, (
        f"Expected '-a, --all' in help output, got: {result.stdout}"
    )


def test_snapshot_delete_short_all(coi_binary):
    """coi snapshot delete --help should show -a for --all."""
    result = subprocess.run(
        [coi_binary, "snapshot", "delete", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert "-a, --all" in result.stdout, (
        f"Expected '-a, --all' in help output, got: {result.stdout}"
    )
