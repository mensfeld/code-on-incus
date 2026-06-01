"""
Tests for coi health - checks that were previously falling into OTHER section.

Verifies that the following checks appear in the correct category sections
of the text output and in the JSON output:
  CRITICAL:    immutable_capability
  NETWORKING:  ufw_conflict, container_connectivity, network_restriction
  MONITORING:  monitoring_configuration, audit_log_directory, cgroup_availability
  OPTIONAL:    process_monitoring (verbose only)
"""

import json
import subprocess

import pytest

# ---------------------------------------------------------------------------
# Module-scoped fixtures — each variant of coi health runs exactly once
# ---------------------------------------------------------------------------


@pytest.fixture(scope="module")
def health_json(coi_binary):
    """Run coi health --format json once and return parsed data."""
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode in (0, 1, 2), (
        f"health exited with unexpected code {result.returncode}. stderr: {result.stderr}"
    )
    return json.loads(result.stdout)


@pytest.fixture(scope="module")
def health_text(coi_binary):
    """Run coi health once and return stdout."""
    result = subprocess.run(
        [coi_binary, "health"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode in (0, 1, 2), (
        f"health exited with unexpected code {result.returncode}. stderr: {result.stderr}"
    )
    return result.stdout


@pytest.fixture(scope="module")
def health_json_verbose(coi_binary):
    """Run coi health --format json --verbose once and return parsed data."""
    result = subprocess.run(
        [coi_binary, "health", "--format", "json", "--verbose"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode in (0, 1, 2), (
        f"health --verbose exited with unexpected code {result.returncode}. stderr: {result.stderr}"
    )
    return json.loads(result.stdout)


@pytest.fixture(scope="module")
def health_text_verbose(coi_binary):
    """Run coi health --verbose once and return stdout."""
    result = subprocess.run(
        [coi_binary, "health", "--verbose"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode in (0, 1, 2), (
        f"health --verbose exited with unexpected code {result.returncode}. stderr: {result.stderr}"
    )
    return result.stdout


# ---------------------------------------------------------------------------
# CRITICAL: immutable_capability
# ---------------------------------------------------------------------------


def test_immutable_capability_present(health_json):
    """immutable_capability must appear in coi health --format json."""
    assert "immutable_capability" in health_json["checks"], (
        f"Expected 'immutable_capability' in checks. Got: {sorted(health_json['checks'].keys())}"
    )


def test_immutable_capability_valid_status(health_json):
    """immutable_capability must have a valid status value."""
    check = health_json["checks"]["immutable_capability"]
    assert check["status"] in ("ok", "warning", "failed"), f"Unexpected status: {check['status']!r}"


def test_immutable_capability_in_critical_section(health_text):
    """immutable_capability must appear under CRITICAL: in text output."""
    assert "CRITICAL:" in health_text, "CRITICAL section missing from health output"
    critical_block = health_text.split("CRITICAL:")[1].split("\n\n")[0]
    assert "Immutable cap" in critical_block, (
        f"'Immutable cap' not found in CRITICAL section:\n{critical_block}"
    )


def test_immutable_capability_not_in_other(health_text):
    """immutable_capability must NOT appear in the OTHER section."""
    if "OTHER:" not in health_text:
        return
    other_block = health_text.split("OTHER:")[1].split("\n\n")[0]
    assert (
        "immutable_capability" not in other_block.lower() and "Immutable cap" not in other_block
    ), "immutable_capability ended up in OTHER section"


# ---------------------------------------------------------------------------
# NETWORKING: ufw_conflict
# ---------------------------------------------------------------------------


def test_ufw_conflict_present(health_json):
    """ufw_conflict must appear in coi health --format json."""
    assert "ufw_conflict" in health_json["checks"], (
        f"Expected 'ufw_conflict' in checks. Got: {sorted(health_json['checks'].keys())}"
    )


def test_ufw_conflict_valid_status(health_json):
    """ufw_conflict must be ok or warning — never failed in a sane environment."""
    check = health_json["checks"]["ufw_conflict"]
    assert check["status"] in ("ok", "warning"), (
        f"ufw_conflict status must be ok/warning, got: {check['status']!r}"
    )


def test_ufw_conflict_in_networking_section(health_text):
    """ufw_conflict must appear under NETWORKING: in text output."""
    assert "NETWORKING:" in health_text, "NETWORKING section missing from health output"
    networking_block = health_text.split("NETWORKING:")[1].split("\n\n")[0]
    assert "UFW conflict" in networking_block, (
        f"'UFW conflict' not found in NETWORKING section:\n{networking_block}"
    )


# ---------------------------------------------------------------------------
# NETWORKING: container_connectivity
# ---------------------------------------------------------------------------


def test_container_connectivity_present(health_json):
    """container_connectivity must appear in coi health --format json."""
    assert "container_connectivity" in health_json["checks"], (
        f"Expected 'container_connectivity' in checks. Got: {sorted(health_json['checks'].keys())}"
    )


def test_container_connectivity_valid_status(health_json):
    """container_connectivity must have a valid status."""
    check = health_json["checks"]["container_connectivity"]
    assert check["status"] in ("ok", "warning", "failed"), f"Unexpected status: {check['status']!r}"


def test_container_connectivity_in_networking_section(health_text):
    """container_connectivity must appear under NETWORKING: in text output."""
    assert "NETWORKING:" in health_text
    networking_block = health_text.split("NETWORKING:")[1].split("\n\n")[0]
    assert "Container connect" in networking_block, (
        f"'Container connect' not found in NETWORKING section:\n{networking_block}"
    )


# ---------------------------------------------------------------------------
# NETWORKING: network_restriction
# ---------------------------------------------------------------------------


def test_network_restriction_present(health_json):
    """network_restriction must appear in coi health --format json."""
    assert "network_restriction" in health_json["checks"], (
        f"Expected 'network_restriction' in checks. Got: {sorted(health_json['checks'].keys())}"
    )


def test_network_restriction_valid_status(health_json):
    """network_restriction must have a valid status."""
    check = health_json["checks"]["network_restriction"]
    assert check["status"] in ("ok", "warning", "failed"), f"Unexpected status: {check['status']!r}"


def test_network_restriction_in_networking_section(health_text):
    """network_restriction must appear under NETWORKING: in text output."""
    assert "NETWORKING:" in health_text
    networking_block = health_text.split("NETWORKING:")[1].split("\n\n")[0]
    assert "Network restriction" in networking_block, (
        f"'Network restriction' not found in NETWORKING section:\n{networking_block}"
    )


# ---------------------------------------------------------------------------
# MONITORING: monitoring_configuration
# ---------------------------------------------------------------------------


def test_monitoring_configuration_present(health_json):
    """monitoring_configuration must appear in coi health --format json."""
    assert "monitoring_configuration" in health_json["checks"], (
        f"Expected 'monitoring_configuration' in checks. Got: {sorted(health_json['checks'].keys())}"
    )


def test_monitoring_configuration_valid_status(health_json):
    """monitoring_configuration must have a valid status."""
    check = health_json["checks"]["monitoring_configuration"]
    assert check["status"] in ("ok", "warning", "failed"), f"Unexpected status: {check['status']!r}"


def test_monitoring_configuration_in_monitoring_section(health_text):
    """monitoring_configuration must appear under MONITORING: in text output."""
    assert "MONITORING:" in health_text, "MONITORING section missing from health output"
    monitoring_block = health_text.split("MONITORING:")[1].split("\n\n")[0]
    assert "Monitoring config" in monitoring_block, (
        f"'Monitoring config' not found in MONITORING section:\n{monitoring_block}"
    )


# ---------------------------------------------------------------------------
# MONITORING: audit_log_directory
# ---------------------------------------------------------------------------


def test_audit_log_directory_present(health_json):
    """audit_log_directory must appear in coi health --format json."""
    assert "audit_log_directory" in health_json["checks"], (
        f"Expected 'audit_log_directory' in checks. Got: {sorted(health_json['checks'].keys())}"
    )


def test_audit_log_directory_valid_status(health_json):
    """audit_log_directory must have a valid status."""
    check = health_json["checks"]["audit_log_directory"]
    assert check["status"] in ("ok", "warning", "failed"), f"Unexpected status: {check['status']!r}"


def test_audit_log_directory_in_monitoring_section(health_text):
    """audit_log_directory must appear under MONITORING: in text output."""
    assert "MONITORING:" in health_text
    monitoring_block = health_text.split("MONITORING:")[1].split("\n\n")[0]
    assert "Audit log dir" in monitoring_block, (
        f"'Audit log dir' not found in MONITORING section:\n{monitoring_block}"
    )


# ---------------------------------------------------------------------------
# MONITORING: cgroup_availability
# ---------------------------------------------------------------------------


def test_cgroup_availability_present(health_json):
    """cgroup_availability must appear in coi health --format json."""
    assert "cgroup_availability" in health_json["checks"], (
        f"Expected 'cgroup_availability' in checks. Got: {sorted(health_json['checks'].keys())}"
    )


def test_cgroup_availability_valid_status(health_json):
    """cgroup_availability must have a valid status."""
    check = health_json["checks"]["cgroup_availability"]
    assert check["status"] in ("ok", "warning", "failed"), f"Unexpected status: {check['status']!r}"


def test_cgroup_availability_in_monitoring_section(health_text):
    """cgroup_availability must appear under MONITORING: in text output."""
    assert "MONITORING:" in health_text
    monitoring_block = health_text.split("MONITORING:")[1].split("\n\n")[0]
    assert "cgroup v2" in monitoring_block, (
        f"'cgroup v2' not found in MONITORING section:\n{monitoring_block}"
    )


# ---------------------------------------------------------------------------
# OPTIONAL: process_monitoring (verbose only)
# ---------------------------------------------------------------------------


def test_process_monitoring_absent_without_verbose(health_json):
    """process_monitoring must NOT appear without --verbose."""
    assert "process_monitoring" not in health_json["checks"], (
        "process_monitoring should only appear with --verbose"
    )


def test_process_monitoring_present_with_verbose(health_json_verbose):
    """process_monitoring must appear with --verbose."""
    assert "process_monitoring" in health_json_verbose["checks"], (
        f"Expected 'process_monitoring' in verbose checks. Got: {sorted(health_json_verbose['checks'].keys())}"
    )


def test_process_monitoring_in_optional_section(health_text_verbose):
    """process_monitoring must appear under OPTIONAL: in verbose text output."""
    assert "OPTIONAL:" in health_text_verbose, "OPTIONAL section missing from verbose health output"
    optional_block = health_text_verbose.split("OPTIONAL:")[1].split("\n\n")[0]
    assert "Process monitoring" in optional_block, (
        f"'Process monitoring' not found in OPTIONAL section:\n{optional_block}"
    )


# ---------------------------------------------------------------------------
# Regression: no OTHER section
# ---------------------------------------------------------------------------


def test_no_other_section_in_normal_output(health_text):
    """No check should fall through to the OTHER section in normal (non-verbose) output."""
    assert "OTHER:" not in health_text, (
        "Some checks are uncategorized and falling into OTHER section:\n"
        + (health_text.split("OTHER:")[1].split("\n\n")[0] if "OTHER:" in health_text else "")
    )


def test_no_other_section_in_verbose_output(health_text_verbose):
    """No check should fall through to the OTHER section even with --verbose."""
    assert "OTHER:" not in health_text_verbose, (
        "Some checks are uncategorized and falling into OTHER section (verbose):\n"
        + (
            health_text_verbose.split("OTHER:")[1].split("\n\n")[0]
            if "OTHER:" in health_text_verbose
            else ""
        )
    )
