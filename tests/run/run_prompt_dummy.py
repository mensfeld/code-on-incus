"""
Integration tests for `coi run` headless prompt mode (#701), using the dummy
tool image (installed as `claude` in the container) so no real agent auth is
needed. These exercise the full pipeline: launch → seed tool config/context →
stage the prompt file → build the headless (print-mode) launch command → exec it
through `bash -lc` → propagate the exit code.

They require Incus (they build/boot a container), so they are skipped where the
dummy image can't be built. Do NOT set COI_USE_DUMMY here: the dummy is installed
as `claude`, and prompt mode launches the configured `claude` tool directly.
"""

import os
import subprocess

from support.helpers import write_workspace_container_config


def test_run_prompt_inline(coi_binary, cleanup_containers, workspace_dir, dummy_image):
    """`coi run --prompt <text>` launches the agent headlessly and exits 0."""
    write_workspace_container_config(workspace_dir, image=dummy_image)

    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--prompt", "say hello and exit"],
        capture_output=True,
        text=True,
        timeout=180,
        stdin=subprocess.DEVNULL,
    )
    assert result.returncode == 0, (
        f"headless prompt run should exit 0. Got {result.returncode}. stderr:\n{result.stderr}"
    )
    # The phase announces the headless launch on stderr.
    assert "headless prompt" in result.stderr.lower()


def test_run_prompt_name_from_config(
    coi_binary, cleanup_containers, workspace_dir, dummy_image, tmp_path
):
    """`coi run --prompt-name <name>` resolves the [prompts] table and runs."""
    write_workspace_container_config(workspace_dir, image=dummy_image)

    cfg = tmp_path / "coi-config.toml"
    cfg.write_text('[prompts]\ngreet = "say hello and exit"\n')
    env = os.environ.copy()
    env["COI_CONFIG"] = str(cfg)

    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--prompt-name", "greet"],
        capture_output=True,
        text=True,
        timeout=180,
        stdin=subprocess.DEVNULL,
        env=env,
    )
    assert result.returncode == 0, (
        f"named prompt run should exit 0. Got {result.returncode}. stderr:\n{result.stderr}"
    )


def test_run_prompt_file(coi_binary, cleanup_containers, workspace_dir, dummy_image, tmp_path):
    """`coi run --prompt-file <path>` stages the file's contents and runs."""
    write_workspace_container_config(workspace_dir, image=dummy_image)

    prompt = tmp_path / "task.md"
    prompt.write_text("Do the nightly maintenance and exit.\n")

    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--prompt-file", str(prompt)],
        capture_output=True,
        text=True,
        timeout=180,
        stdin=subprocess.DEVNULL,
    )
    assert result.returncode == 0, (
        f"prompt-file run should exit 0. Got {result.returncode}. stderr:\n{result.stderr}"
    )


def test_run_prompt_shell_metacharacters(
    coi_binary, cleanup_containers, workspace_dir, dummy_image
):
    """A prompt containing shell metacharacters must not corrupt the launch: the
    file-based "$(cat ...)" staging keeps arbitrary text off the command line, so
    the run still completes cleanly (exit 0) rather than the shell trying to
    execute the embedded substitution/backticks."""
    write_workspace_container_config(workspace_dir, image=dummy_image)

    nasty = "oops \"; rm -rf / ; echo `whoami` $(id) ${HOME} 'quote"
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--prompt", nasty],
        capture_output=True,
        text=True,
        timeout=180,
        stdin=subprocess.DEVNULL,
    )
    assert result.returncode == 0, (
        f"metacharacter prompt should run cleanly (exit 0). Got {result.returncode}. "
        f"stderr:\n{result.stderr}"
    )
