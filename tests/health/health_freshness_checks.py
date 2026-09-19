"""
Test for coi health freshness/posture checks (kernel build age, kernel
mitigations, distro EOL).

Tests that:
1. coi health --format json includes the kernel_build_age, kernel_mitigations,
   and distro_eol checks
2. All degrade gracefully (any of ok/warning, never a crash/failed)
3. They render under the SYSTEM category in text output
"""

import json
import subprocess


def test_health_freshness_checks_present(coi_binary):
    """The freshness checks must be part of every health report."""
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode in (0, 1, 2), (
        f"coi health did not run (exit {result.returncode}).\n{result.stdout}\n{result.stderr}"
    )
    data = json.loads(result.stdout)
    checks = data["checks"]

    for name in ("kernel_build_age", "kernel_mitigations", "distro_eol"):
        assert name in checks, f"health report should include the {name} check"
        # Freshness signals advise, they never fail the report outright.
        assert checks[name]["status"] in ("ok", "warning"), (
            f"{name} should be ok or warning, got {checks[name]}"
        )


def test_health_freshness_checks_in_system_category(coi_binary):
    """Text output must list the freshness checks under SYSTEM (an
    unregistered check would fall into OTHER)."""
    result = subprocess.run(
        [coi_binary, "health"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode in (0, 1, 2)
    output = result.stdout
    system_section = output.split("SYSTEM", 1)[-1]
    # Cut at the next category header so the assertion is scoped to SYSTEM.
    for header in ("CRITICAL", "NETWORKING"):
        system_section = system_section.split(header, 1)[0]
    assert "Kernel build age" in system_section, "kernel_build_age should render under SYSTEM"
    assert "Kernel mitigations" in system_section, "kernel_mitigations should render under SYSTEM"
    assert "Distro support" in system_section, "distro_eol should render under SYSTEM"
