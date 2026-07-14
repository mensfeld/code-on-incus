"""
Integration tests for allowlist mode's deterministic name resolution.

Allowlist mode does not let the container resolve names itself. COI resolves the
allowlisted hostnames on the host, installs those addresses in the firewall, and
writes the SAME addresses into the container's /etc/hosts. All DNS egress is then
blocked, so that hosts file is the container's only route from a name to an
address.

That equality is the point. The old design resolved on the host, pinned the
result, and left the container to resolve independently — and for any domain
behind a rotating pool of frontend addresses (every Google API endpoint) the
container routinely got a different member of the pool, one the firewall had
never seen. The packet hit the default reject and the agent died mid-task with
"Unable to connect", healing seconds later when a retry happened to land on an
address that had been pinned.

Nothing has to stay running for the new guarantee to hold: the hosts file and the
firewall are both already in place and they already agree. It survives `coi`
exiting, the user detaching from tmux, or the process being killed — which is
exactly what an in-process DNS proxy could not do, since `coi shell --background`
returns immediately and the container outlives it.
"""

import json
import os
import subprocess
import sys
import tempfile
import time

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from support.helpers import extract_container_name, wait_for_firewall_rules

# A host behind a large, rotating pool of frontend addresses — the shape that
# broke the old design. It answers with several addresses at once, and the
# container may pick any of them, so every one must be in both the hosts file and
# the firewall.
ROTATING_HOST = "oauth2.googleapis.com"

ALLOWLIST_CONFIG = f"""
[network]
mode = "allowlist"
allowed_domains = [
    "{ROTATING_HOST}",
    "registry.npmjs.org",
]
allow_local_network_access = false
refresh_interval_minutes = 30
"""


def start_allowlist_container(coi_binary, workspace_dir, config_body):
    """Launch a background container with the given [network] config.

    Returns (container_name, launch_result). The caller asserts on the result when
    it expects a failure.
    """
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write(config_body)
        config_file = f.name

    env = os.environ.copy()
    env["COI_CONFIG"] = config_file

    result = subprocess.run(
        [coi_binary, "shell", "--workspace", workspace_dir, "--background"],
        capture_output=True,
        text=True,
        timeout=120,
        env=env,
    )
    return extract_container_name(result), result


def container_exec(coi_binary, container_name, command, timeout=30):
    """Run a shell command inside the container."""
    return subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "bash", "-c", command],
        capture_output=True,
        text=True,
        timeout=timeout,
        check=False,
    )


def container_ip(container_name):
    """The container's IPv4 address, or None."""
    result = subprocess.run(
        ["incus", "list", container_name, "--format=json"],
        capture_output=True,
        text=True,
        timeout=15,
        check=False,
    )
    if result.returncode != 0:
        return None
    for entry in json.loads(result.stdout):
        if entry.get("name") != container_name:
            continue
        for addr in entry.get("state", {}).get("network", {}).get("eth0", {}).get("addresses", []):
            if addr.get("family") == "inet":
                return addr.get("address")
    return None


def nft_coi_table():
    """The full `ip coi` table, including set elements."""
    result = subprocess.run(
        ["sudo", "-n", "nft", "list", "table", "ip", "coi"],
        capture_output=True,
        text=True,
        timeout=15,
        check=False,
    )
    return result.stdout if result.returncode == 0 else ""


def resolve_in_container(coi_binary, container_name, name):
    """Resolve a name inside the container, as the agent's HTTP client would.

    Returns the addresses the container was handed (empty if it could not resolve).
    """
    result = container_exec(coi_binary, container_name, f"getent ahostsv4 {name}")
    if result.returncode != 0:
        return []
    ips = []
    for line in result.stdout.splitlines():
        parts = line.split()
        if parts and parts[0] not in ips:
            ips.append(parts[0])
    return ips


def start_ready_container(coi_binary, workspace_dir, config=ALLOWLIST_CONFIG):
    """Launch a container and wait for its firewall rules. Returns the name."""
    name, result = start_allowlist_container(coi_binary, workspace_dir, config)
    assert result.returncode == 0, f"container failed to start: {result.stderr}"
    assert name, f"no container name in output: {result.stdout}{result.stderr}"
    assert wait_for_firewall_rules(name, timeout=40), "firewall rules were not applied in time"
    return name


def test_every_address_the_container_can_resolve_is_already_allowed(
    coi_binary, workspace_dir, cleanup_containers
):
    """The core invariant: the container cannot resolve to an address the firewall lacks.

    It resolves through /etc/hosts, which COI wrote from the same answer it fed the
    firewall. There is no second source of addresses, so the two cannot disagree.
    """
    name = start_ready_container(coi_binary, workspace_dir)

    ips = resolve_in_container(coi_binary, name, ROTATING_HOST)
    assert ips, f"the container could not resolve the allowlisted host {ROTATING_HOST}"

    table = nft_coi_table()
    missing = [ip for ip in ips if ip not in table]
    assert not missing, (
        f"the container resolved {ROTATING_HOST} to {missing}, but those addresses are not in the "
        f"firewall — a connection to them would be rejected. This is the divergence the design "
        f"exists to make impossible.\n\n{table}"
    )


def test_hosts_file_holds_every_address_and_is_delimited(
    coi_binary, workspace_dir, cleanup_containers
):
    """COI's managed block must carry every address, without clobbering the rest of /etc/hosts.

    A rotating frontend answers with several addresses and the container may pick
    any of them; writing only the first would leave it working until that one
    address went away.
    """
    name = start_ready_container(coi_binary, workspace_dir)

    hosts = container_exec(coi_binary, name, "cat /etc/hosts")
    assert hosts.returncode == 0, f"could not read /etc/hosts: {hosts.stderr}"
    content = hosts.stdout

    assert "BEGIN coi allowlist" in content, f"COI's managed block is missing:\n{content}"
    assert "END coi allowlist" in content, f"COI's managed block is unterminated:\n{content}"

    # Everything outside the markers must survive — the container needs localhost.
    assert "localhost" in content, f"the pre-existing hosts entries were clobbered:\n{content}"

    for host in (ROTATING_HOST, "registry.npmjs.org"):
        assert host in content, f"{host} is allowlisted but absent from /etc/hosts:\n{content}"

    # Every address in the file must also be in the firewall.
    table = nft_coi_table()
    for line in content.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[1] == ROTATING_HOST:
            assert parts[0] in table, (
                f"/etc/hosts offers the container {parts[0]} for {ROTATING_HOST}, "
                f"but the firewall does not allow it\n{table}"
            )


def test_dns_is_blocked_entirely(coi_binary, workspace_dir, cleanup_containers):
    """The container must have no route to any nameserver.

    Leave it one and it can resolve a name to an address the firewall has never
    seen, which is the whole failure mode. Both directions have to be closed: an
    off-box resolver (forwarded, caught by the forward chain) and the bridge's own
    dnsmasq (addressed to the HOST, so it never traverses the forward chain and
    needs an input rule).
    """
    name = start_ready_container(coi_binary, workspace_dir)

    gateway = container_exec(
        coi_binary, name, "ip route | awk '/default/ {print $3}'"
    ).stdout.strip()
    assert gateway, "could not determine the container's gateway"

    # The bridge's own resolver — the one an input rule is needed for.
    bridge_dns = container_exec(
        coi_binary,
        name,
        f"getent ahostsv4 example.com 2>/dev/null; dig +time=2 +tries=1 @{gateway} example.com",
    )
    assert "NOERROR" not in bridge_dns.stdout, (
        f"the container resolved a name via the bridge resolver at {gateway} — DNS is not blocked "
        f"and the allowlist can be bypassed\n{bridge_dns.stdout}"
    )

    # An off-box resolver.
    public_dns = container_exec(coi_binary, name, "dig +time=2 +tries=1 @8.8.8.8 example.com")
    assert "NOERROR" not in public_dns.stdout, (
        f"the container resolved a name via 8.8.8.8 — DNS is not blocked\n{public_dns.stdout}"
    )


def test_hijacking_resolv_conf_gains_nothing(coi_binary, workspace_dir, cleanup_containers):
    """Root inside the container can rewrite resolv.conf, and it buys them nothing.

    DNS egress is blocked at the host, so pointing the resolver anywhere at all is
    inert. The allowlisted names still resolve (they come from /etc/hosts), and
    everything else still does not.
    """
    name = start_ready_container(coi_binary, workspace_dir)

    hijack = container_exec(coi_binary, name, "echo 'nameserver 8.8.8.8' > /etc/resolv.conf")
    assert hijack.returncode == 0, f"could not rewrite resolv.conf: {hijack.stderr}"

    # Allowlisted names keep working: they never needed DNS.
    allowed = resolve_in_container(coi_binary, name, ROTATING_HOST)
    assert allowed, "an allowlisted host should still resolve from /etc/hosts after the hijack"

    # Anything else still does not resolve.
    denied = resolve_in_container(coi_binary, name, "example.com")
    assert not denied, (
        f"example.com resolved to {denied} after resolv.conf was pointed at 8.8.8.8 — "
        "DNS is not actually blocked and the allowlist can be bypassed"
    )


def test_unlisted_domain_is_unreachable(coi_binary, workspace_dir, cleanup_containers):
    """A host outside the allowlist has no address and no route."""
    name = start_ready_container(coi_binary, workspace_dir)

    assert not resolve_in_container(coi_binary, name, "example.com"), (
        "example.com is not allowlisted"
    )

    curl = container_exec(
        coi_binary, name, "curl -s --max-time 8 -o /dev/null -w '%{http_code}' https://example.com"
    )
    assert curl.returncode != 0, "a non-allowlisted host must not be reachable"


def test_allowlisted_domain_is_reachable(coi_binary, workspace_dir, cleanup_containers):
    """The allowlist has to actually work, not merely block."""
    name = start_ready_container(coi_binary, workspace_dir)

    curl = container_exec(
        coi_binary,
        name,
        "curl -s --max-time 20 -o /dev/null -w '%{http_code}' https://registry.npmjs.org",
        timeout=40,
    )
    assert curl.returncode == 0, f"an allowlisted host must be reachable: {curl.stderr}"
    assert curl.stdout.strip().startswith("2") or curl.stdout.strip().startswith("3"), (
        f"unexpected status from an allowlisted host: {curl.stdout}"
    )


def test_wildcard_fails_loudly(coi_binary, workspace_dir, cleanup_containers):
    """A wildcard cannot be honoured, so it must not start a container.

    Allowlist mode resolves each name up front and writes the answer into
    /etc/hosts. A wildcard has no answer to write — you cannot know which
    subdomains will be asked for. The old resolver pretended otherwise: it stripped
    the "*." and resolved the BASE domain, whose addresses have no overlap with the
    subdomains actually dialled, leaving a firewall that permitted addresses
    nothing would ever connect to.
    """
    bad_config = """
[network]
mode = "allowlist"
allowed_domains = [
    "*.googleapis.com",
]
"""
    name, result = start_allowlist_container(coi_binary, workspace_dir, bad_config)

    assert result.returncode != 0, (
        f"a wildcard cannot be enforced and must fail the launch, but the container started (name={name})"
    )
    output = result.stdout + result.stderr
    assert "exact hostnames" in output or "CIDR" in output, (
        f"the error must tell the user what to write instead:\n{output}"
    )


def test_literal_addresses_need_no_dns(coi_binary, workspace_dir, cleanup_containers):
    """Raw IPs and CIDRs go straight into the static set; no resolution involved."""
    config = """
[network]
mode = "allowlist"
allowed_domains = [
    "1.1.1.1",
    "8.8.4.0/24",
    "registry.npmjs.org",
]
"""
    name = start_ready_container(coi_binary, workspace_dir, config)

    ident = container_ip(name).replace(".", "_")
    result = subprocess.run(
        ["sudo", "-n", "nft", "list", "set", "ip", "coi", f"coi_s_{ident}"],
        capture_output=True,
        text=True,
        timeout=15,
        check=False,
    )
    assert result.returncode == 0, f"could not read the static set: {result.stderr}"
    for literal in ("1.1.1.1", "8.8.4.0/24"):
        assert literal in result.stdout, (
            f"literal entry {literal} should be in the static set\n{result.stdout}"
        )


def test_teardown_removes_rules_and_sets(coi_binary, workspace_dir, cleanup_containers):
    """Teardown must remove forward rules, the input-chain DNS block, and both sets.

    Order matters: the kernel refuses to drop a set a rule still references.
    """
    name = start_ready_container(coi_binary, workspace_dir)
    ip = container_ip(name)
    ident = ip.replace(".", "_")
    assert f"coi_d_{ident}" in nft_coi_table(), "the dynamic set should exist while running"

    subprocess.run(
        [coi_binary, "container", "delete", name, "--force"],
        capture_output=True,
        timeout=60,
        check=False,
    )

    deadline = time.time() + 30
    while time.time() < deadline:
        table = nft_coi_table()
        input_chain = subprocess.run(
            ["sudo", "-n", "nft", "list", "chain", "ip", "coi", "input"],
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        ).stdout
        if (
            f"coi_s_{ident}" not in table
            and f"coi_d_{ident}" not in table
            and ip not in input_chain
        ):
            return
        time.sleep(1)

    raise AssertionError(
        f"allowlist rules or sets for {ip} survived teardown\ncoi table:\n{nft_coi_table()}"
    )
