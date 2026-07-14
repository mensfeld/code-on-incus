"""
Integration tests for DNS-enforced allowlist mode.

Allowlist mode does not pre-resolve `allowed_domains` and pin the resulting IPs.
It makes COI the container's resolver — transparently, via a prerouting DNAT on
port 53 — and installs an answer's addresses into the container's nftables set
*before* handing the answer back. The container therefore cannot be told about an
address the firewall does not already trust.

These tests pin the properties that the previous resolve-then-pin design could
not hold:

  * every address the container is handed is already in the firewall (the core
    invariant; the old design resolved on the HOST and hoped the container would
    later agree, which fails for any domain behind a rotating frontend pool);
  * wildcards cover subdomains, matched on the name being queried rather than by
    resolving the base domain (which returns an entirely different address set);
  * DNS is intercepted, so pointing resolv.conf at a public resolver does not
    escape the policy;
  * a config that cannot be honoured fails loudly instead of being skipped.
"""

import json
import os
import subprocess
import sys
import tempfile
import time

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from support.helpers import extract_container_name, wait_for_firewall_rules

# A domain behind a large, rotating pool of frontend addresses. This is the shape
# that broke the old design: the host and the container resolve independently and
# get different members of the same pool, so a pinned snapshot goes stale between
# one query and the next. Every Google API endpoint behaves this way, which is why
# Claude-via-Vertex users hit it constantly and direct api.anthropic.com users
# (one stable address) essentially never did.
ROTATING_DOMAIN = "oauth2.googleapis.com"
ROTATING_WILDCARD = "*.googleapis.com"

ALLOWLIST_CONFIG = f"""
[network]
mode = "allowlist"
allowed_domains = [
    "{ROTATING_WILDCARD}",
    "registry.npmjs.org",
]
allow_local_network_access = false
refresh_interval_minutes = 30
"""


def start_allowlist_container(coi_binary, workspace_dir, config_body):
    """Launch a background container with the given [network] config.

    Returns (container_name, launch_result). Caller asserts on the result when it
    expects a failure.
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
    """Resolve a name from inside the container, as the agent's HTTP client would.

    Returns the list of IPv4 addresses the container was handed (empty if the
    lookup failed or was refused).
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


def test_every_address_handed_to_the_container_is_already_allowed(
    coi_binary, workspace_dir, cleanup_containers
):
    """The core invariant of DNS-enforced allowlisting.

    Whatever the container resolves an allowed name to, the firewall must already
    permit — because COI answered the query and installed the addresses first.

    Under the old design this could not be guaranteed: COI resolved the domain on
    the host, pinned that snapshot, and the container then resolved the same name
    independently. For a rotating frontend pool it routinely got a *different*
    member of the pool, hit the default reject, and the agent died mid-task with
    "Unable to connect" — healing on its own moments later when a retry happened to
    land on an address that had been pinned.
    """
    name, result = start_allowlist_container(coi_binary, workspace_dir, ALLOWLIST_CONFIG)
    assert result.returncode == 0, f"container failed to start: {result.stderr}"
    assert name, f"no container name in output: {result.stdout}{result.stderr}"

    assert wait_for_firewall_rules(name, timeout=40), "firewall rules were not applied in time"

    ips = resolve_in_container(coi_binary, name, ROTATING_DOMAIN)
    assert ips, f"the container could not resolve the allowlisted domain {ROTATING_DOMAIN}"

    table = nft_coi_table()
    missing = [ip for ip in ips if ip not in table]
    assert not missing, (
        f"the container was handed {missing} for {ROTATING_DOMAIN}, but those addresses are "
        f"not in the firewall — a connection to them would be rejected. This is exactly the "
        f"race DNS-enforced allowlisting exists to eliminate.\n\n{table}"
    )


def test_wildcard_covers_subdomains(coi_binary, workspace_dir, cleanup_containers):
    """`*.googleapis.com` must cover its subdomains.

    The old resolver handled a wildcard by stripping the "*." and resolving the
    BASE domain, whose addresses have zero overlap with the regional endpoints a
    client actually dials. A user who allowlisted a wildcard got a firewall that
    permitted a set of addresses nothing would ever connect to.
    """
    name, result = start_allowlist_container(coi_binary, workspace_dir, ALLOWLIST_CONFIG)
    assert result.returncode == 0, f"container failed to start: {result.stderr}"
    assert wait_for_firewall_rules(name, timeout=40), "firewall rules were not applied in time"

    # Two different subdomains, neither of which is the configured base domain.
    for subdomain in ("oauth2.googleapis.com", "storage.googleapis.com"):
        ips = resolve_in_container(coi_binary, name, subdomain)
        assert ips, f"{subdomain} should be covered by {ROTATING_WILDCARD} but did not resolve"

        table = nft_coi_table()
        missing = [ip for ip in ips if ip not in table]
        assert not missing, (
            f"{subdomain}: {missing} handed to the container but not allowed\n{table}"
        )


def test_unlisted_domain_is_refused(coi_binary, workspace_dir, cleanup_containers):
    """A name outside the allowlist must not resolve at all."""
    name, result = start_allowlist_container(coi_binary, workspace_dir, ALLOWLIST_CONFIG)
    assert result.returncode == 0, f"container failed to start: {result.stderr}"
    assert wait_for_firewall_rules(name, timeout=40), "firewall rules were not applied in time"

    ips = resolve_in_container(coi_binary, name, "example.com")
    assert not ips, f"example.com is not allowlisted but resolved to {ips}"


def test_dns_interception_survives_a_hijacked_resolv_conf(
    coi_binary, workspace_dir, cleanup_containers
):
    """Pointing resolv.conf at a public resolver must not escape the policy.

    This is what makes the allowlist enforceable rather than merely cooperative.
    In-container root can rewrite /etc/resolv.conf, and COI historically shipped
    8.8.8.8 and 1.1.1.1 as literal allowlist entries — so an agent could simply
    resolve names COI never saw. A prerouting DNAT on port 53 now redirects the
    query to COI's resolver whatever nameserver the client aims at.
    """
    name, result = start_allowlist_container(coi_binary, workspace_dir, ALLOWLIST_CONFIG)
    assert result.returncode == 0, f"container failed to start: {result.stderr}"
    assert wait_for_firewall_rules(name, timeout=40), "firewall rules were not applied in time"

    # Bypass systemd-resolved entirely and aim straight at a public resolver.
    hijack = container_exec(coi_binary, name, "echo 'nameserver 8.8.8.8' > /etc/resolv.conf")
    assert hijack.returncode == 0, f"could not rewrite resolv.conf: {hijack.stderr}"

    # An allowlisted name still resolves — the query was redirected to COI, which
    # permits it — and its addresses are in the firewall.
    allowed = resolve_in_container(coi_binary, name, ROTATING_DOMAIN)
    assert allowed, "an allowlisted domain should still resolve through the intercepted resolver"
    table = nft_coi_table()
    missing = [ip for ip in allowed if ip not in table]
    assert not missing, f"{missing} handed to the container but not allowed\n{table}"

    # An unlisted name is still refused. If 8.8.8.8 had actually answered, this
    # would have resolved and the interception would be broken.
    denied = resolve_in_container(coi_binary, name, "example.com")
    assert not denied, (
        f"example.com resolved to {denied} after resolv.conf was pointed at 8.8.8.8 — "
        "DNS interception is not in effect and the allowlist can be bypassed"
    )


def test_rules_reference_sets_not_individual_addresses(
    coi_binary, workspace_dir, cleanup_containers
):
    """Rules must name the container's nft sets, not each address.

    This is what removed the fail-closed refresh window. The old refresher appended
    a fresh batch of accept rules BEHIND the still-present default reject, then
    deleted the stale handles one `sudo nft` exec at a time; until that finished,
    any address only in the new batch was rejected — so the firewall was at its
    most closed exactly when an address had rotated and the container most needed
    the new one. Set elements are applied atomically by the kernel, so there is no
    window to be caught in.
    """
    name, result = start_allowlist_container(coi_binary, workspace_dir, ALLOWLIST_CONFIG)
    assert result.returncode == 0, f"container failed to start: {result.stderr}"
    assert wait_for_firewall_rules(name, timeout=40), "firewall rules were not applied in time"

    ip = container_ip(name)
    assert ip, "could not determine the container's IP"
    ident = ip.replace(".", "_")

    table = nft_coi_table()
    for expected_set in (f"coi_s_{ident}", f"coi_d_{ident}"):
        assert expected_set in table, f"expected the {expected_set} set to exist\n{table}"
        assert f"@{expected_set}" in table, (
            f"expected a rule referencing @{expected_set} rather than per-address rules\n{table}"
        )

    assert "reject" in table, f"default deny is missing\n{table}"


def test_dns_learned_addresses_carry_a_timeout(coi_binary, workspace_dir, cleanup_containers):
    """DNS-learned addresses expire rather than being evicted on the next refresh.

    A large frontend round-robins across a pool far bigger than any single answer,
    so replacing the set wholesale would keep evicting addresses that are still
    live and still in use by an open session. Elements carry a TTL + grace timeout
    instead.
    """
    name, result = start_allowlist_container(coi_binary, workspace_dir, ALLOWLIST_CONFIG)
    assert result.returncode == 0, f"container failed to start: {result.stderr}"
    assert wait_for_firewall_rules(name, timeout=40), "firewall rules were not applied in time"

    ips = resolve_in_container(coi_binary, name, ROTATING_DOMAIN)
    assert ips, f"could not resolve {ROTATING_DOMAIN}"

    ip = container_ip(name)
    ident = ip.replace(".", "_")

    result = subprocess.run(
        ["sudo", "-n", "nft", "list", "set", "ip", "coi", f"coi_d_{ident}"],
        capture_output=True,
        text=True,
        timeout=15,
        check=False,
    )
    assert result.returncode == 0, f"could not read the dynamic set: {result.stderr}"
    assert "timeout" in result.stdout, (
        f"dynamic set elements must carry a timeout so a rotated-out address expires "
        f"rather than lingering forever\n{result.stdout}"
    )


def test_partial_label_wildcard_fails_loudly(coi_binary, workspace_dir, cleanup_containers):
    """A config that cannot be honoured must not start a container.

    `*-aiplatform.googleapis.com` looks like a reasonable way to allow every
    regional Vertex endpoint, and it is exactly what someone reaching for Vertex
    would write. The old resolver did not recognise it as a wildcard, passed it to
    DNS verbatim, watched it fail to resolve, and then dropped it from the
    allowlist WITHOUT stopping — leaving a firewall that quietly did not cover what
    the config asked for.
    """
    bad_config = """
[network]
mode = "allowlist"
allowed_domains = [
    "*-aiplatform.googleapis.com",
]
"""
    name, result = start_allowlist_container(coi_binary, workspace_dir, bad_config)

    assert result.returncode != 0, (
        "a partial-label wildcard cannot be enforced and must fail the launch, "
        f"but the container started (name={name})"
    )
    output = result.stdout + result.stderr
    assert "*." in output, f"the error should name the supported wildcard form:\n{output}"


def test_literal_addresses_are_allowed_without_dns(coi_binary, workspace_dir, cleanup_containers):
    """Raw IPs and CIDRs go straight into the static set and need no resolution."""
    config = """
[network]
mode = "allowlist"
allowed_domains = [
    "1.1.1.1",
    "8.8.4.0/24",
    "registry.npmjs.org",
]
"""
    name, result = start_allowlist_container(coi_binary, workspace_dir, config)
    assert result.returncode == 0, f"container failed to start: {result.stderr}"
    assert wait_for_firewall_rules(name, timeout=40), "firewall rules were not applied in time"

    ip = container_ip(name)
    ident = ip.replace(".", "_")

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


def test_teardown_removes_sets_and_the_dns_intercept(coi_binary, workspace_dir, cleanup_containers):
    """Teardown must remove rules, sets and the DNAT redirect.

    Rule order matters: the kernel refuses to drop a set a rule still references,
    and a stale DNAT rule would black-hole DNS for whatever container gets that IP
    next.
    """
    name, result = start_allowlist_container(coi_binary, workspace_dir, ALLOWLIST_CONFIG)
    assert result.returncode == 0, f"container failed to start: {result.stderr}"
    assert wait_for_firewall_rules(name, timeout=40), "firewall rules were not applied in time"

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
        nat = subprocess.run(
            ["sudo", "-n", "nft", "list", "table", "ip", "coi_nat"],
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        ).stdout
        if f"coi_s_{ident}" not in table and f"coi_d_{ident}" not in table and ip not in nat:
            return
        time.sleep(1)

    raise AssertionError(
        f"allowlist sets or the DNS intercept for {ip} survived teardown\n"
        f"coi table:\n{nft_coi_table()}"
    )
