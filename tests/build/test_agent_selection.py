"""
Tests for selectable agents in the base image build (issue #454).

profiles/default/build.sh used to call install_claude_cli / install_opencode /
install_pi unconditionally. It now dispatches through install_selected_agents,
which installs the agents named in $COI_AGENTS (comma/space separated) and falls
back to ALL supported agents when COI_AGENTS is unset — preserving the historical
default.

These tests source the REAL install_selected_agents from build.sh (like
test_opencode_arch_selection.py sources opencode_asset_arch), stub the per-agent
install functions as markers, and assert the dispatch. Hermetic: no network, no Incus.
"""

import os
import subprocess
from pathlib import Path

import pytest

BUILD_SH = Path(__file__).resolve().parents[2] / "profiles" / "default" / "build.sh"

# Stub the per-agent installers + log as markers, then source ONLY the real
# install_selected_agents function and run it. The stubs are defined before the
# source, so the dispatch resolves to them at call time.
_RUN = r"""
set -e
install_claude_cli() { echo "AGENT:claude"; }
install_opencode()   { echo "AGENT:opencode"; }
install_pi()         { echo "AGENT:pi"; }
log()                { echo "LOG:$*"; }
source <(sed -n '/^install_selected_agents()/,/^}/p' "$BUILD_SH")
install_selected_agents
"""


def _run(coi_agents=None):
    env = {**os.environ, "BUILD_SH": str(BUILD_SH)}
    if coi_agents is not None:
        env["COI_AGENTS"] = coi_agents
    else:
        env.pop("COI_AGENTS", None)
    r = subprocess.run(["bash", "-c", _RUN], capture_output=True, text=True, env=env, timeout=30)
    assert r.returncode == 0, f"dispatch failed: {r.stdout}{r.stderr}"
    return [
        line.removeprefix("AGENT:") for line in r.stdout.splitlines() if line.startswith("AGENT:")
    ], r.stdout


def test_unset_installs_all_agents():
    """COI_AGENTS unset installs every supported agent (historical default)."""
    agents, _ = _run(coi_agents=None)
    assert agents == ["claude", "opencode", "pi"], agents


def test_empty_installs_all_agents():
    """An empty COI_AGENTS also falls back to all agents."""
    agents, _ = _run(coi_agents="")
    assert agents == ["claude", "opencode", "pi"], agents


def test_single_agent_selection():
    """Only the named agent is installed."""
    agents, _ = _run(coi_agents="claude")
    assert agents == ["claude"], agents


def test_subset_comma_separated():
    """A comma-separated subset installs exactly those, in order."""
    agents, _ = _run(coi_agents="claude,pi")
    assert agents == ["claude", "pi"], agents


def test_subset_space_separated():
    """Space separation works too."""
    agents, _ = _run(coi_agents="opencode pi")
    assert agents == ["opencode", "pi"], agents


def test_unknown_agent_warns_and_skips():
    """An unknown name is warned about and skipped, not fatal (coi validates
    host-side before build; this is defense-in-depth)."""
    agents, out = _run(coi_agents="claude bogus")
    assert agents == ["claude"], agents
    assert "WARNING" in out and "bogus" in out, out


# --- end-to-end via the real coi binary (config -> validation) ---------------


def _skip_without_incus(coi_binary):
    if (
        subprocess.run(
            [coi_binary, "image", "exists", "coi-default"], capture_output=True
        ).returncode
        != 0
    ):
        pytest.skip("coi base image not available")


def test_coi_build_rejects_unknown_agent(coi_binary, workspace_dir):
    """`coi build` fails fast with a clear error when [container.build] agents names an
    unsupported agent — validation happens before any image build, so this is cheap and
    proves the config -> default-profile -> validate wiring end to end (#454)."""
    _skip_without_incus(coi_binary)
    coi_dir = Path(workspace_dir) / ".coi"
    coi_dir.mkdir(exist_ok=True)
    (coi_dir / "config.toml").write_text(
        '[container]\nimage = "coi-default"\n\n[container.build]\nagents = ["bogus"]\n'
    )
    result = subprocess.run(
        [coi_binary, "build"], capture_output=True, text=True, timeout=120, cwd=workspace_dir
    )
    combined = (result.stdout + result.stderr).lower()
    assert result.returncode != 0, (
        f"unknown agent must fail the build:\n{result.stdout}{result.stderr}"
    )
    assert "unknown agent" in combined and "bogus" in combined, combined


def test_coi_build_warns_when_configured_tool_not_built(coi_binary, workspace_dir):
    """`coi build` warns when the agents list omits the tool the config runs, before
    building. Gated on an existing coi-default (no --force) so it warns then skips —
    no expensive rebuild."""
    _skip_without_incus(coi_binary)
    coi_dir = Path(workspace_dir) / ".coi"
    coi_dir.mkdir(exist_ok=True)
    (coi_dir / "config.toml").write_text(
        '[tool]\nname = "claude"\n\n[container]\nimage = "coi-default"\n\n'
        '[container.build]\nagents = ["opencode"]\n'
    )
    result = subprocess.run(
        [coi_binary, "build"], capture_output=True, text=True, timeout=120, cwd=workspace_dir
    )
    combined = result.stdout + result.stderr
    # Warn fired (tool 'claude' not in agents) and the build did not error out.
    assert "claude" in combined.lower() and "warning" in combined.lower(), combined
    assert result.returncode == 0, f"warn is non-fatal; build should skip/succeed:\n{combined}"
