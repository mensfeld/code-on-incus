"""
Test that coi build status messages go to stderr, not stdout.

Regression test for PR #396 — the default/coi-image build path used
fmt.Printf (stdout) for status messages while the custom-profile path used
fmt.Fprintf(os.Stderr, ...). Both paths now write to stderr consistently.

Rule: status/progress messages → stderr; structured data → stdout.
"""

import subprocess


def test_build_already_exists_output_on_stderr(coi_binary):
    """When coi build is run without --force and the image already exists,
    the 'already exists' message should appear on stderr, not stdout.

    Before the fix: fmt.Printf(...) sent this to stdout.
    After the fix: fmt.Fprintf(os.Stderr, ...) sends it to stderr.
    """
    # Skip if coi-default image doesn't exist (fresh environment, no build yet)
    check = subprocess.run(
        [coi_binary, "image", "exists", "coi-default"],
        capture_output=True,
    )
    if check.returncode != 0:
        return

    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"Expected exit 0 when image already exists. stderr: {result.stderr}"
    )
    assert result.stdout.strip() == "", (
        f"Expected no stdout output from coi build. Got: {result.stdout!r}"
    )
    assert "already exists" in result.stderr, (
        f"Expected 'already exists' message on stderr. Got stderr: {result.stderr!r}"
    )
