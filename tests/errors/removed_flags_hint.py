"""
Test for the removed --image / --persistent flags migration hint.

The global --image and --persistent CLI flags were removed (0.10 breaking
change) in favor of [container] image / persistent config. Using them must
fail with an actionable migration message, not a bare "unknown flag".
`coi profile create` keeps local flags with the same names (profile authoring
writes config), so it must still accept them.
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


def test_profile_create_keeps_local_flags(coi_binary):
    """profile create's local --image/--persistent must NOT trigger the hint
    at flag-parse time (they exist there and write profile config)."""
    result = _run(coi_binary, ["profile", "create", "--help"])
    assert result.returncode == 0
    assert "--image" in result.stdout, "profile create should document its local --image"
    assert "--persistent" in result.stdout, "profile create should document its local --persistent"
