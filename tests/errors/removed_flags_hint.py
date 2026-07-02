"""
Test for the removed --image / --persistent flags migration hint.

The --image and --persistent CLI flags were removed everywhere (0.10 breaking
change) in favor of [container] image / persistent config. Using them must
fail with an actionable migration message, not a bare "unknown flag" — on any
command, including `coi profile create` (profiles are authored by editing
their config.toml).
"""

import subprocess


def _run(coi_binary, args):
    return subprocess.run(
        [coi_binary, *args],
        capture_output=True,
        text=True,
        timeout=10,
    )


def test_removed_persistent_flag_hint(coi_binary):
    result = _run(coi_binary, ["run", "--persistent"])
    assert result.returncode != 0, "removed flag must fail"
    assert "flag was removed" in result.stderr, f"want migration hint, got:\n{result.stderr}"
    assert "[container]" in result.stderr, f"hint must point at config, got:\n{result.stderr}"


def test_removed_image_flag_hint(coi_binary):
    result = _run(coi_binary, ["shell", "--image", "foo"])
    assert result.returncode != 0, "removed flag must fail"
    assert "flag was removed" in result.stderr, f"want migration hint, got:\n{result.stderr}"
    assert "[container]" in result.stderr, f"hint must point at config, got:\n{result.stderr}"


def test_profile_create_flags_removed_too(coi_binary):
    """The flags are gone from profile create as well — profiles are authored
    by editing their config.toml, so the migration hint fires there too."""
    result = _run(coi_binary, ["profile", "create", "x", "--image", "y"])
    assert result.returncode != 0, "removed flag must fail on profile create"
    assert "flag was removed" in result.stderr, f"want migration hint, got:\n{result.stderr}"

    help_result = _run(coi_binary, ["profile", "create", "--help"])
    assert help_result.returncode == 0
    assert "--image" not in help_result.stdout, "help must not document the removed --image"
    assert "--persistent" not in help_result.stdout, (
        "help must not document the removed --persistent"
    )
