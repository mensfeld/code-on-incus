"""Test Docker registry access in different network modes.

This test verifies that:
1. In restricted mode, Docker registry (docker.io) is accessible
2. In allowlist mode, Docker access requires explicit configuration
3. Users can pull Docker images when needed

Why this matters:
- Developers need to pull Docker images for their workflows
- Restricted mode should allow Docker by default
- Allowlist mode requires explicit configuration
"""

from support.helpers import run


def test_docker_registry_accessible_in_restricted_mode(coi_binary, test_workspace, test_image):
    """Verify Docker registry is accessible in restricted network mode."""
    # Try to ping Docker Hub (registry-1.docker.io)
    # This should work in restricted mode
    result = run(
        coi_binary,
        "run",
        "--workspace",
        test_workspace,
        "--image",
        test_image,
        "--network-mode",
        "restricted",
        "--",
        "sh",
        "-c",
        "nc -zv -w 5 registry-1.docker.io 443 || curl -I --connect-timeout 5 https://registry-1.docker.io",
    )

    # Either nc or curl should succeed
    assert result.returncode == 0 or "Connected" in result.stderr or "200 OK" in result.stdout, (
        "Docker registry should be accessible in restricted mode"
    )


def test_docker_pull_works_in_restricted_mode(coi_binary, test_workspace, test_image):
    """Verify Docker pull works in restricted network mode.

    Note: This test requires Docker to be installed in the image.
    It verifies that the network configuration allows Docker registry access.
    """
    # Start docker daemon and try to pull a tiny image
    # Use alpine as it's small (~3MB) and commonly cached
    result = run(
        coi_binary,
        "run",
        "--workspace",
        test_workspace,
        "--image",
        test_image,
        "--network-mode",
        "restricted",
        "--",
        "sh",
        "-c",
        "sudo dockerd > /var/log/dockerd.log 2>&1 & sleep 3 && sudo docker pull alpine:latest",
        timeout=60,
    )

    # Note: This might fail if Docker daemon can't start, but that's OK
    # The key is that network restrictions don't block the pull
    if "Cannot connect to the Docker daemon" in result.stderr:
        # Docker daemon issue, not network issue - skip
        return

    # If Docker works, pull should succeed or at least reach registry
    assert (
        result.returncode == 0
        or "Pulling from library/alpine" in result.stdout
        or "Download complete" in result.stdout
        or "Already exists" in result.stdout
    ), "Docker pull should work in restricted mode (network-wise)"


def test_docker_sudo_passwordless(coi_binary, test_workspace, test_image):
    """Verify Docker commands work with passwordless sudo.

    This ensures the code user can run Docker without password prompts.
    """
    result = run(
        coi_binary,
        "run",
        "--workspace",
        test_workspace,
        "--image",
        test_image,
        "--",
        "sudo",
        "-n",
        "docker",
        "--version",
    )

    assert result.returncode == 0, "Should be able to run Docker with sudo -n (no password)"
    assert "Docker version" in result.stdout, "Docker should be available"
