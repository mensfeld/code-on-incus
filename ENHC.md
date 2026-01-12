# Enhancement Plan: Programmatic Output Formats for claude_yard Integration

## Overview

This document outlines enhancements to `claude-on-incus` (coi) to enable full programmatic integration with `claude_yard`. The goal is to replace direct Incus CLI calls in `claude_yard` with `coi` commands, providing better encapsulation, shared maintenance, and consistent behavior.

## Current Status

`claude-on-incus` was extracted from `claude_yard` as a standalone CLI tool for running Claude Code in Incus containers. Currently:

- ✅ **Core operations covered**: launch, stop, delete, exec, mount, file push/pull, image publish
- ✅ **Interactive use optimized**: Human-readable output, confirmation prompts
- ⚠️ **Programmatic use limited**: Some commands lack machine-readable output

## Required Enhancements

To fully support `claude_yard` integration, we need **two format flags**:

1. **`--format=json` for `coi list`** - Machine-readable container/session listing
2. **`--format=raw` for `coi container exec --capture`** - Clean stdout without JSON wrapper

---

## Enhancement 1: JSON Output for `coi list`

### Motivation

**claude_yard currently:**
```ruby
output = incus_output("list", container_name, "--format=json")
containers = JSON.parse(output)
containers.any? { |c| c["name"] == container_name && c["status"] == "Running" }
```

**With coi:**
```ruby
output = `coi list --format=json`
data = JSON.parse(output)
data["active_containers"].any? { |c| c["name"] == container_name }
```

### Implementation

#### File: `internal/cli/list.go`

**1. Add format flag variable** (after line 17):
```go
var (
    listAll    bool
    listFormat string  // NEW
)
```

**2. Register flag in `init()`** (after line 35):
```go
func init() {
    listCmd.Flags().BoolVar(&listAll, "all", false, "Show saved sessions in addition to active containers")
    listCmd.Flags().StringVar(&listFormat, "format", "text", "Output format: text or json")  // NEW
}
```

**3. Refactor `listCommand()` function** (lines 38-121):

Split into three functions:

```go
func listCommand(cmd *cobra.Command, args []string) error {
    cfg, err := config.Load()
    if err != nil {
        return fmt.Errorf("failed to load config: %w", err)
    }

    // Get containers
    containers, err := listActiveContainers()
    if err != nil {
        return fmt.Errorf("failed to list containers: %w", err)
    }

    // Build metadata maps
    containerWorkspaces := make(map[string]string)
    containerPersistent := make(map[string]bool)

    for _, c := range containers {
        // Extract workspace from metadata
        if strings.HasPrefix(c.Name, "coi-") {
            metadataPath := filepath.Join(cfg.Paths.SessionsDir, c.Name, "metadata.json")
            if data, err := os.ReadFile(metadataPath); err == nil {
                var metadata map[string]interface{}
                if err := json.Unmarshal(data, &metadata); err == nil {
                    if ws, ok := metadata["workspace"].(string); ok {
                        containerWorkspaces[c.Name] = ws
                    }
                }
            }
        }

        // Determine if persistent (no ephemeral in config)
        containerPersistent[c.Name] = !strings.Contains(c.Config, "ephemeral")
    }

    // Get saved sessions if --all
    var sessions []SessionInfo
    if listAll {
        sessions, err = listSavedSessions(cfg.Paths.SessionsDir)
        if err != nil {
            return fmt.Errorf("failed to list sessions: %w", err)
        }
    }

    // Route to formatter
    if listFormat == "json" {
        return outputJSON(containers, sessions, containerWorkspaces, containerPersistent)
    }

    return outputText(containers, sessions, containerWorkspaces, containerPersistent)
}
```

**4. Add JSON formatter function:**

```go
// outputJSON formats container and session data as JSON
func outputJSON(containers []ContainerInfo, sessions []SessionInfo,
                workspaces map[string]string, persistent map[string]bool) error {

    // Enrich container data
    enrichedContainers := make([]map[string]interface{}, 0, len(containers))
    for _, c := range containers {
        item := map[string]interface{}{
            "name":       c.Name,
            "status":     c.Status,
            "created_at": c.CreatedAt,
            "image":      c.Image,
            "persistent": persistent[c.Name],
        }
        if ws, ok := workspaces[c.Name]; ok {
            item["workspace"] = ws
        }
        enrichedContainers = append(enrichedContainers, item)
    }

    // Build output structure
    output := map[string]interface{}{
        "active_containers": enrichedContainers,
    }

    if len(sessions) > 0 {
        output["saved_sessions"] = sessions
    }

    // Marshal to JSON
    jsonData, err := json.MarshalIndent(output, "", "  ")
    if err != nil {
        return fmt.Errorf("failed to marshal JSON: %w", err)
    }

    fmt.Println(string(jsonData))
    return nil
}
```

**5. Extract text formatter function:**

```go
// outputText formats container and session data as human-readable text
func outputText(containers []ContainerInfo, sessions []SessionInfo,
                workspaces map[string]string, persistent map[string]bool) error {

    // Active Containers section
    if len(containers) > 0 {
        fmt.Println("Active Containers:")
        fmt.Println("------------------")

        for _, c := range containers {
            mode := "ephemeral"
            if persistent[c.Name] {
                mode = "persistent"
            }
            fmt.Printf("  %s (%s)\n", c.Name, mode)
            fmt.Printf("    Status: %s\n", c.Status)
            fmt.Printf("    Created: %s\n", c.CreatedAt)
            fmt.Printf("    Image: %s\n", c.Image)

            if workspace, ok := workspaces[c.Name]; ok {
                fmt.Printf("    Workspace: %s\n", workspace)
            }
            fmt.Println()
        }
    } else {
        fmt.Println("No active containers found.")
        fmt.Println("\nUse 'coi shell' to start a new session.")
    }

    // Saved Sessions section (only with --all)
    if len(sessions) > 0 {
        fmt.Println("\nSaved Sessions:")
        fmt.Println("---------------")

        for _, s := range sessions {
            fmt.Printf("  %s\n", s.ID)
            fmt.Printf("    Saved: %s\n", s.SavedAt)

            if s.Workspace != "" {
                fmt.Printf("    Workspace: %s\n", s.Workspace)
            }

            // Calculate data size
            sessionDir := filepath.Join(cfg.Paths.SessionsDir, s.ID)
            if size := getDirSize(sessionDir); size > 0 {
                fmt.Printf("    Data: %s\n", formatBytes(size))
            }

            fmt.Printf("\n    Resume: coi shell --resume %s\n", s.ID)
            fmt.Println()
        }
    }

    return nil
}
```

### Expected Output

**Command:**
```bash
coi list --format=json
```

**Output:**
```json
{
  "active_containers": [
    {
      "name": "coi-abc12345-1",
      "status": "Running",
      "created_at": "2026-01-12 10:30:00 UTC",
      "image": "Ubuntu 22.04 LTS",
      "persistent": true,
      "workspace": "/home/user/project"
    },
    {
      "name": "coi-def67890-1",
      "status": "Running",
      "created_at": "2026-01-12 11:15:00 UTC",
      "image": "Ubuntu 22.04 LTS",
      "persistent": false,
      "workspace": "/home/user/other-project"
    }
  ]
}
```

**With `--all` flag:**
```json
{
  "active_containers": [...],
  "saved_sessions": [
    {
      "id": "coi-abc12345-1",
      "saved_at": "2026-01-12 12:00:00 UTC",
      "workspace": "/home/user/project"
    }
  ]
}
```

### Testing

**Create test file:** `tests/list/list_format_json_active.py`

```python
"""Test coi list --format=json with active containers"""
import json
import subprocess
from support.helpers import calculate_container_name

def test_list_format_json_active(coi_binary, cleanup_containers, workspace_dir):
    """Test that coi list --format=json outputs valid JSON with active containers."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Phase 1: Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Launch failed: {result.stderr}"

    # Phase 2: Run list with JSON format
    result = subprocess.run(
        [coi_binary, "list", "--format=json"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List failed: {result.stderr}"

    # Phase 3: Parse and verify JSON
    data = json.loads(result.stdout)

    # Verify structure
    assert "active_containers" in data, "Missing 'active_containers' key"
    assert isinstance(data["active_containers"], list), "active_containers should be a list"
    assert len(data["active_containers"]) > 0, "Should have at least one container"

    # Find our container
    container = None
    for c in data["active_containers"]:
        if c["name"] == container_name:
            container = c
            break

    assert container is not None, f"Container {container_name} not found in output"

    # Verify container fields
    assert container["status"] == "Running", "Container should be running"
    assert "created_at" in container, "Missing created_at field"
    assert "image" in container, "Missing image field"
    assert "persistent" in container, "Missing persistent field"
    assert isinstance(container["persistent"], bool), "persistent should be boolean"

    # Phase 4: Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
```

**Create test file:** `tests/list/list_format_json_empty.py`

```python
"""Test coi list --format=json with no containers"""
import json
import subprocess

def test_list_format_json_empty(coi_binary):
    """Test that coi list --format=json outputs valid JSON with no containers."""

    # Run list with JSON format (no containers running)
    result = subprocess.run(
        [coi_binary, "list", "--format=json"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List failed: {result.stderr}"

    # Parse and verify JSON
    data = json.loads(result.stdout)

    # Verify structure
    assert "active_containers" in data, "Missing 'active_containers' key"
    assert isinstance(data["active_containers"], list), "active_containers should be a list"
    assert len(data["active_containers"]) == 0, "Should have no containers"
```

**Create test file:** `tests/list/list_format_text_default.py`

```python
"""Test that coi list defaults to text format"""
import subprocess
from support.helpers import calculate_container_name

def test_list_format_text_default(coi_binary, cleanup_containers, workspace_dir):
    """Test that coi list without --format outputs human-readable text."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0

    # Run list without format flag
    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0

    # Verify text output
    output = result.stdout
    assert "Active Containers:" in output, "Should have text header"
    assert container_name in output, "Should show container name"
    assert "Running" in output or "Status:" in output, "Should show status"

    # Should NOT be JSON
    assert not output.strip().startswith("{"), "Should not output JSON by default"

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
```

---

## Enhancement 2: Raw Output for `coi container exec --capture`

### Motivation

**claude_yard currently:**
```ruby
stdout, _stderr, status = Open3.capture3("incus exec container -- command")
result = stdout.strip  # Expects raw string
```

**With coi (current):**
```bash
coi container exec --capture container -- command
# Returns: {"stdout": "result", "stderr": "", "exit_code": 0}
# Must parse JSON to extract stdout
```

**With coi (enhanced):**
```bash
coi container exec --capture --format=raw container -- command
# Returns: result (raw stdout)
# Exit code via $?
```

### Implementation

#### File: `internal/cli/container.go`

**1. Register format flag in `init()`** (after line 303):

```go
func init() {
    // ... existing flags ...
    containerExecCmd.Flags().Bool("capture", false, "Capture output as JSON")
    containerExecCmd.Flags().String("format", "json", "Output format when using --capture: json or raw")  // NEW
    // ... rest of init ...
}
```

**2. Modify capture logic** (lines 124-169):

```go
if capture {
    // Parse flags
    userFlag, _ := cmd.Flags().GetInt("user")
    groupFlag, _ := cmd.Flags().GetInt("group")
    envVars, _ := cmd.Flags().GetStringArray("env")
    cwd, _ := cmd.Flags().GetString("cwd")
    format, _ := cmd.Flags().GetString("format")  // NEW

    // Parse environment variables
    env := make(map[string]string)
    for _, e := range envVars {
        parts := strings.SplitN(e, "=", 2)
        if len(parts) == 2 {
            env[parts[0]] = parts[1]
        }
    }

    // Build execution options
    opts := container.ExecCommandOptions{
        Cwd:     cwd,
        Env:     env,
        Capture: true,
    }

    if cmd.Flags().Changed("user") {
        opts.User = &userFlag
    }
    if cmd.Flags().Changed("group") {
        opts.Group = &groupFlag
    }

    // Execute command
    output, err := mgr.ExecCommand(command, opts)

    // Handle raw format - output stdout and exit with proper code
    if format == "raw" {
        fmt.Print(output)  // No newline, preserve exact output
        if err != nil {
            // Exit with code 1 on error
            os.Exit(1)
        }
        return nil
    }

    // Handle JSON format (default)
    result := map[string]interface{}{
        "stdout":    output,
        "stderr":    "",
        "exit_code": 0,
    }
    if err != nil {
        result["exit_code"] = 1
        result["stderr"] = err.Error()
    }

    jsonOutput, _ := json.MarshalIndent(result, "", "  ")
    fmt.Println(string(jsonOutput))
    return nil
}
```

### Expected Output

**JSON format (default):**
```bash
$ coi container exec --capture my-container -- echo "hello"
{
  "stdout": "hello\n",
  "stderr": "",
  "exit_code": 0
}
```

**Raw format:**
```bash
$ coi container exec --capture --format=raw my-container -- echo "hello"
hello

$ echo $?
0
```

**Raw format with error:**
```bash
$ coi container exec --capture --format=raw my-container -- false
$ echo $?
1
```

### Testing

**Create test file:** `tests/container/exec_capture_format_raw.py`

```python
"""Test coi container exec --capture --format=raw"""
import subprocess
from support.helpers import calculate_container_name

def test_exec_capture_format_raw(coi_binary, cleanup_containers, workspace_dir):
    """Test that --capture --format=raw outputs raw stdout."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Phase 1: Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0

    # Phase 2: Execute command with raw format
    result = subprocess.run(
        [coi_binary, "container", "exec", "--capture", "--format=raw",
         container_name, "--", "echo", "hello world"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Verify raw output
    assert result.returncode == 0, "Command should succeed"
    assert result.stdout == "hello world\n", f"Expected 'hello world\\n', got '{result.stdout}'"

    # Should NOT be JSON
    assert not result.stdout.strip().startswith("{"), "Should not output JSON"

    # Phase 3: Test command failure
    result = subprocess.run(
        [coi_binary, "container", "exec", "--capture", "--format=raw",
         container_name, "--", "false"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Verify exit code propagation
    assert result.returncode == 1, "Should exit with code 1 for failed command"

    # Phase 4: Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
```

**Create test file:** `tests/container/exec_capture_format_json.py`

```python
"""Test coi container exec --capture (JSON format, default)"""
import json
import subprocess
from support.helpers import calculate_container_name

def test_exec_capture_format_json(coi_binary, cleanup_containers, workspace_dir):
    """Test that --capture outputs JSON by default."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0

    # Execute command with capture (no format flag)
    result = subprocess.run(
        [coi_binary, "container", "exec", "--capture",
         container_name, "--", "echo", "test output"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Command failed: {result.stderr}"

    # Parse JSON
    data = json.loads(result.stdout)

    # Verify structure
    assert "stdout" in data, "Missing stdout field"
    assert "stderr" in data, "Missing stderr field"
    assert "exit_code" in data, "Missing exit_code field"

    # Verify values
    assert data["stdout"] == "test output\n", f"Unexpected stdout: {data['stdout']}"
    assert data["exit_code"] == 0, "Exit code should be 0"

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
```

---

## Integration Guide for claude_yard

Once these enhancements are implemented, `claude_yard` can replace direct Incus calls with `coi` commands.

### Replacement Mapping

#### 1. Container Status Checks

**Before (direct Incus):**
```ruby
def running?
  output = capture_incus("list", container_name, "--format=json")
  containers = JSON.parse(output)
  containers.any? { |c| c["name"] == container_name && c["status"] == "Running" }
end
```

**After (using coi):**
```ruby
def running?
  # Option A: Use exit code (simpler)
  system("coi", "container", "running", container_name, out: File::NULL, err: File::NULL)

  # Option B: Parse JSON list (more info)
  output = `coi list --format=json 2>/dev/null`
  return false if output.empty?
  data = JSON.parse(output)
  data["active_containers"].any? { |c| c["name"] == container_name && c["status"] == "Running" }
end
```

#### 2. Image Existence Checks

**Before:**
```ruby
def image_exists?(alias_name)
  output = incus_output("image", "list", "--format=json")
  images = JSON.parse(output)
  images.any? { |img| img["aliases"]&.any? { |a| a["name"] == alias_name } }
end
```

**After:**
```ruby
def image_exists?(alias_name)
  system("coi", "image", "exists", alias_name, out: File::NULL, err: File::NULL)
end
```

#### 3. Execute Command and Capture Output

**Before:**
```ruby
def exec_command(command, user: nil, group: nil, cwd: "/workspace", env: {}, capture: false)
  # Build incus exec command with flags
  args = build_exec_args(user: user, group: group, cwd: cwd, env: env)
  args += ["--", "bash", "-c", command]

  if capture
    capture_incus(*args)
  else
    run_incus(*args)
  end
end
```

**After:**
```ruby
def exec_command(command, user: nil, group: nil, cwd: "/workspace", env: {}, capture: false)
  args = ["coi", "container", "exec"]

  # Add capture flag with raw format for clean stdout
  args += ["--capture", "--format=raw"] if capture

  # Add user context
  args += ["--user", user.to_s] if user
  args += ["--group", (group || user).to_s] if user

  # Add working directory
  args += ["--cwd", cwd]

  # Add environment variables
  env.each { |k, v| args += ["--env", "#{k}=#{v}"] }

  # Add container and command
  args += [container_name, "--", "bash", "-c", command]

  if capture
    `#{args.shelljoin}`.strip
  else
    system(*args)
  end
end
```

#### 4. List Containers Programmatically

**Before:**
```ruby
def list_containers_by_prefix(prefix)
  output = incus_output("list", "#{prefix}*", "--format=json")
  containers = JSON.parse(output)
  containers.map { |c| c["name"] }
end
```

**After:**
```ruby
def list_containers_by_prefix(prefix)
  output = `coi list --format=json 2>/dev/null`
  return [] if output.empty?

  data = JSON.parse(output)
  data["active_containers"]
    .select { |c| c["name"].start_with?(prefix) }
    .map { |c| c["name"] }
end
```

### Benefits of Migration

1. **Simpler Code**: Exit codes instead of JSON parsing for existence checks
2. **Better Encapsulation**: Incus implementation details hidden
3. **Shared Maintenance**: Bug fixes benefit both projects
4. **Consistent Behavior**: UID shifting, permission handling, etc. centralized
5. **Easier Testing**: Mock `coi` commands instead of low-level `incus` calls

### Migration Strategy

**Phase 1: Add coi as Dependency**
- Install coi binary in claude_yard deployment
- Add `Containers::CoiCommands` module as alternative to `Containers::Commands`

**Phase 2: Gradual Migration**
- Migrate non-critical operations first (existence checks, simple exec)
- Test thoroughly in development environment
- Keep `Containers::Commands` as fallback

**Phase 3: Full Cutover**
- Replace all direct Incus calls with coi equivalents
- Remove `Containers::Commands` module
- Update documentation

---

## Implementation Checklist

### Enhancement 1: JSON Output for `coi list`
- [ ] Add `listFormat` variable to `internal/cli/list.go`
- [ ] Register `--format` flag in `init()`
- [ ] Create `outputJSON()` function
- [ ] Create `outputText()` function
- [ ] Refactor `listCommand()` to route to formatters
- [ ] Write test: `tests/list/list_format_json_active.py`
- [ ] Write test: `tests/list/list_format_json_empty.py`
- [ ] Write test: `tests/list/list_format_text_default.py`
- [ ] Update README with `--format` flag documentation
- [ ] Update `coi list --help` text

### Enhancement 2: Raw Output for `coi container exec --capture`
- [ ] Register `--format` flag in `init()` of `internal/cli/container.go`
- [ ] Add format handling in capture logic
- [ ] Handle raw format output (stdout only)
- [ ] Handle raw format exit codes
- [ ] Write test: `tests/container/exec_capture_format_raw.py`
- [ ] Write test: `tests/container/exec_capture_format_json.py`
- [ ] Update README with `--format` flag documentation
- [ ] Update `coi container exec --help` text

### Integration with claude_yard
- [ ] Install coi binary in claude_yard environment
- [ ] Create `Containers::CoiCommands` wrapper module
- [ ] Write integration tests in claude_yard
- [ ] Update claude_yard documentation
- [ ] Plan migration phases

---

## Documentation Updates Needed

### README.md

Add to `coi list` section:
```markdown
### coi list

List active containers and saved sessions.

Usage:
  coi list [flags]

Flags:
  --all            Show saved sessions in addition to active containers
  --format string  Output format: text or json (default "text")

Examples:
  # Human-readable output (default)
  coi list

  # Machine-readable JSON output
  coi list --format=json

  # Include saved sessions
  coi list --all --format=json
```

Add to `coi container exec` section:
```markdown
### coi container exec

Execute command in container.

Usage:
  coi container exec [flags] <container> -- <command>

Flags:
  --capture        Capture output instead of streaming
  --format string  Output format when using --capture: json or raw (default "json")
  --user int       User ID to run as
  --group int      Group ID to run as
  --cwd string     Working directory (default "/workspace")
  --env strings    Environment variables (KEY=VALUE)

Examples:
  # Interactive execution
  coi container exec my-container -- bash

  # Capture output as JSON
  coi container exec --capture my-container -- echo "hello"

  # Capture output as raw string (for scripting)
  coi container exec --capture --format=raw my-container -- pwd
```

---

## Testing Strategy

### Unit Tests (Go)
- No unit tests needed (CLI-level changes only)

### Integration Tests (Python)
- **3 new tests for `coi list --format=json`**
  - With active containers
  - Empty (no containers)
  - With `--all` flag

- **2 new tests for `coi container exec --capture --format=raw`**
  - Successful command execution
  - Failed command execution

### Manual Testing
```bash
# Test 1: List with JSON format
coi container launch coi test-container
coi list --format=json | jq '.active_containers[0].name'
# Expected: "test-container"

# Test 2: Exec with raw format
output=$(coi container exec --capture --format=raw test-container -- echo "hello")
echo "[$output]"
# Expected: [hello]

# Test 3: Exec raw format with error
coi container exec --capture --format=raw test-container -- false
echo $?
# Expected: 1

# Cleanup
coi container delete test-container --force
```

---

## Timeline Estimate

- **Enhancement 1 (list --format=json)**: 4-6 hours
  - Implementation: 2 hours
  - Testing: 2 hours
  - Documentation: 1 hour

- **Enhancement 2 (exec --format=raw)**: 2-3 hours
  - Implementation: 1 hour
  - Testing: 1 hour
  - Documentation: 0.5 hour

- **Total**: 6-9 hours

---

## Success Criteria

✅ `coi list --format=json` outputs valid, parseable JSON
✅ `coi list` (no flags) outputs human-readable text (default)
✅ `coi container exec --capture --format=raw` outputs raw stdout
✅ `coi container exec --capture` outputs JSON (default)
✅ All new integration tests pass
✅ Existing tests continue to pass
✅ Documentation updated
✅ claude_yard can successfully replace direct Incus calls with coi commands

---

## Future Enhancements (Not in Scope)

These are nice-to-haves but not required for claude_yard integration:

- [ ] `--format=json` for other commands (container launch, image list, etc.)
- [ ] `--quiet` flag for suppressing non-essential output
- [ ] Structured error output (`--error-format=json`)
- [ ] Machine-readable progress indicators

---

## Questions / Decisions Needed

1. Should `--format=json` be added to other commands beyond `list`?
   - **Recommendation**: Start with `list` only, add others as needed

2. Should we support `--format=yaml` or other formats?
   - **Recommendation**: No, JSON is sufficient for programmatic use

3. Should `--format=raw` work without `--capture` flag?
   - **Recommendation**: No, raw format only makes sense with capture mode

4. Should JSON output include additional metadata (timestamp, coi version, etc.)?
   - **Recommendation**: Keep it simple initially, can add later if needed

---

## Contact

For questions or clarifications on this enhancement plan:
- Review existing coi codebase
- Consult claude_yard integration requirements
- Test changes in development environment before merging
