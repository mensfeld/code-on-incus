"""
Integration tests for `coi schema profile`.

Verifies that the command outputs a valid JSON Schema (2020-12) document
and that the schema correctly accepts and rejects profile configurations.

These tests use the `jsonschema` Python package to exercise the schema as
an external consumer (e.g. a Rails web UI) would.

jsonschema is installed automatically by the test setup if not present.
"""

import json
import subprocess
import sys

import pytest

# ---------------------------------------------------------------------------
# Ensure jsonschema is available; skip gracefully if it can't be installed.
# ---------------------------------------------------------------------------
try:
    from jsonschema import Draft202012Validator
except ImportError:
    subprocess.run(
        [sys.executable, "-m", "pip", "install", "jsonschema", "-q"],
        check=True,
    )
    from jsonschema import Draft202012Validator


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


@pytest.fixture(scope="module")
def profile_schema(coi_binary):
    """Run `coi schema profile` once per module and return the parsed schema."""
    result = subprocess.run(
        [coi_binary, "schema", "profile"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"coi schema profile failed:\n{result.stderr}"
    return json.loads(result.stdout)


@pytest.fixture(scope="module")
def validator(profile_schema):
    """Return a Draft202012Validator pre-loaded with the profile schema."""
    Draft202012Validator.check_schema(profile_schema)
    return Draft202012Validator(profile_schema)


# ---------------------------------------------------------------------------
# Schema structure tests
# ---------------------------------------------------------------------------


def test_schema_output_is_valid_json(coi_binary):
    """coi schema profile must emit valid JSON."""
    result = subprocess.run(
        [coi_binary, "schema", "profile"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0
    json.loads(result.stdout)  # raises if invalid


def test_schema_meta_fields(profile_schema):
    """Schema must declare required meta-fields."""
    assert profile_schema.get("$schema") == "https://json-schema.org/draft/2020-12/schema"
    assert "$id" in profile_schema
    assert "title" in profile_schema
    assert profile_schema.get("type") == "object"


def test_schema_top_level_properties(profile_schema):
    """All expected top-level profile keys must be declared."""
    props = profile_schema.get("properties", {})
    expected = {
        "inherits",
        "context",
        "model",
        "forward_env",
        "environment",
        "container",
        "limits",
        "tool",
        "mounts",
        "network",
        "paths",
        "incus",
        "git",
        "ssh",
        "security",
        "monitoring",
        "timezone",
        "shell",
    }
    missing = expected - set(props.keys())
    assert not missing, f"Schema is missing top-level properties: {missing}"


def test_schema_defs_present(profile_schema):
    """All referenced $defs must be present in the schema document."""
    defs = profile_schema.get("$defs", {})
    expected_defs = {
        "ContainerConfig",
        "BuildConfig",
        "LimitsConfig",
        "CPULimits",
        "MemoryLimits",
        "DiskLimits",
        "RuntimeLimits",
        "ToolConfig",
        "ClaudeToolConfig",
        "MountEntry",
        "NetworkConfig",
        "NetworkLoggingConfig",
        "PathsConfig",
        "IncusConfig",
        "GitConfig",
        "SSHConfig",
        "SecurityConfig",
        "MonitoringConfig",
        "NFTMonitoringConfig",
        "TimezoneConfig",
        "ShellConfig",
    }
    missing = expected_defs - set(defs.keys())
    assert not missing, f"Schema is missing $defs entries: {missing}"


def test_schema_is_valid_draft202012(profile_schema):
    """The schema itself must be a valid JSON Schema 2020-12 document."""
    Draft202012Validator.check_schema(profile_schema)


# ---------------------------------------------------------------------------
# Valid profile tests
# ---------------------------------------------------------------------------


def test_empty_profile_is_valid(validator):
    """An empty profile (no keys at all) must be valid — everything is optional."""
    validator.validate({})


def test_minimal_profile_with_image(validator):
    """A profile that only sets a container image must be valid."""
    validator.validate({"container": {"image": "coi-rust"}})


def test_full_profile_is_valid(validator):
    """A profile exercising every section must pass validation."""
    profile = {
        "inherits": "default",
        "context": "CONTEXT.md",
        "model": "claude-opus-4-5",
        "forward_env": ["RUST_BACKTRACE", "CARGO_HOME"],
        "environment": {"RUST_BACKTRACE": "1", "EDITOR": "vim"},
        "container": {
            "image": "coi-rust",
            "persistent": True,
            "storage_pool": "default",
            "alias": "rust-workspace",
            "build": {
                "base": "coi",
                "script": "build.sh",
                "commands": ["apt-get install -y rustup"],
            },
        },
        "limits": {
            "cpu": {"count": "4", "allowance": "50%", "priority": 5},
            "memory": {"limit": "8GiB", "enforce": "hard", "swap": "false"},
            "disk": {
                "read": "100MiB/s",
                "write": "50MiB/s",
                "max": "150MiB/s",
                "priority": 3,
                "tmpfs_size": "4GiB",
            },
            "runtime": {
                "max_duration": "4h",
                "max_processes": 512,
                "auto_stop": True,
                "stop_graceful": True,
            },
        },
        "tool": {
            "name": "claude",
            "binary": "claude",
            "permission_mode": "bypass",
            "context_file": "~/.coi/context.md",
            "auto_context": True,
            "claude": {"effort_level": "high"},
        },
        "mounts": [
            {"host": "~/.cargo", "container": "/home/code/.cargo"},
            {"host": "~/.rustup", "container": "/home/code/.rustup", "readonly": True},
        ],
        "network": {
            "mode": "restricted",
            "block_private_networks": True,
            "block_metadata_endpoint": True,
            "allowed_domains": ["crates.io", "github.com"],
            "refresh_interval_minutes": 60,
            "allow_local_network_access": False,
            "logging": {"enabled": True, "path": "/tmp/net.log"},
        },
        "paths": {
            "sessions_dir": "~/.coi/sessions",
            "storage_dir": "~/.coi/storage",
            "logs_dir": "~/.coi/logs",
            "preserve_workspace_path": False,
        },
        "incus": {
            "project": "default",
            "group": "incus",
            "code_uid": 1000,
            "code_user": "code",
            "disable_shift": False,
        },
        "git": {"writable_hooks": False},
        "ssh": {"forward_agent": True},
        "security": {
            "protected_paths": [".git/hooks", ".git/config"],
            "additional_protected_paths": [".husky"],
            "disable_protection": False,
            "host_immutable": True,
        },
        "monitoring": {
            "enabled": True,
            "auto_pause_on_high": True,
            "auto_kill_on_critical": True,
            "poll_interval_sec": 5,
            "file_read_threshold_mb": 500.0,
            "file_read_rate_mb_per_sec": 100.0,
            "audit_log_retention_days": 30,
            "nft": {
                "enabled": True,
                "rate_limit_per_second": 100,
                "dns_query_threshold": 200,
                "log_dns_queries": True,
                "lima_host": "",
            },
        },
        "timezone": {"mode": "fixed", "name": "Europe/Warsaw"},
        "shell": {"use_tmux": True},
    }
    validator.validate(profile)


def test_network_mode_open(validator):
    validator.validate({"network": {"mode": "open"}})


def test_network_mode_allowlist(validator):
    validator.validate(
        {"network": {"mode": "allowlist", "allowed_domains": ["example.com"]}}
    )


def test_tool_permission_mode_interactive(validator):
    validator.validate({"tool": {"permission_mode": "interactive"}})


def test_claude_effort_level_variants(validator):
    for level in ["low", "medium", "high", "xhigh", "max", "auto", ""]:
        validator.validate({"tool": {"claude": {"effort_level": level}}})


def test_timezone_utc_mode(validator):
    validator.validate({"timezone": {"mode": "utc"}})


def test_timezone_host_mode(validator):
    validator.validate({"timezone": {"mode": "host"}})


def test_memory_enforce_soft(validator):
    validator.validate({"limits": {"memory": {"enforce": "soft"}}})


# ---------------------------------------------------------------------------
# Invalid profile tests
# ---------------------------------------------------------------------------


def test_invalid_network_mode_rejected(validator):
    """Unknown network mode must fail validation."""
    errors = list(validator.iter_errors({"network": {"mode": "blocked"}}))
    assert errors, "Expected validation error for unknown network mode"
    paths = [list(e.absolute_path) for e in errors]
    assert any("mode" in p for p in paths), f"Error should point to 'mode', got: {paths}"


def test_invalid_permission_mode_rejected(validator):
    """Unknown permission mode must fail validation."""
    errors = list(validator.iter_errors({"tool": {"permission_mode": "auto"}}))
    assert errors, "Expected validation error for unknown permission_mode"


def test_invalid_effort_level_rejected(validator):
    """Unknown effort level must fail validation."""
    errors = list(validator.iter_errors({"tool": {"claude": {"effort_level": "turbo"}}}))
    assert errors, "Expected validation error for unknown effort_level"


def test_invalid_timezone_mode_rejected(validator):
    """Unknown timezone mode must fail validation."""
    errors = list(validator.iter_errors({"timezone": {"mode": "localtime"}}))
    assert errors, "Expected validation error for unknown timezone mode"


def test_invalid_memory_enforce_rejected(validator):
    """Unknown memory enforce value must fail validation."""
    errors = list(validator.iter_errors({"limits": {"memory": {"enforce": "oom-kill"}}}))
    assert errors, "Expected validation error for unknown memory enforce value"


def test_mount_missing_host_rejected(validator):
    """A mount entry without 'host' must fail validation."""
    errors = list(validator.iter_errors({"mounts": [{"container": "/data"}]}))
    assert errors, "Expected validation error for mount missing 'host'"


def test_mount_missing_container_rejected(validator):
    """A mount entry without 'container' must fail validation."""
    errors = list(validator.iter_errors({"mounts": [{"host": "~/data"}]}))
    assert errors, "Expected validation error for mount missing 'container'"


def test_unknown_top_level_key_rejected(validator):
    """Unknown top-level keys must fail validation (additionalProperties: false)."""
    errors = list(validator.iter_errors({"unknown_key": "value"}))
    assert errors, "Expected validation error for unknown top-level key"


def test_unknown_container_key_rejected(validator):
    """Unknown keys inside [container] must fail validation."""
    errors = list(validator.iter_errors({"container": {"typo_key": "oops"}}))
    assert errors, "Expected validation error for unknown key in container"


def test_cpu_priority_out_of_range(validator):
    """CPU priority > 10 must fail validation."""
    errors = list(validator.iter_errors({"limits": {"cpu": {"priority": 11}}}))
    assert errors, "Expected validation error for CPU priority > 10"


def test_wrong_type_for_persistent(validator):
    """container.persistent must be boolean, not string."""
    errors = list(validator.iter_errors({"container": {"persistent": "yes"}}))
    assert errors, "Expected validation error for persistent='yes' (should be boolean)"


def test_wrong_type_for_environment_value(validator):
    """Environment values must be strings, not integers."""
    errors = list(validator.iter_errors({"environment": {"PORT": 8080}}))
    assert errors, "Expected validation error for integer environment value"


# ---------------------------------------------------------------------------
# Schema command output stability tests
# ---------------------------------------------------------------------------


def test_schema_command_output_is_deterministic(coi_binary):
    """Running `coi schema profile` twice must produce identical output."""
    runs = []
    for _ in range(2):
        result = subprocess.run(
            [coi_binary, "schema", "profile"],
            capture_output=True,
            text=True,
            timeout=30,
        )
        assert result.returncode == 0
        runs.append(result.stdout)
    assert runs[0] == runs[1], "Schema output is not deterministic"


def test_schema_command_exits_zero_without_config(tmp_path, coi_binary):
    """schema profile must work even when there is no COI config in the workspace."""
    result = subprocess.run(
        [coi_binary, "schema", "profile", "--workspace", str(tmp_path)],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=str(tmp_path),
    )
    assert result.returncode == 0, (
        f"Expected exit 0 in empty workspace, got {result.returncode}:\n{result.stderr}"
    )
    json.loads(result.stdout)
