"""
Integration tests for automatic OVN host routing.

These tests verify that COI automatically configures host routes to OVN networks,
allowing the host to access services running in containers.
"""

import ipaddress
import os
import subprocess

import pytest


@pytest.mark.skipif(
    os.getenv("CI_NETWORK_TYPE") == "bridge",
    reason="OVN routing tests require OVN networking",
)
def test_ovn_route_added_automatically(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that starting a container on an OVN network automatically adds a host route.

    Verifies that:
    1. Route doesn't exist before container starts
    2. Route is added when container starts
    3. Route uses correct OVN uplink IP (not bridge gateway IP)
    """
    # Get the OVN network configuration to know what route to expect
    result = subprocess.run(
        ["incus", "network", "show", "ovn-net"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode == 0, f"Failed to get network config: {result.stderr}"

    # Extract subnet and uplink IP from config
    subnet = None
    uplink_ip = None
    for line in result.stdout.split("\n"):
        line = line.strip()
        if line.startswith("ipv4.address:"):
            # Parse CIDR to get network address (e.g., "10.215.220.1/24" -> "10.215.220.0/24")
            gateway_cidr = line.split(":", 1)[1].strip()
            network = ipaddress.ip_network(gateway_cidr, strict=False)
            subnet = str(network)
        elif line.startswith("volatile.network.ipv4.address:"):
            uplink_ip = line.split(":", 1)[1].strip()

    assert subnet, "Could not find ipv4.address in network config"
    assert uplink_ip, "Could not find volatile.network.ipv4.address in network config"

    # Start a container (route should be added automatically)
    result = subprocess.run(
        [coi_binary, "shell", "--workspace", workspace_dir, "--background"],
        capture_output=True,
        text=True,
        timeout=90,
    )

    assert result.returncode == 0, f"Failed to start container: {result.stderr}"

    # Verify route was added
    result = subprocess.run(
        ["ip", "route", "show"],
        capture_output=True,
        text=True,
        timeout=5,
    )
    final_routes = result.stdout

    # Check that the route exists and uses the correct uplink IP
    found_route = False
    for line in final_routes.split("\n"):
        if subnet in line and uplink_ip in line:
            found_route = True
            # Verify it's routing via the OVN uplink IP, not the bridge gateway
            assert "via " + uplink_ip in line, (
                f"Route should use OVN uplink IP {uplink_ip}, got: {line}"
            )
            break

    assert found_route, (
        f"Expected route '{subnet} via {uplink_ip}' not found in routing table.\n"
        f"Routes:\n{final_routes}"
    )


@pytest.mark.skipif(
    os.getenv("CI_NETWORK_TYPE") != "bridge",
    reason="Bridge network test only runs in bridge environment",
)
def test_bridge_network_no_route_added(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that starting a container on a bridge network does NOT add a host route.

    Bridge networks don't need special routing - they're directly accessible.
    """
    # Get current routes
    result = subprocess.run(
        ["ip", "route", "show"],
        capture_output=True,
        text=True,
        timeout=5,
    )
    routes_before = result.stdout

    # Start a container on bridge network
    result = subprocess.run(
        [coi_binary, "shell", "--workspace", workspace_dir, "--background"],
        capture_output=True,
        text=True,
        timeout=90,
    )

    assert result.returncode == 0, f"Failed to start container: {result.stderr}"

    # Get routes after
    result = subprocess.run(
        ["ip", "route", "show"],
        capture_output=True,
        text=True,
        timeout=5,
    )
    routes_after = result.stdout

    # Routes should be unchanged (bridge networks are already routable)
    # We allow the incusbr0 network route to exist (10.47.62.0/24 dev incusbr0)
    # but no new routes should be added
    routes_before_lines = set(routes_before.strip().split("\n"))
    routes_after_lines = set(routes_after.strip().split("\n"))

    new_routes = routes_after_lines - routes_before_lines

    # Filter out any routes that are just the bridge network itself
    # (these may appear dynamically when container starts)
    significant_new_routes = [r for r in new_routes if r and not r.startswith("10.47.62.0/24")]

    assert len(significant_new_routes) == 0, (
        f"Expected no new routes for bridge network, but found: {significant_new_routes}"
    )


@pytest.mark.skipif(
    os.getenv("CI_NETWORK_TYPE") == "bridge",
    reason="OVN routing tests require OVN networking",
)
def test_route_idempotent_on_second_container(
    coi_binary, workspace_dir, cleanup_containers, tmp_path
):
    """
    Test that starting a second container on the same OVN network doesn't
    fail or duplicate the route.

    Verifies idempotency: route should only be added once.
    """
    # Start first container
    result = subprocess.run(
        [coi_binary, "shell", "--workspace", workspace_dir, "--background"],
        capture_output=True,
        text=True,
        timeout=90,
    )
    assert result.returncode == 0, f"Failed to start first container: {result.stderr}"

    # Get routes after first container
    result = subprocess.run(
        ["ip", "route", "show"],
        capture_output=True,
        text=True,
        timeout=5,
    )
    routes_after_first = result.stdout

    # Start second container in different workspace
    workspace_dir2 = tmp_path / "workspace2"
    workspace_dir2.mkdir()

    result = subprocess.run(
        [coi_binary, "shell", "--workspace", str(workspace_dir2), "--background"],
        capture_output=True,
        text=True,
        timeout=90,
    )
    assert result.returncode == 0, f"Failed to start second container: {result.stderr}"

    # Get routes after second container
    result = subprocess.run(
        ["ip", "route", "show"],
        capture_output=True,
        text=True,
        timeout=5,
    )
    routes_after_second = result.stdout

    # Routes should be identical (no duplication)
    # Sort for comparison since order might differ
    routes_first_sorted = sorted(routes_after_first.strip().split("\n"))
    routes_second_sorted = sorted(routes_after_second.strip().split("\n"))

    assert routes_first_sorted == routes_second_sorted, (
        "Routes should not change when starting second container on same network.\n"
        f"After first:\n{routes_after_first}\n"
        f"After second:\n{routes_after_second}"
    )
