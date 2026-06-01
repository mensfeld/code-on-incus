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


def run_health_json(coi_binary, verbose=False, timeout=120):
    args = [coi_binary, "health", "--format", "json"]
    if verbose:
        args.append("--verbose")
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
    assert result.returncode in (0, 1, 2), (
        f"health exited with unexpected code {result.returncode}. stderr: {result.stderr}"
    )
    return json.loads(result.stdout)


def run_health_text(coi_binary, verbose=False, timeout=120):
    args = [coi_binary, "health"]
    if verbose:
        args.append("--verbose")
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
    assert result.returncode in (0, 1, 2), (
        f"health exited with unexpected code {result.returncode}. stderr: {result.stderr}"
    )
    return result.stdout


# ---------------------------------------------------------------------------
# CRITICAL: immutable_capability
# ---------------------------------------------------------------------------


def test_immutable_capability_present(coi_binary):
    """immutable_capability must appear in coi health --format json."""
    data = run_health_json(coi_binary)
    assert "immutable_capability" in data["checks"], (
        f"Expected 'immutable_capability' in checks. Got: {sorted(data['checks'].keys())}"
    )


def test_immutable_capability_valid_status(coi_binary):
    """immutable_capability must have a valid status value."""
    check = run_health_json(coi_binary)["checks"]["immutable_capability"]
    assert check["status"] in ("ok", "warning", "failed"), (
        f"Unexpected status: {check['status']!r}"
    )


def test_immutable_capability_in_critical_section(coi_binary):
    """immutable_capability must appear under CRITICAL: in text output."""
    output = run_health_text(coi_binary)
    assert "CRITICAL:" in output, "CRITICAL section missing from health output"
    critical_block = output.split("CRITICAL:")[1].split("\n\n")[0]
    assert "Immutable cap" in critical_block, (
        f"'Immutable cap' not found in CRITICAL section:\n{critical_block}"
    )


def test_immutable_capability_not_in_other(coi_binary):
    """immutable_capability must NOT appear in the OTHER section."""
    output = run_health_text(coi_binary)
    if "OTHER:" not in output:
        return
    other_block = output.split("OTHER:")[1].split("\n\n")[0]
    assert "immutable_capability" not in other_block.lower() and "Immutable cap" not in other_block, (
        "immutable_capability ended up in OTHER section"
    )


# ---------------------------------------------------------------------------
# NETWORKING: ufw_conflict
# ---------------------------------------------------------------------------


def test_ufw_conflict_present(coi_binary):
    """ufw_conflict must appear in coi health --format json."""
    data = run_health_json(coi_binary)
    assert "ufw_conflict" in data["checks"], (
        f"Expected 'ufw_conflict' in checks. Got: {sorted(data['checks'].keys())}"
    )


def test_ufw_conflict_valid_status(coi_binary):
    """ufw_conflict must be ok or warning — never failed in a sane environment."""
    check = run_health_json(coi_binary)["checks"]["ufw_conflict"]
    assert check["status"] in ("ok", "warning"), (
        f"ufw_conflict status must be ok/warning, got: {check['status']!r}"
    )


def test_ufw_conflict_in_networking_section(coi_binary):
    """ufw_conflict must appear under NETWORKING: in text output."""
    output = run_health_text(coi_binary)
    assert "NETWORKING:" in output, "NETWORKING section missing from health output"
    networking_block = output.split("NETWORKING:")[1].split("\n\n")[0]
    assert "UFW conflict" in networking_block, (
        f"'UFW conflict' not found in NETWORKING section:\n{networking_block}"
    )


# ---------------------------------------------------------------------------
# NETWORKING: container_connectivity
# ---------------------------------------------------------------------------


def test_container_connectivity_present(coi_binary):
    """container_connectivity must appear in coi health --format json."""
    data = run_health_json(coi_binary)
    assert "container_connectivity" in data["checks"], (
        f"Expected 'container_connectivity' in checks. Got: {sorted(data['checks'].keys())}"
    )


def test_container_connectivity_valid_status(coi_binary):
    """container_connectivity must have a valid status."""
    check = run_health_json(coi_binary)["checks"]["container_connectivity"]
    assert check["status"] in ("ok", "warning", "failed"), (
        f"Unexpected status: {check['status']!r}"
    )


def test_container_connectivity_in_networking_section(coi_binary):
    """container_connectivity must appear under NETWORKING: in text output."""
    output = run_health_text(coi_binary)
    assert "NETWORKING:" in output
    networking_block = output.split("NETWORKING:")[1].split("\n\n")[0]
    assert "Container connect" in networking_block, (
        f"'Container connect' not found in NETWORKING section:\n{networking_block}"
    )


# ---------------------------------------------------------------------------
# NETWORKING: network_restriction
# ---------------------------------------------------------------------------


def test_network_restriction_present(coi_binary):
    """network_restriction must appear in coi health --format json."""
    data = run_health_json(coi_binary)
    assert "network_restriction" in data["checks"], (
        f"Expected 'network_restriction' in checks. Got: {sorted(data['checks'].keys())}"
    )


def test_network_restriction_valid_status(coi_binary):
    """network_restriction must have a valid status."""
    check = run_health_json(coi_binary)["checks"]["network_restriction"]
    assert check["status"] in ("ok", "warning", "failed"), (
        f"Unexpected status: {check['status']!r}"
    )


def test_network_restriction_in_networking_section(coi_binary):
    """network_restriction must appear under NETWORKING: in text output."""
    output = run_health_text(coi_binary)
    assert "NETWORKING:" in output
    networking_block = output.split("NETWORKING:")[1].split("\n\n")[0]
    assert "Network restriction" in networking_block, (
        f"'Network restriction' not found in NETWORKING section:\n{networking_block}"
    )


# ---------------------------------------------------------------------------
# MONITORING: monitoring_configuration
# ---------------------------------------------------------------------------


def test_monitoring_configuration_present(coi_binary):
    """monitoring_configuration must appear in coi health --format json."""
    data = run_health_json(coi_binary)
    assert "monitoring_configuration" in data["checks"], (
        f"Expected 'monitoring_configuration' in checks. Got: {sorted(data['checks'].keys())}"
    )


def test_monitoring_configuration_valid_status(coi_binary):
    """monitoring_configuration must have a valid status."""
    check = run_health_json(coi_binary)["checks"]["monitoring_configuration"]
    assert check["status"] in ("ok", "warning", "failed"), (
        f"Unexpected status: {check['status']!r}"
    )


def test_monitoring_configuration_in_monitoring_section(coi_binary):
    """monitoring_configuration must appear under MONITORING: in text output."""
    output = run_health_text(coi_binary)
    assert "MONITORING:" in output, "MONITORING section missing from health output"
    monitoring_block = output.split("MONITORING:")[1].split("\n\n")[0]
    assert "Monitoring config" in monitoring_block, (
        f"'Monitoring config' not found in MONITORING section:\n{monitoring_block}"
    )


# ---------------------------------------------------------------------------
# MONITORING: audit_log_directory
# ---------------------------------------------------------------------------


def test_audit_log_directory_present(coi_binary):
    """audit_log_directory must appear in coi health --format json."""
    data = run_health_json(coi_binary)
    assert "audit_log_directory" in data["checks"], (
        f"Expected 'audit_log_directory' in checks. Got: {sorted(data['checks'].keys())}"
    )


def test_audit_log_directory_valid_status(coi_binary):
    """audit_log_directory must have a valid status."""
    check = run_health_json(coi_binary)["checks"]["audit_log_directory"]
    assert check["status"] in ("ok", "warning", "failed"), (
        f"Unexpected status: {check['status']!r}"
    )


def test_audit_log_directory_in_monitoring_section(coi_binary):
    """audit_log_directory must appear under MONITORING: in text output."""
    output = run_health_text(coi_binary)
    assert "MONITORING:" in output
    monitoring_block = output.split("MONITORING:")[1].split("\n\n")[0]
    assert "Audit log dir" in monitoring_block, (
        f"'Audit log dir' not found in MONITORING section:\n{monitoring_block}"
    )


# ---------------------------------------------------------------------------
# MONITORING: cgroup_availability
# ---------------------------------------------------------------------------


def test_cgroup_availability_present(coi_binary):
    """cgroup_availability must appear in coi health --format json."""
    data = run_health_json(coi_binary)
    assert "cgroup_availability" in data["checks"], (
        f"Expected 'cgroup_availability' in checks. Got: {sorted(data['checks'].keys())}"
    )


def test_cgroup_availability_valid_status(coi_binary):
    """cgroup_availability must have a valid status."""
    check = run_health_json(coi_binary)["checks"]["cgroup_availability"]
    assert check["status"] in ("ok", "warning", "failed"), (
        f"Unexpected status: {check['status']!r}"
    )


def test_cgroup_availability_in_monitoring_section(coi_binary):
    """cgroup_availability must appear under MONITORING: in text output."""
    output = run_health_text(coi_binary)
    assert "MONITORING:" in output
    monitoring_block = output.split("MONITORING:")[1].split("\n\n")[0]
    assert "cgroup v2" in monitoring_block, (
        f"'cgroup v2' not found in MONITORING section:\n{monitoring_block}"
    )


# ---------------------------------------------------------------------------
# OPTIONAL: process_monitoring (verbose only)
# ---------------------------------------------------------------------------


def test_process_monitoring_absent_without_verbose(coi_binary):
    """process_monitoring must NOT appear without --verbose."""
    data = run_health_json(coi_binary)
    assert "process_monitoring" not in data["checks"], (
        "process_monitoring should only appear with --verbose"
    )


def test_process_monitoring_present_with_verbose(coi_binary):
    """process_monitoring must appear with --verbose."""
    data = run_health_json(coi_binary, verbose=True)
    assert "process_monitoring" in data["checks"], (
        f"Expected 'process_monitoring' in verbose checks. Got: {sorted(data['checks'].keys())}"
    )


def test_process_monitoring_in_optional_section(coi_binary):
    """process_monitoring must appear under OPTIONAL: in verbose text output."""
    output = run_health_text(coi_binary, verbose=True)
    assert "OPTIONAL:" in output, "OPTIONAL section missing from verbose health output"
    optional_block = output.split("OPTIONAL:")[1].split("\n\n")[0]
    assert "Process monitoring" in optional_block, (
        f"'Process monitoring' not found in OPTIONAL section:\n{optional_block}"
    )


# ---------------------------------------------------------------------------
# Regression: no OTHER section
# ---------------------------------------------------------------------------


def test_no_other_section_in_normal_output(coi_binary):
    """No check should fall through to the OTHER section in normal (non-verbose) output."""
    output = run_health_text(coi_binary)
    assert "OTHER:" not in output, (
        "Some checks are uncategorized and falling into OTHER section:\n"
        + (output.split("OTHER:")[1].split("\n\n")[0] if "OTHER:" in output else "")
    )


def test_no_other_section_in_verbose_output(coi_binary):
    """No check should fall through to the OTHER section even with --verbose."""
    output = run_health_text(coi_binary, verbose=True)
    assert "OTHER:" not in output, (
        "Some checks are uncategorized and falling into OTHER section (verbose):\n"
        + (output.split("OTHER:")[1].split("\n\n")[0] if "OTHER:" in output else "")
    )
