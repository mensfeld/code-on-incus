"""
End-to-end tests for the egress-policy controls dns_servers (DNS resolver
pinning) and allowed_ports (destination-port allowlist), driving REAL traffic
from inside the container.

These run in CI's network lane against real Incus + nftables + internet, which is
what makes them the enforcing coverage for the feature — the Go
`*_integration_test.go` counterparts self-skip without Incus and are not run by
this lane.

Both keys are TRUSTED-SCOPE ONLY (a resolver pin is a DNS-redirect primitive; the
port cap is honored from trusted scope for a uniform rule), so every test supplies
them via `write_trusted_coi_config` (COI_CONFIG env), NOT a workspace
.coi/config.toml — a project-scope config would have them stripped at load.

Test targets are raw public IPs (no DNS needed for the probe itself), and each
"blocked" assertion is paired with an "allowed" assertion to the SAME host on a
different port (or the same port on a different host), so a block can never pass
for the wrong reason (host simply unreachable):

    1.1.1.1 — Cloudflare: listens on 80, 443 and 53
    8.8.8.8 — Google:     listens on 443 and 53
"""

import subprocess

from support.helpers import wait_for_firewall_rules, write_trusted_coi_config


def _start_background_shell(coi_binary, workspace_dir, env):
    """Start a background coi shell with the given (trusted) env and return the
    container name once its firewall rules are in place."""
    result = subprocess.run(
        [coi_binary, "shell", "--workspace", workspace_dir, "--background", "--debug"],
        capture_output=True,
        text=True,
        timeout=90,
        env=env,
    )
    assert result.returncode == 0, f"background shell should start. stderr: {result.stderr}"

    container_name = None
    for line in result.stderr.split("\n"):
        if "Container name:" in line:
            container_name = line.split("Container name:")[-1].strip()
            break
    assert container_name is not None, f"could not find container name. stderr: {result.stderr}"

    assert wait_for_firewall_rules(container_name), (
        f"firewall rules for {container_name} did not appear in time"
    )
    return container_name


def _can_connect(coi_binary, container_name, host, port):
    """True if a TCP connection to host:port succeeds from inside the container.
    Uses bash's /dev/tcp so no extra image tooling is needed; a rejected port
    fails fast (the rules use `reject`)."""
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "timeout",
            "6",
            "bash",
            "-c",
            f"exec 3<>/dev/tcp/{host}/{port}",
        ],
        capture_output=True,
        text=True,
        timeout=20,
    )
    return result.returncode == 0


def _can_curl(coi_binary, container_name, url):
    """True if curl to url succeeds from inside the container (exercises the
    container's normal DNS + connectivity path)."""
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "curl",
            "-s",
            "-o",
            "/dev/null",
            "--connect-timeout",
            "10",
            url,
        ],
        capture_output=True,
        text=True,
        timeout=25,
    )
    return result.returncode == 0


def test_restricted_allowed_ports_blocks_other_ports(coi_binary, workspace_dir, cleanup_containers):
    """restricted + allowed_ports=[443]: 443 reachable, 80/22 rejected — to the
    SAME host, so the block is attributable to the port, not host reachability."""
    env = write_trusted_coi_config('[network]\nmode = "restricted"\nallowed_ports = [443]\n')
    name = _start_background_shell(coi_binary, workspace_dir, env)

    assert _can_connect(coi_binary, name, "1.1.1.1", 443), "allowed port 443 should be reachable"
    assert not _can_connect(coi_binary, name, "1.1.1.1", 80), (
        "port 80 is not in allowed_ports and must be blocked (same host as the working 443)"
    )
    assert not _can_connect(coi_binary, name, "1.1.1.1", 22), (
        "port 22 is not in allowed_ports and must be blocked"
    )


def test_restricted_dns_pin_blocks_unpinned_resolver(coi_binary, workspace_dir, cleanup_containers):
    """restricted + dns_servers=[1.1.1.1]: :53 to the pinned resolver is allowed,
    :53 to any other resolver is rejected, and non-53 traffic is unaffected."""
    env = write_trusted_coi_config('[network]\nmode = "restricted"\ndns_servers = ["1.1.1.1"]\n')
    name = _start_background_shell(coi_binary, workspace_dir, env)

    assert _can_connect(coi_binary, name, "1.1.1.1", 53), "pinned resolver must be reachable on :53"
    assert not _can_connect(coi_binary, name, "8.8.8.8", 53), (
        "an unpinned resolver must be BLOCKED on :53"
    )
    # Contrast: same unpinned host is reachable on 443, so the :53 block above is
    # attributable to the DNS pin, not to 8.8.8.8 being unreachable.
    assert _can_connect(coi_binary, name, "8.8.8.8", 443), (
        "non-53 traffic to the unpinned host must be unaffected by the DNS pin"
    )
    # The bridge resolver (the container's default DNS) must keep working — the pin
    # only touches the forward path, never the container's normal resolution.
    assert _can_curl(coi_binary, name, "https://example.com"), (
        "normal DNS resolution + HTTPS must keep working (bridge resolver untouched)"
    )


def test_allowlist_allowed_ports_constrains_host(coi_binary, workspace_dir, cleanup_containers):
    """allowlist + allowed_ports=[443]: an allowlisted host is reachable only on
    443; the same host on 80 is blocked, and a non-allowlisted host is blocked
    entirely. The allowlist entry is a literal /32 so no DNS resolution occurs."""
    env = write_trusted_coi_config(
        '[network]\nmode = "allowlist"\nallowed_domains = ["1.1.1.1/32"]\nallowed_ports = [443]\n'
    )
    name = _start_background_shell(coi_binary, workspace_dir, env)

    assert _can_connect(coi_binary, name, "1.1.1.1", 443), (
        "allowlisted host on the allowed port must be reachable"
    )
    assert not _can_connect(coi_binary, name, "1.1.1.1", 80), (
        "allowlisted host on a non-allowed port must be blocked (port cap)"
    )
    assert not _can_connect(coi_binary, name, "8.8.8.8", 443), (
        "a non-allowlisted host must be blocked entirely"
    )


def test_dns_servers_rejected_in_allowlist_mode(coi_binary, workspace_dir, cleanup_containers):
    """dns_servers + allowlist mode is refused (fail closed): allowlist mode blocks
    all DNS by design, so re-opening :53 would defeat it. coi run must abort and
    never execute the command."""
    env = write_trusted_coi_config(
        "[network]\n"
        'mode = "allowlist"\n'
        'allowed_domains = ["1.1.1.1/32"]\n'
        'dns_servers = ["1.1.1.1"]\n'
    )
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "sh", "-c", "echo SESSION_RAN"],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
        env=env,
    )
    combined = (result.stdout + result.stderr).lower()

    assert result.returncode != 0, (
        "coi run must fail closed when dns_servers is combined with allowlist mode.\n"
        f"stdout: {result.stdout}\nstderr: {result.stderr}"
    )
    assert "SESSION_RAN" not in result.stdout, (
        "the command must not run on a fail-closed setup error"
    )
    assert "dns_servers" in combined and "allowlist" in combined, (
        "the failure must be attributable to the dns_servers/allowlist incompatibility.\n"
        f"stdout: {result.stdout}\nstderr: {result.stderr}"
    )


def test_invalid_dns_server_fails_closed(coi_binary, workspace_dir, cleanup_containers):
    """An invalid dns_servers entry (not an IPv4 address) must fail the session
    closed rather than starting with a half-applied policy."""
    env = write_trusted_coi_config('[network]\nmode = "restricted"\ndns_servers = ["not-an-ip"]\n')
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "sh", "-c", "echo SESSION_RAN"],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
        env=env,
    )
    combined = (result.stdout + result.stderr).lower()

    assert result.returncode != 0, (
        "coi run must fail closed on an invalid dns_servers value.\n"
        f"stdout: {result.stdout}\nstderr: {result.stderr}"
    )
    assert "SESSION_RAN" not in result.stdout, (
        "the command must not run on a fail-closed setup error"
    )
    assert "dns_servers" in combined, (
        "the failure must be attributable to dns_servers validation.\n"
        f"stdout: {result.stdout}\nstderr: {result.stderr}"
    )
