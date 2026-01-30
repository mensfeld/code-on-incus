# Implementation Plan: `coi health` Command

## Overview

Design and implement a comprehensive health check command that verifies COI installation, diagnoses common issues, and provides actionable remediation steps. This addresses the need identified in issue #80 and the numerous edge cases discovered through the project's evolution.

## Background

After reviewing the README, CHANGELOG, and git history, the project has encountered numerous setup and configuration edge cases:
- Network ACL support detection (OVN vs bridge)
- DNS misconfiguration (127.0.0.53 stub resolver)
- User permissions (incus-admin group)
- Image availability issues
- OVN host routing for container services
- Colima/Lima environment detection
- Settings merge conflicts

A health check command will help users quickly diagnose these issues instead of encountering cryptic errors during usage.

## Architecture

### Package Structure
```
internal/health/          # New package for health checking
├── health.go            # Main orchestrator with HealthChecker type
├── checker.go           # Core types and Checker interface
├── incus.go            # Incus installation & permissions checks
├── config.go           # Configuration validation checks
├── images.go           # Image availability checks
├── network.go          # Network configuration checks
├── environment.go      # Environment detection checks
└── health_test.go      # Unit tests

internal/cli/health.go   # CLI command implementation
```

### Core Types
```go
type CheckStatus string  // "ok", "warning", "error", "skipped"

type CheckResult struct {
    Name        string
    Status      CheckStatus
    Message     string
    Details     map[string]interface{}
    Error       string
    Remediation string
    Duration    time.Duration
}

type HealthReport struct {
    Status        CheckStatus
    Timestamp     time.Time
    Checks        map[string]*CheckResult
    Summary       Summary
    TotalDuration time.Duration
}

type Checker interface {
    Name() string
    Category() string
    Check(ctx context.Context) *CheckResult
}
```

## Health Checks by Category

### 1. Incus Installation (Critical)
- **Binary Check**: Verify `incus` binary exists in PATH
- **Daemon Check**: Verify daemon responding (`incus info`)
- **Group Check**: Verify user in incus-admin group (Linux only)
- **Project Check**: Verify configured project exists

**Reuse**: `container.Available()` (manager.go:311-330), `container.IncusOutput()`

### 2. Configuration
- **Config File Check**: Verify config parses correctly
- **Directories Check**: Verify sessions/storage/logs dirs exist & writable
- **Mounts Check**: Validate mount configuration (no nested paths)
- **Tool Config Check**: Verify configured tool is valid

**Reuse**: `config.Load()`, `session.ValidateMounts()` (mount_validator.go:10-33)

### 3. Images
- **Default Image Check**: Verify 'coi' image exists
- **Custom Image Check**: Verify custom image exists (if configured)

**Reuse**: `container.ImageExists()`

### 4. Network (Complex)
- **Network Exists Check**: Verify default network accessible
- **Network Type Check**: Detect bridge vs OVN
- **ACL Support Check**: Verify ACL support if using restricted/allowlist mode
- **DNS Check**: Test DNS resolution (8.8.8.8, 1.1.1.1)
- **OVN Route Check**: Verify host route if OVN network

**Reuse**:
- `network.networkSupportsACLs()` (acl.go:128-151)
- `network.isOVNNetwork()` (manager.go:505-522)
- `network.routeExists()` (manager.go:589-607)

### 5. Environment
- **Colima/Lima Check**: Detect VM environment
- **UID Shift Check**: Verify shift config appropriate for environment

**Reuse**: `session.isColimaOrLimaEnvironment()` (setup.go:26-46)

### 6. Tool Configuration
- **Tool Config Dir Check**: Verify tool config directory accessible
- **Sessions Dir Check**: Verify tool-specific sessions directory

**Reuse**: `tool.Tool` interface from config

## CLI Command Specification

### Usage
```bash
coi health                    # Run health checks (text output)
coi health --verbose          # Show detailed output
coi health --format=json      # JSON output for automation
coi health --timeout=30       # Custom timeout in seconds
```

### Exit Codes
- `0` - All checks passed
- `1` - One or more errors (blocking issues)
- `2` - Warnings only (system functional but non-optimal)

### Text Output Format
```
Code on Incus - Health Check
============================

✓ Incus Installation
  ✓ Incus binary found (/usr/bin/incus)
  ✓ Incus daemon responding (version 6.7)
  ✓ User in incus-admin group

✗ Images
  ✗ Default image 'coi' not found
    → Run: coi build

⚠ Network
  ✓ Network 'ovn-net' exists (OVN)
  ✓ Network ACL support available
  ⚠ OVN host route not configured
    → Run: sudo ip route add 10.128.178.0/24 via 10.47.62.100 dev incusbr0

Summary
-------
Passed: 5, Warnings: 1, Errors: 1

Run 'coi health --verbose' for detailed diagnostics
```

### JSON Output Format
```json
{
  "status": "error",
  "timestamp": "2026-01-29T10:00:00Z",
  "checks": {
    "incus": {
      "status": "ok",
      "details": {
        "binary": { "status": "ok", "message": "Incus binary found", ... },
        "daemon": { "status": "ok", "message": "Daemon responding", ... }
      }
    },
    "images": {
      "status": "error",
      "details": {
        "default": {
          "status": "error",
          "error": "Image 'coi' not found",
          "remediation": "coi build"
        }
      }
    }
  },
  "summary": { "passed": 5, "warned": 1, "failed": 1, "skipped": 0 },
  "total_duration": "1.234s"
}
```

## Critical Files

### New Files to Create
1. `internal/health/checker.go` - Core types and interfaces
2. `internal/health/health.go` - Main orchestrator
3. `internal/health/incus.go` - Incus checks (highest priority)
4. `internal/health/config.go` - Config checks
5. `internal/health/images.go` - Image checks
6. `internal/health/network.go` - Network checks (complex but essential)
7. `internal/health/environment.go` - Environment detection
8. `internal/cli/health.go` - CLI command

### Files to Modify
1. `internal/cli/root.go` - Register health command (line ~113)

### Files to Reference
1. `internal/cli/list.go` - CLI pattern example (lines 1-100)
2. `internal/container/manager.go` - Available() function (lines 310-330)
3. `internal/network/acl.go` - ACL support detection (lines 128-151)
4. `internal/network/manager.go` - Network utilities (lines 354-631)
5. `internal/session/setup.go` - Environment detection (lines 26-46)
6. `internal/session/mount_validator.go` - Mount validation (lines 10-33)

## Implementation Order

### Phase 1: Core Infrastructure (Day 1)
1. Create `internal/health/checker.go`
   - Define `CheckStatus`, `CheckResult`, `HealthReport`, `Checker` interface
2. Create `internal/health/health.go`
   - Implement `HealthChecker` with `Check()` orchestrator
   - Implement checker registration
3. Create `internal/cli/health.go`
   - Basic CLI command structure
   - Text output formatter
4. Register command in `internal/cli/root.go`

**Milestone**: Basic `coi health` runs (no checks yet)

### Phase 2: Critical Checks (Day 2)
1. Create `internal/health/incus.go`
   - BinaryChecker, DaemonChecker, GroupChecker, ProjectChecker
2. Create `internal/health/config.go`
   - ConfigFileChecker, DirectoriesChecker, MountsChecker
3. Create `internal/health/images.go`
   - DefaultImageChecker, CustomImageChecker
4. Add verbose mode to CLI

**Milestone**: Core checks working, helpful error messages

### Phase 3: Advanced Checks (Day 3)
1. Create `internal/health/network.go`
   - NetworkExistsChecker, NetworkTypeChecker
   - ACLSupportChecker, DNSChecker, OVNRouteChecker
2. Create `internal/health/environment.go`
   - ColimaLimaChecker, UIDShiftChecker
3. Add JSON output support to CLI

**Milestone**: Complete health command with all checks

### Phase 4: Testing & Polish (Day 4)
1. Create `internal/health/health_test.go`
   - Unit tests for all checkers
   - Test health report generation
2. Add integration tests in `tests/health/`
   - Test with missing dependencies
   - Test with misconfigurations
3. Update README with health command documentation
4. Performance optimization (run checks with context timeout)

**Milestone**: Production-ready health command

## Integration with Existing Code

### Functions to Reuse
```go
// Container operations
container.Available()              // Check incus availability
container.ImageExists(alias)       // Check image existence
container.IncusOutput(args...)     // Run incus commands
container.IncusExec(args...)       // Run incus commands (no output)

// Configuration
config.Load()                      // Load and validate config
config.GetDefaultConfig()          // Get default config

// Session management
session.ValidateMounts(mounts)     // Validate mount config
session.isColimaOrLimaEnvironment() // Detect VM environment

// Network operations
network.networkSupportsACLs()      // Check ACL support (need to expose)
network.isOVNNetwork()             // Check network type (need to expose)
network.routeExists()              // Check host route (need to expose)
```

### New Helper Functions Needed
```go
// Check DNS resolution
func checkDNSResolution(domain string) error

// Check directory writability
func checkDirectoryWritable(path string) error

// Get Incus version string
func getIncusVersion() (string, error)

// Check if user is in group (Linux)
func checkUserInGroup(groupName string) (bool, error)
```

## Testing Strategy

### Unit Tests
- Test each checker independently with mocked dependencies
- Test HealthChecker orchestration
- Test output formatters (text and JSON)
- Test error aggregation and status calculation

### Integration Tests
- Test in CI environment with real Incus setup
- Test with missing dependencies (no incus binary)
- Test with misconfigured network (bridge vs OVN)
- Test with missing images
- Test with invalid config files

### Mock Strategy
- Mock Incus commands for unit tests
- Use real Incus in integration tests
- Test both success and failure paths

## Verification

After implementation, verify:
1. **Installation Issues**: Run on fresh system without Incus → clear error
2. **Missing Image**: Delete coi image → health detects and suggests `coi build`
3. **Network Issues**: Test with bridge network + restricted mode → warns about ACL
4. **Permission Issues**: Test without incus-admin group → clear error with fix
5. **OVN Routing**: Test with OVN network without route → warns with command
6. **Colima Detection**: Test in Colima VM → detects environment correctly
7. **JSON Output**: Validate JSON is parseable and complete
8. **Performance**: Health check completes in <5 seconds
9. **Exit Codes**: Verify 0 (pass), 1 (error), 2 (warning) work correctly

## Future Enhancements (Not in v1)
- `--fix` flag to auto-remediate common issues
- Check for image updates/outdated versions
- Integration with system monitoring tools
- Periodic health check scheduling
- Web UI for health status dashboard

## Success Criteria

- Health command helps users diagnose all issues mentioned in CHANGELOG
- Clear, actionable error messages for common setup problems
- Supports both human (text) and machine (JSON) consumption
- Fast (<5 seconds typical runtime)
- Follows existing CLI patterns and code style
- Comprehensive test coverage (>80%)
- Documentation in README with examples
