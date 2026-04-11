"""
Regression test: `coi run` against a custom image built from coi-default
must keep working after the `detectCodeUser` refactor.

Context: the primary fix for the "coi shell lands as root" bug lives in
internal/session/setup.go (probe the container for the `code` user
instead of matching the image alias literally). As a sibling cleanup,
internal/cli/run.go's `remapContainerUserIfNeeded` was switched from
the same broken `img != session.CoiImage` heuristic to the new
exported `session.DetectCodeUser` probe. `coi run` never exhibited
the shell bug because it hardcodes `--user container.CodeUID` when
executing the command — but the remap path (only reachable when
`[incus] code_uid` is set to a non-default value) was affected, and
the refactor touches the same function. This test guards against a
refactor regression: `coi run whoami` against a custom-from-default
image must still succeed and run as `code`.

This is distinct from the sibling
`tests/shell/ephemeral/shell_custom_image_uses_code_user_ephemeral.py`
which covers the actual bug in the `coi shell` path. Both should be
green after the fix; on master before the fix the shell test fails
with `root` and this run test passes unchanged.
"""

import subprocess
from pathlib import Path

import pytest


def test_custom_image_coi_run_still_runs_as_code(coi_binary, workspace_dir, cleanup_containers):
    """`coi run` against a custom-from-default image must still succeed as `code`."""
    image_name = "coi-test-custom-run-still-works"

    # Skip if the base image isn't built — this test cannot run without it.
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-default"],
        capture_output=True,
    )
    if result.returncode != 0:
        pytest.skip("coi-default image not built; cannot exercise custom-from-default path")

    # Best-effort cleanup of any leftover image from a prior failed run.
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    try:
        # Minimal custom-build config (no explicit base → defaults to coi-default).
        config_dir = Path(workspace_dir) / ".coi"
        config_dir.mkdir(exist_ok=True)
        (config_dir / "config.toml").write_text(
            f"""
[container]
image = "{image_name}"

[container.build]
commands = ["echo custom-image-marker > /tmp/custom_image_marker"]
"""
        )

        # Build the custom image.
        result = subprocess.run(
            [coi_binary, "build"],
            capture_output=True,
            text=True,
            timeout=600,
            cwd=workspace_dir,
        )
        assert result.returncode == 0, f"Custom build should succeed. stderr: {result.stderr}"

        # Sanity: the custom image now exists with the configured alias.
        result = subprocess.run(
            [coi_binary, "image", "exists", image_name],
            capture_output=True,
        )
        assert result.returncode == 0, f"Custom image '{image_name}' should exist after build"

        # Run a whoami via `coi run` wrapped in a unique marker so we can
        # extract it reliably from coi's own progress / stderr chatter
        # without worrying about stream interleaving or missing trailing
        # newlines.
        marker = "COI_WHOAMI_MARKER"
        result = subprocess.run(
            [coi_binary, "run", "--", "sh", "-c", f'echo "{marker}:$(whoami):END"'],
            capture_output=True,
            text=True,
            timeout=180,
            cwd=workspace_dir,
        )
        assert result.returncode == 0, (
            f"coi run against custom image should succeed. stderr: {result.stderr}"
        )

        # `coi run` may route the inner command's stdout through its own
        # stderr when there is no TTY — check both streams as one blob.
        combined = result.stdout + result.stderr

        assert f"{marker}:code:END" in combined, (
            f"`coi run` against a custom-from-default image must still run as `code`. "
            f"Looking for {marker}:code:END in combined output:\n"
            f"--- stdout ---\n{result.stdout}\n--- stderr ---\n{result.stderr}"
        )
        assert f"{marker}:root:END" not in combined, (
            f"`coi run` against a custom-from-default image must NOT run as root.\n"
            f"--- stdout ---\n{result.stdout}\n--- stderr ---\n{result.stderr}"
        )

    finally:
        subprocess.run(
            [coi_binary, "image", "delete", image_name],
            check=False,
            capture_output=True,
        )
