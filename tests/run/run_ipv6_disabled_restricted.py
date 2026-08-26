"""
Test that `coi run` disables IPv6 at boot in restricted/allowlist mode.

`coi shell` set the pre-boot IPv6-disable sysctls (DisableIPv6AtBoot) so there is
no IPv6 egress window before the host-side ip6 drop lands; `coi run` applied only
the networkd half. This asserts IPv6 is disabled inside a restricted-mode run
(#726 follow-up).
"""

import pathlib
import subprocess


def test_run_disables_ipv6_at_boot_restricted(coi_binary, cleanup_containers, workspace_dir):
    """A restricted coi run should boot with IPv6 disabled in the container."""
    config_dir = pathlib.Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text('[network]\nmode = "restricted"\n')

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "cat",
            "/proc/sys/net/ipv6/conf/all/disable_ipv6",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )
    assert result.returncode == 0, f"coi run should succeed. stderr: {result.stderr}"
    assert result.stdout.strip() == "1", (
        "IPv6 should be disabled (disable_ipv6=1) in a restricted-mode coi run, "
        f"got: {result.stdout.strip()!r}"
    )
