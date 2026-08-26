"""
Test that `coi run` honors [limits.disk] tmpfs_size.

`coi shell` sized the /tmp tmpfs from [limits.disk] tmpfs_size but `coi run`
ignored it, so a big build that fit under `coi shell` could ENOSPC under
`coi run`. This asserts /tmp inside a run is the configured size (#726 follow-up),
verified from inside the container with `df` (the same method as the proven
container.TestSetTmpfsSize).
"""

import pathlib
import subprocess


def test_run_honors_tmpfs_size(coi_binary, cleanup_containers, workspace_dir):
    """coi run must size the /tmp tmpfs from [limits.disk] tmpfs_size."""
    config_dir = pathlib.Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_dir.joinpath("config.toml").write_text('[limits.disk]\ntmpfs_size = "256MiB"\n')

    # df --output=size prints a header line then the size in 1K-blocks.
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "df",
            "--output=size",
            "/tmp",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )
    assert result.returncode == 0, f"coi run should succeed. stderr: {result.stderr}"

    tokens = result.stdout.split()
    assert tokens, f"unexpected df output: {result.stdout!r}"
    size_kb = int(tokens[-1])  # last token is the value; skip the "1K-blocks" header
    want_kb = 256 * 1024  # 256 MiB
    # tmpfs size is exact, but allow a small margin; the point is it's the small
    # configured size, not the multi-GB host default.
    assert abs(size_kb - want_kb) <= want_kb * 0.05, (
        f"/tmp should be ~256MiB ({want_kb} KB) under coi run, got {size_kb} KB "
        f"(full df output: {result.stdout!r})"
    )
