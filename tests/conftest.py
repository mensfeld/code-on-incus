"""
Pytest configuration and fixtures for CLI integration tests.
"""

import os
import subprocess
import sys

import pytest

# Add tests directory to Python path so 'from support.helpers import ...' works
tests_dir = os.path.dirname(os.path.abspath(__file__))
if tests_dir not in sys.path:
    sys.path.insert(0, tests_dir)


@pytest.fixture(scope="session")
def coi_binary():
    """Return path to coi binary."""
    # Check COI_BINARY env var first (for CI)
    if "COI_BINARY" in os.environ:
        binary_path = os.environ["COI_BINARY"]
        if os.path.exists(binary_path):
            return os.path.abspath(binary_path)

    # Look for coi binary in project root
    binary_path = os.path.join(os.path.dirname(__file__), "..", "coi")
    if not os.path.exists(binary_path):
        pytest.skip("coi binary not found - run 'make build' first")
    return os.path.abspath(binary_path)


@pytest.fixture
def workspace_dir(tmp_path):
    """Provide an isolated temporary workspace directory for each test."""
    # Create a unique workspace directory for this test
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    return str(workspace)


@pytest.fixture
def cleanup_containers(workspace_dir, coi_binary):
    """Cleanup test containers after each test."""
    # Import here to avoid circular imports
    from support.helpers import calculate_container_name, get_container_list

    yield

    # Calculate container names for this workspace (slots 1-10)
    workspace_containers = set()
    for slot in range(1, 11):
        workspace_containers.add(calculate_container_name(workspace_dir, slot))

    # Get all running containers and delete any that belong to this test's workspace
    containers = get_container_list()
    for container in containers:
        if container in workspace_containers:
            subprocess.run(
                [coi_binary, "container", "delete", container, "--force"],
                capture_output=True,
                timeout=30,
                check=False,
            )

    # Kill any orphaned tmux sessions to prevent test pollution
    # This ensures clean state between tests, especially after tmux command tests
    subprocess.run(
        ["tmux", "kill-server"],
        capture_output=True,
        timeout=5,
        check=False,
    )


@pytest.fixture(scope="session")
def dummy_path():
    """Return path to dummy CLI for testing.

    This allows tests to run without requiring actual software licenses.
    The dummy simulates basic interactive CLI behavior for testing container
    orchestration logic.
    """
    dummy_dir = os.path.join(os.path.dirname(__file__), "..", "testdata", "dummy")
    if not os.path.exists(os.path.join(dummy_dir, "dummy")):
        pytest.skip("dummy not found")
    return os.path.abspath(dummy_dir)


@pytest.fixture(scope="session")
def dummy_image(coi_binary):
    """Build and return a test image with dummy pre-installed.

    This image includes dummy at /usr/local/bin/dummy, allowing
    tests to run 10x+ faster without requiring actual software licenses.

    The image is built once per test session and reused across all tests.
    """
    image_name = "coi-test-dummy"

    # Check if image already exists
    result = subprocess.run([coi_binary, "image", "exists", image_name], capture_output=True)

    if result.returncode == 0:
        return image_name  # Already built

    # Build image with dummy
    script_path = os.path.join(os.path.dirname(__file__), "..", "testdata", "dummy", "install.sh")

    if not os.path.exists(script_path):
        pytest.skip(f"Dummy install script not found: {script_path}")

    print("\nBuilding test image with dummy (one-time setup)...")

    result = subprocess.run(
        [coi_binary, "build", "custom", image_name, "--script", script_path],
        capture_output=True,
        text=True,
        timeout=300,
    )

    if result.returncode != 0:
        pytest.skip(f"Could not build dummy image: {result.stderr}")

    print(f"✓ Test image '{image_name}' built successfully")
    return image_name


# Hook to show test duration inline with each test result
@pytest.hookimpl(hookwrapper=True)
def pytest_runtest_makereport(item, call):
    """Add duration to test reports."""
    outcome = yield
    report = outcome.get_result()

    # Add duration to the report for later display
    if call.when == "call":
        report.duration = call.duration


def pytest_report_teststatus(report, config):
    """Customize test status output to include duration inline."""
    if report.when == "call" and hasattr(report, "duration"):
        duration = report.duration
        # Format duration nicely
        duration_str = f"{duration:.2f}s" if duration < 1 else f"{duration:.1f}s"

        # Append duration to the word (status)
        if report.passed:
            word = f"PASSED ({duration_str})"
        elif report.failed:
            word = f"FAILED ({duration_str})"
        elif report.skipped:
            word = f"SKIPPED ({duration_str})"
        else:
            word = f"{report.outcome.upper()} ({duration_str})"

        return report.outcome, "", word

    return None  # Use default formatting
