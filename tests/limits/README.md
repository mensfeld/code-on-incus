# Limits Integration Tests

Comprehensive integration tests for the resource and time limits feature.

## Test Coverage

### test_limits_config.py
Tests configuration loading and merging:
- Config file limits are loaded correctly
- Profile limits override global limits
- Environment variables work
- Configuration hierarchy is respected
- Empty limits means unlimited

### test_limits_validation.py
Tests validation of limit formats:
- Valid CPU count formats (1, 2, 0-3, 0,1,3)
- Invalid CPU count formats rejected
- Valid memory formats (512MiB, 2GiB, 50%)
- Invalid memory formats rejected
- Valid CPU allowance formats (50%, 25ms/100ms)
- Invalid CPU allowance formats rejected
- Valid disk I/O formats (10MiB/s, 1000iops)
- Invalid disk I/O formats rejected
- Valid duration formats (30s, 5m, 2h, 1h30m)
- Invalid duration formats rejected
- Priority range validation (0-10)

### test_limits_apply.py
Tests that limits are actually applied to containers:
- CPU limits are set correctly
- CPU allowance is applied
- Memory limits are set correctly
- Memory swap configuration applied
- Disk I/O limits applied
- Process limits applied
- Multiple limits can be combined
- CPU priority applied
- Limits work with persistent containers

### test_limits_flags.py
Tests CLI flag precedence:
- CLI flags override config file settings
- CLI flags override profile settings
- CLI flags override environment variables
- Profile overrides config
- Partial CLI override (only specified flags)
- Empty CLI flag doesn't override config

### test_limits_profile.py
Tests profile-specific limits:
- Profiles can define their own limits
- Profile limits override global config limits
- Multiple profiles can have different limits
- Profile can define partial limits
- Profile limits with global config interaction
- CLI flags override profile limits
- Profile without limits uses global config

### test_timeout_monitor.py
Tests timeout monitor functionality:
- Container auto-stops after max_duration
- Timeout works with persistent containers
- Normal exit before timeout works
- Timeout applied in shell mode
- Zero duration means unlimited
- Timeout logging message appears

## Running the Tests

### Prerequisites

1. Build the coi binary:
   ```bash
   go build -o coi ./cmd/coi
   ```

2. Ensure Incus is installed and running:
   ```bash
   incus version
   ```

3. Install Python dependencies:
   ```bash
   pip install pytest pexpect pyte
   ```

### Run All Limits Tests

```bash
# Run all limits tests
pytest tests/limits/ -v

# Run with more details
pytest tests/limits/ -vv

# Run specific test file
pytest tests/limits/test_limits_validation.py -v

# Run specific test
pytest tests/limits/test_limits_validation.py::test_valid_cpu_count_formats -v
```

### Run with Coverage

```bash
pytest tests/limits/ --cov=internal/limits --cov-report=html
```

## Test Design

### Container Naming
Tests use the workspace directory name to generate unique container names:
```python
container_name = f"coi-{Path(workspace_dir).name}-1"
```

### Cleanup
The `cleanup_containers` fixture automatically cleans up containers after each test.

### Timeouts
Tests use appropriate timeouts to prevent hanging:
- Container operations: 120s
- Config reads: 30s
- Timeout monitor tests: 60s

### Assertions
Tests verify limits by:
1. Running coi command with limits
2. Inspecting container config with `incus config show`
3. Checking for expected limit values in config output

## Common Issues

### Container Already Exists
If tests fail due to existing containers:
```bash
incus list
incus delete <container-name> --force
```

### Permission Denied
Ensure you're in the `incus-admin` group:
```bash
sudo usermod -aG incus-admin $USER
newgrp incus-admin
```

### Test Isolation
Tests use temporary workspace directories to ensure isolation.
Each test gets a unique workspace via the `workspace_dir` fixture.

## Adding New Tests

When adding new limit tests:

1. Follow the existing test structure
2. Use descriptive test names starting with `test_`
3. Include docstrings explaining what is tested
4. Use appropriate fixtures (`coi_binary`, `workspace_dir`, `cleanup_containers`)
5. Clean up resources (fixtures handle most cleanup automatically)
6. Add timeout values to prevent hanging
7. Verify both success and failure cases

Example:
```python
def test_new_limit_feature(coi_binary, workspace_dir, cleanup_containers):
    """Test that new limit feature works correctly."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Launch with new limit
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir,
         "--limit-new-feature=value", "echo", "test"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0

    # Verify limit applied
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert "expected-config-value" in result.stdout
```

## CI Integration

These tests are designed to run in CI environments:
- Use `COI_BINARY` environment variable to specify binary path
- Respect pytest fixtures for cleanup
- Use reasonable timeouts
- Handle container state properly
