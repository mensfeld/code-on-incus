# End-to-End Integration Testing Plan

## Overview

This document outlines the plan for implementing comprehensive end-to-end (E2E) integration tests for `claude-on-incus` (coi). These tests will validate the actual compiled binary through real user workflows, ensuring the CLI works correctly in production scenarios.

## Key Highlights

This testing plan includes **215 comprehensive test cases** covering:

### Advanced Workflow Coverage
- ✅ **Multi-Shell Workflows** (6 tests) - Running multiple shells for the same workspace with different slots
- ✅ **Resume & Continue** (8 tests) - Session restoration with `--resume`, continuing previous work
- ✅ **Tmux Integration** (8 tests) - Send commands, capture output, attach to sessions
- ✅ **Storage & Mounting** (5 tests) - Persistent storage, Claude config mounting, SSH keys, git config
- ✅ **Image Management** (5 tests) - Switching between sandbox/privileged, custom images, rebuilds
- ✅ **Advanced Development** (7 tests) - Microservices, frontend+backend, CI/CD, databases, dotfiles
- ✅ **Cleanup & Maintenance** (4 tests) - Periodic cleanup, workspace migration, disaster recovery

### Real User Scenarios
- Multiple terminals working on the same workspace (different slots)
- Resuming sessions with full `.claude` state restoration
- Tmux-based background process monitoring
- Persistent containers that survive restarts
- Parallel development (frontend + backend simultaneously)
- Container state isolation while sharing workspace files

### Test Execution Strategy
- **Smoke tests** (<30s) - Quick binary validation
- **E2E tests** (~10min) - Full workflow testing via binary subprocess
- **Acceptance tests** (~15min) - README examples verification
- **Performance tests** - Startup time, scalability, resource usage

## Current State

### Existing Test Coverage
- **Unit Tests**: ~25% of tests - Config, session naming, ID generation (no external deps)
- **Integration Tests**: ~25% of tests - Container management, internal APIs (requires Incus)
- **Scenario Tests**: ~45% of tests - Session lifecycle, persistent mode (mixed binary/internal)
- **Binary E2E Tests**: ~5% of tests - Basic CLI parsing and commands

### Gap Analysis
**Problem**: Only ~5% of tests actually execute the compiled `coi` binary
- Most tests call internal packages directly
- Limited validation of CLI argument parsing
- No comprehensive workflow testing
- Missing configuration hierarchy validation
- Insufficient error scenario coverage

## Testing Strategy

### Test Pyramid for coi

```
        /\
       /  \  Acceptance Tests (15 min)
      /----\  - Real user workflows from README
     /      \  - Production-like scenarios
    /--------\
   / E2E Tests \ (10 min)
  /  Binary Sub-\  - Full CLI workflows via subprocess
 /--------------\  - Multi-command scenarios
/   Integration  \ (5 min)
/    Internal API  \ - Container operations
/------------------\ - Session management
/   Unit Tests      \ (2 min)
/  Pure functions   \ - Config, naming, IDs
/--------------------\ - No external dependencies
    (Test Count ↑)
```

### Test Execution Modes

| Mode | Command | Duration | Dependencies | Use Case |
|------|---------|----------|--------------|----------|
| **Smoke** | `make test-smoke` | 30s | Binary only | Quick sanity check |
| **Unit** | `make test-unit` | 2min | None | Development feedback |
| **Integration** | `make test-integration` | 5min | Incus | Pre-commit validation |
| **E2E** | `make test-e2e` | 10min | Incus + Images | Pre-PR validation |
| **Acceptance** | `make test-acceptance` | 15min | Incus + Images | Release validation |
| **All** | `make test-all` | 30min | Full stack | CI/CD pipeline |

## Test Organization

### Directory Structure

```
/workspace/
├── tests/
│   ├── e2e/                          # NEW: End-to-end binary tests
│   │   ├── README.md                 # E2E testing guide
│   │   ├── helpers.go                # Binary execution helpers
│   │   ├── smoke_test.go             # Quick sanity checks
│   │   ├── workflows_test.go         # Multi-command workflows
│   │   ├── acceptance_test.go        # README examples
│   │   ├── configuration_test.go     # Config hierarchy tests
│   │   ├── errors_test.go            # Error scenario tests
│   │   ├── interactive_test.go       # Interactive shell tests
│   │   ├── parallel_test.go          # Concurrent operations
│   │   ├── persistence_test.go       # Persistent mode workflows
│   │   ├── session_test.go           # Session management workflows
│   │   ├── privileged_test.go        # Privileged mode workflows
│   │   └── performance_test.go       # Performance benchmarks
│   ├── fixtures/                     # Test data and configs
│   │   ├── configs/                  # Sample config files
│   │   ├── workspaces/               # Sample workspace structures
│   │   └── scripts/                  # Test scripts
│   └── integration/                  # Existing integration tests
│       └── ...
├── integrations/                     # EXISTING: Keep as-is initially
│   ├── cli/
│   ├── scenarios/
│   └── errors/
└── internal/                         # EXISTING: Unit tests
    └── ...
```

### Build Tags

```go
// Smoke tests - no build tags, always run
// tests/e2e/smoke_test.go

// E2E tests - require integration tag
// +build integration
// tests/e2e/workflows_test.go

// Acceptance tests - require integration + scenarios tags
// +build integration,scenarios
// tests/e2e/acceptance_test.go
```

## Test Case Categories

### 1. Smoke Tests (Quick Sanity Checks)

**Goal**: Verify basic binary functionality in <30 seconds

#### 1.1 Binary Existence
```
TC-SMOKE-001: Binary exists after build
  GIVEN fresh build
  WHEN make build completes
  THEN coi binary exists at ./coi
  AND binary is executable
```

#### 1.2 Help & Version
```
TC-SMOKE-002: Help command works
  GIVEN coi binary
  WHEN ./coi --help
  THEN exit code is 0
  AND output contains "claude-on-incus"
  AND output contains all 7 commands

TC-SMOKE-003: Version command works
  GIVEN coi binary
  WHEN ./coi --version
  THEN exit code is 0
  AND output matches semantic version pattern

TC-SMOKE-004: Invalid command shows help
  GIVEN coi binary
  WHEN ./coi invalid-command
  THEN exit code is 1
  AND stderr contains "unknown command"
  AND stderr suggests --help
```

#### 1.3 Quick Run
```
TC-SMOKE-005: Basic run command works
  GIVEN coi binary
  AND test workspace
  WHEN ./coi run "echo hello"
  THEN exit code is 0
  AND output contains "hello"
  AND container is cleaned up

TC-SMOKE-006: Run command with workspace
  GIVEN coi binary
  AND test workspace with file "test.txt"
  WHEN ./coi run --workspace $WORKSPACE "cat /workspace/test.txt"
  THEN exit code is 0
  AND output contains file contents
```

### 2. Command-Level E2E Tests

**Goal**: Validate each CLI command through binary execution

#### 2.1 Build Command
```
TC-BUILD-001: Build sandbox image
  GIVEN coi binary
  AND no existing sandbox image
  WHEN ./coi build sandbox
  THEN exit code is 0
  AND image "coi-sandbox" exists in Incus
  AND image contains Claude CLI
  AND image contains Docker
  AND image contains Node.js 20

TC-BUILD-002: Build privileged image
  GIVEN coi binary
  AND sandbox image exists
  WHEN ./coi build privileged
  THEN exit code is 0
  AND image "coi-privileged" exists
  AND image contains GitHub CLI
  AND image contains SSH

TC-BUILD-003: Force rebuild existing image
  GIVEN coi binary
  AND sandbox image exists
  WHEN ./coi build sandbox --force
  THEN exit code is 0
  AND image is rebuilt (new fingerprint)

TC-BUILD-004: Build with missing base fails gracefully
  GIVEN coi binary
  AND no sandbox image
  WHEN ./coi build privileged
  THEN exit code is 1
  AND stderr contains "sandbox image not found"
  AND suggests building sandbox first

TC-BUILD-005: Build invalid image type
  GIVEN coi binary
  WHEN ./coi build invalid-type
  THEN exit code is 1
  AND stderr contains "unknown image type"
  AND lists valid types (sandbox, privileged)
```

#### 2.2 Run Command
```
TC-RUN-001: Run simple command
  GIVEN coi binary
  WHEN ./coi run "echo test"
  THEN exit code is 0
  AND output contains "test"
  AND container is ephemeral (auto-deleted)

TC-RUN-002: Run command with workspace
  GIVEN workspace with package.json
  WHEN ./coi run --workspace $WS "npm install"
  THEN exit code is 0
  AND node_modules exists in workspace
  AND file ownership is correct (not root)

TC-RUN-003: Run command with environment variables
  GIVEN coi binary
  WHEN ./coi run -e FOO=bar -e BAZ=qux "env"
  THEN output contains "FOO=bar"
  AND output contains "BAZ=qux"

TC-RUN-004: Run command in privileged mode
  GIVEN coi binary
  WHEN ./coi run --privileged "gh --version"
  THEN exit code is 0
  AND output contains GitHub CLI version

TC-RUN-005: Run command captures exit code
  GIVEN coi binary
  WHEN ./coi run "exit 42"
  THEN coi exit code is 42

TC-RUN-006: Run command with invalid workspace
  GIVEN coi binary
  WHEN ./coi run --workspace /nonexistent "echo test"
  THEN exit code is 1
  AND stderr contains "workspace does not exist"

TC-RUN-007: Run command with output capture
  GIVEN coi binary
  WHEN ./coi run --capture "echo test"
  THEN exit code is 0
  AND output is captured cleanly (no container logs)

TC-RUN-008: Run command with custom slot
  GIVEN coi binary
  WHEN ./coi run --slot 5 "echo test"
  THEN exit code is 0
  AND uses slot 5 container

TC-RUN-009: Run long-running command
  GIVEN coi binary
  WHEN ./coi run "sleep 2 && echo done"
  THEN exit code is 0
  AND waits for completion
  AND output contains "done"

TC-RUN-010: Run command with quotes and special chars
  GIVEN coi binary
  WHEN ./coi run "echo 'hello world' && echo \$PATH"
  THEN exit code is 0
  AND output contains "hello world"
  AND output contains PATH value
```

#### 2.3 Shell Command
```
TC-SHELL-001: Start basic shell session
  GIVEN coi binary
  WHEN ./coi shell --workspace $WS
  THEN container starts
  AND workspace is mounted at /workspace
  AND user is 'claude'
  AND pwd is /workspace

TC-SHELL-002: Shell with custom slot
  GIVEN coi binary
  WHEN ./coi shell --slot 3 --workspace $WS
  THEN container name includes slot 3
  AND session is isolated from other slots

TC-SHELL-003: Shell in privileged mode
  GIVEN coi binary
  WHEN ./coi shell --privileged --workspace $WS
  THEN container uses privileged image
  AND gh command is available
  AND git config is mounted

TC-SHELL-004: Shell with persistent mode
  GIVEN coi binary
  WHEN ./coi shell --persistent --workspace $WS
  THEN container is created as non-ephemeral
  AND container persists after exit

TC-SHELL-005: Shell with environment variables
  GIVEN coi binary
  WHEN ./coi shell -e DEBUG=1 --workspace $WS
  THEN container has DEBUG=1 in environment

TC-SHELL-006: Shell with storage mount
  GIVEN coi binary
  AND storage directory exists
  WHEN ./coi shell --storage /tmp/storage --workspace $WS
  THEN /storage is mounted in container
  AND is writable

TC-SHELL-007: Shell with invalid workspace
  GIVEN coi binary
  WHEN ./coi shell --workspace /invalid
  THEN exit code is 1
  AND stderr contains helpful error message

TC-SHELL-008: Shell with missing image
  GIVEN coi binary
  AND no sandbox image
  WHEN ./coi shell --workspace $WS
  THEN exit code is 1
  AND stderr suggests running 'coi build sandbox'

TC-SHELL-009: Shell with invalid slot
  GIVEN coi binary
  WHEN ./coi shell --slot -1
  THEN exit code is 1
  AND stderr contains "invalid slot number"

TC-SHELL-010: Shell with mount-claude-config
  GIVEN coi binary
  AND ~/.claude exists with API key
  WHEN ./coi shell --mount-claude-config --workspace $WS
  THEN ~/.claude is mounted in container
  AND API key is accessible
```

#### 2.4 List Command
```
TC-LIST-001: List with no containers
  GIVEN coi binary
  AND no active containers
  WHEN ./coi list
  THEN exit code is 0
  AND output indicates no containers

TC-LIST-002: List active containers
  GIVEN coi binary
  AND 3 running containers (slots 1,2,3)
  WHEN ./coi list
  THEN exit code is 0
  AND output shows all 3 containers
  AND shows workspace, slot, state

TC-LIST-003: List shows sessions
  GIVEN coi binary
  AND 2 saved sessions
  WHEN ./coi list
  THEN output shows saved sessions
  AND shows session IDs and timestamps

TC-LIST-004: List with filtering
  GIVEN coi binary
  AND containers for multiple workspaces
  WHEN ./coi list --workspace $WS1
  THEN only shows containers for WS1

TC-LIST-005: List JSON output
  GIVEN coi binary
  AND active containers
  WHEN ./coi list --json
  THEN output is valid JSON
  AND contains container metadata
```

#### 2.5 Info Command
```
TC-INFO-001: Info for saved session
  GIVEN coi binary
  AND saved session with ID abc123
  WHEN ./coi info abc123
  THEN exit code is 0
  AND shows session metadata
  AND shows workspace path
  AND shows session size
  AND shows resume command

TC-INFO-002: Info for nonexistent session
  GIVEN coi binary
  WHEN ./coi info nonexistent-id
  THEN exit code is 1
  AND stderr contains "session not found"

TC-INFO-003: Info with no session ID
  GIVEN coi binary
  AND multiple saved sessions for current workspace
  WHEN ./coi info
  THEN shows most recent session for current workspace

TC-INFO-004: Info shows session contents
  GIVEN coi binary
  AND session with .claude directory
  WHEN ./coi info $SESSION_ID
  THEN lists .claude directory contents
  AND shows .claude.json if exists
```

#### 2.6 Clean Command
```
TC-CLEAN-001: Clean with confirmation
  GIVEN coi binary
  AND stopped containers exist
  WHEN ./coi clean
  THEN prompts for confirmation
  AND waits for user input

TC-CLEAN-002: Clean with force flag
  GIVEN coi binary
  AND 3 stopped containers
  WHEN ./coi clean --force
  THEN exit code is 0
  AND all stopped containers are deleted
  AND no confirmation prompt

TC-CLEAN-003: Clean preserves running containers
  GIVEN coi binary
  AND 2 running containers
  AND 1 stopped container
  WHEN ./coi clean --force
  THEN only stopped container is deleted
  AND running containers remain

TC-CLEAN-004: Clean old sessions
  GIVEN coi binary
  AND sessions older than 30 days
  WHEN ./coi clean --sessions --older-than 30d --force
  THEN old sessions are deleted
  AND recent sessions preserved

TC-CLEAN-005: Clean with dry-run
  GIVEN coi binary
  AND stopped containers
  WHEN ./coi clean --dry-run
  THEN shows what would be deleted
  AND doesn't actually delete anything

TC-CLEAN-006: Clean with no stopped containers
  GIVEN coi binary
  AND no stopped containers
  WHEN ./coi clean --force
  THEN exit code is 0
  AND output indicates nothing to clean
```

#### 2.7 Tmux Command
```
TC-TMUX-001: Send command to tmux session
  GIVEN coi binary
  AND container with tmux session
  WHEN ./coi tmux send --container $NAME "echo test"
  THEN command is sent to tmux
  AND executed in container

TC-TMUX-002: Capture tmux output
  GIVEN coi binary
  AND container with tmux session
  AND recent output in tmux
  WHEN ./coi tmux capture --container $NAME
  THEN output is captured and returned

TC-TMUX-003: List tmux sessions
  GIVEN coi binary
  AND container with multiple tmux sessions
  WHEN ./coi tmux list --container $NAME
  THEN shows all tmux sessions

TC-TMUX-004: Tmux with nonexistent container
  GIVEN coi binary
  WHEN ./coi tmux send --container invalid "echo test"
  THEN exit code is 1
  AND stderr contains "container not found"
```

### 3. Workflow E2E Tests

**Goal**: Test multi-command workflows that users actually perform

#### 3.1 Development Workflows
```
TC-WORKFLOW-001: Initial setup and first run
  GIVEN fresh installation
  WHEN user runs:
    ./coi build sandbox
    ./coi run "echo hello"
  THEN builds complete successfully
  AND run executes correctly
  AND cleanup happens automatically

TC-WORKFLOW-002: Persistent development session
  GIVEN coi binary
  WHEN user runs:
    ./coi shell --persistent --workspace $WS
    # Install tools in session
    # Exit session
    ./coi shell --persistent --workspace $WS
  THEN second session reuses same container
  AND installed tools are still present
  AND no reinstallation needed

TC-WORKFLOW-003: Install tools once, use forever
  GIVEN coi binary
  WHEN user runs:
    ./coi shell --persistent --workspace $WS
    # sudo apt-get install -y jq ripgrep
    # exit
    ./coi shell --persistent --workspace $WS
    # which jq
  THEN jq is available in second session
  AND no reinstall was needed
  AND startup is fast

TC-WORKFLOW-004: Multi-project development
  GIVEN coi binary
  AND 3 different workspaces
  WHEN user runs:
    ./coi shell --workspace /project1 &
    ./coi shell --workspace /project2 &
    ./coi shell --workspace /project3 &
  THEN 3 separate containers are created
  AND each has correct workspace mounted
  AND sessions don't interfere

TC-WORKFLOW-005: Parallel slots for same project
  GIVEN coi binary
  AND single workspace
  WHEN user runs:
    ./coi shell --slot 1 --workspace $WS &
    ./coi shell --slot 2 --workspace $WS &
    ./coi shell --slot 3 --workspace $WS &
  THEN 3 containers are created
  AND all share same workspace
  AND can work in parallel

TC-WORKFLOW-006: Quick command execution
  GIVEN coi binary
  AND workspace with package.json
  WHEN user runs:
    ./coi run "npm install"
    ./coi run "npm test"
    ./coi run "npm run build"
  THEN each command executes in fresh container
  AND results persist in workspace
  AND containers are cleaned up

TC-WORKFLOW-007: Session resume workflow
  GIVEN coi binary
  WHEN user runs:
    ./coi shell --workspace $WS
    # Do work, create .claude config
    # Exit
    ./coi list
    ./coi info $SESSION_ID
    ./coi shell --resume $SESSION_ID
  THEN session is restored
  AND .claude directory is restored
  AND session state is preserved

TC-WORKFLOW-008: Cleanup workflow
  GIVEN coi binary
  AND multiple stopped containers
  WHEN user runs:
    ./coi list
    ./coi clean --force
    ./coi list
  THEN stopped containers are removed
  AND list shows empty state

TC-WORKFLOW-009: Upgrade workflow
  GIVEN old version of coi
  AND active sessions
  WHEN user upgrades:
    git pull
    make build
    ./coi list
  THEN existing sessions still work
  AND can resume old sessions
  AND new features available
```

#### 3.2 Configuration Workflows
```
TC-WORKFLOW-010: Configuration hierarchy
  GIVEN coi binary
  WHEN user creates:
    ~/.config/claude-on-incus/config.toml (persistent=false)
    ./.claude-on-incus.toml (persistent=true)
  AND runs: ./coi shell
  THEN project config takes precedence
  AND container is persistent

TC-WORKFLOW-011: Environment variable override
  GIVEN coi binary
  AND config file with persistent=false
  WHEN CLAUDE_ON_INCUS_PERSISTENT=true ./coi shell
  THEN environment variable overrides config
  AND container is persistent

TC-WORKFLOW-012: CLI flag override
  GIVEN coi binary
  AND config file with persistent=true
  AND CLAUDE_ON_INCUS_PERSISTENT=true
  WHEN ./coi shell --persistent=false
  THEN CLI flag takes highest precedence
  AND container is ephemeral

TC-WORKFLOW-013: Profile usage
  GIVEN coi binary
  AND config with [profiles.rust]
  WHEN ./coi shell --profile rust
  THEN rust profile settings applied
  AND custom image used if specified
  AND environment variables set
```

#### 3.3 Privileged Mode Workflows
```
TC-WORKFLOW-014: GitHub integration
  GIVEN coi binary
  AND ~/.gitconfig exists
  WHEN ./coi shell --privileged --workspace $WS
  THEN git config is mounted
  AND gh CLI is available
  AND git commands work

TC-WORKFLOW-015: SSH key usage
  GIVEN coi binary
  AND ~/.ssh/id_rsa exists
  WHEN ./coi shell --privileged --ssh-key ~/.ssh/id_rsa
  THEN SSH key is mounted
  AND git clone via SSH works

TC-WORKFLOW-016: Docker-in-container
  GIVEN coi binary
  WHEN ./coi shell --privileged --workspace $WS
  AND docker ps in container
  THEN Docker daemon is accessible
  AND can run containers

TC-WORKFLOW-017: Privileged with persistence
  GIVEN coi binary
  WHEN ./coi shell --privileged --persistent
  THEN both modes work together
  AND container persists
  AND has privileged access
```

#### 3.4 Multi-Shell Workflows (Same Workspace)
```
TC-WORKFLOW-018: Multiple shells for same workspace (different slots)
  GIVEN coi binary
  AND workspace /proj
  WHEN user opens:
    Terminal 1: ./coi shell --slot 1 --workspace /proj
    Terminal 2: ./coi shell --slot 2 --workspace /proj
    Terminal 3: ./coi shell --slot 3 --workspace /proj
  THEN 3 separate containers created
  AND each has unique name (claude-<hash>-1, -2, -3)
  AND all mount same workspace at /workspace
  AND changes in workspace visible across all slots
  AND container states are independent

TC-WORKFLOW-019: Auto-slot allocation for same workspace
  GIVEN coi binary
  AND workspace /proj
  WHEN user opens 5 terminals simultaneously:
    ./coi shell --workspace /proj (no --slot specified)
  THEN slots 1-5 auto-allocated
  AND each gets unique container
  AND all work in parallel

TC-WORKFLOW-020: Mixed persistent and ephemeral for same workspace
  GIVEN coi binary
  WHEN user runs:
    ./coi shell --slot 1 --persistent --workspace /proj
    ./coi shell --slot 2 --workspace /proj (ephemeral)
  THEN slot 1 container persists after exit
  AND slot 2 container deleted after exit
  AND both work independently

TC-WORKFLOW-021: Share workspace files, isolate container state
  GIVEN 2 shells for same workspace (slots 1 and 2)
  WHEN slot 1 creates /workspace/test.txt
  AND slot 2 installs package 'jq'
  THEN test.txt appears in slot 2's /workspace
  AND jq is NOT available in slot 1
  AND file changes are shared
  AND container packages are isolated

TC-WORKFLOW-022: Concurrent builds in same workspace
  GIVEN coi binary
  AND Rust workspace
  WHEN slot 1 runs: cargo build --release
  AND slot 2 runs: cargo build --debug
  THEN both builds complete without conflict
  AND target/ directory has both outputs
  AND no race conditions on workspace files

TC-WORKFLOW-023: Development + Testing in parallel
  GIVEN coi binary
  AND workspace with test suite
  WHEN slot 1 runs interactive development session
  AND slot 2 runs: ./coi run --slot 2 "npm run test:watch"
  THEN can edit in slot 1
  AND tests re-run automatically in slot 2
  AND both see workspace changes
```

#### 3.5 Resume and Continue Workflows
```
TC-WORKFLOW-024: Resume last session for workspace
  GIVEN saved session for workspace /proj
  WHEN ./coi shell --resume --workspace /proj
  THEN most recent session for /proj is resumed
  AND .claude state restored
  AND same slot reused

TC-WORKFLOW-025: Resume specific session by ID
  GIVEN 3 saved sessions for workspace
  WHEN ./coi shell --resume abc123
  THEN session abc123 is restored
  AND correct .claude state loaded
  AND original workspace mounted

TC-WORKFLOW-026: Resume with session continuation
  GIVEN saved session with API key in .claude
  WHEN ./coi shell --resume $SESSION_ID
  THEN API key is available
  AND previous conversation context accessible
  AND can continue work seamlessly

TC-WORKFLOW-027: Resume session, modify, save new session
  GIVEN saved session abc123
  WHEN ./coi shell --resume abc123
  AND modify .claude config
  AND exit
  THEN new session ID created
  AND both sessions preserved
  AND can resume either session later

TC-WORKFLOW-028: Resume after workspace moved
  GIVEN saved session for /old/path
  AND workspace moved to /new/path
  WHEN ./coi shell --resume $SESSION_ID --workspace /new/path
  THEN session .claude state restored
  AND works with new workspace path
  AND updates session metadata

TC-WORKFLOW-029: Resume with override flags
  GIVEN saved session (sandbox, slot 1, ephemeral)
  WHEN ./coi shell --resume $SESSION_ID --privileged --persistent
  THEN .claude state restored
  BUT uses privileged image
  AND makes container persistent
  AND warns about mode changes

TC-WORKFLOW-030: Auto-resume on repeat command
  GIVEN recent session for current workspace
  WHEN ./coi shell (no explicit --resume)
  AND prompt asks "Resume last session?"
  AND user confirms
  THEN last session is resumed

TC-WORKFLOW-031: Resume failed session
  GIVEN session that exited with error
  WHEN ./coi shell --resume $SESSION_ID
  THEN session is restored
  AND can investigate error
  AND session state intact despite failure
```

#### 3.6 Tmux Integration Workflows
```
TC-WORKFLOW-032: Attach to existing tmux session
  GIVEN container with active tmux session
  WHEN ./coi tmux attach --container $NAME
  THEN attaches to tmux session
  AND can interact with running processes
  AND detach preserves session

TC-WORKFLOW-033: Send commands via tmux
  GIVEN container with tmux session
  WHEN ./coi tmux send --container $NAME "npm test"
  THEN command executes in tmux
  AND can capture output later
  AND tmux session continues

TC-WORKFLOW-034: Monitor long-running process via tmux
  GIVEN container running "npm run watch" in tmux
  WHEN ./coi tmux capture --container $NAME
  THEN shows recent output from watch process
  AND can see real-time updates
  AND process continues running

TC-WORKFLOW-035: Multiple tmux sessions in container
  GIVEN container
  WHEN create tmux sessions: "dev", "test", "logs"
  AND ./coi tmux list --container $NAME
  THEN shows all 3 sessions
  AND can attach to any session by name

TC-WORKFLOW-036: Tmux session persistence
  GIVEN persistent container with tmux sessions
  WHEN exit shell
  AND ./coi shell --persistent (restart)
  AND ./coi tmux list --container $NAME
  THEN tmux sessions still exist
  AND running processes preserved

TC-WORKFLOW-037: Automated workflows via tmux
  GIVEN container with Claude running in tmux
  WHEN ./coi tmux send --container $NAME "/help"
  AND ./coi tmux capture --container $NAME
  THEN captures Claude's response
  AND can script Claude interactions
  AND tmux provides session isolation

TC-WORKFLOW-038: Detached session workflow
  GIVEN coi binary
  WHEN ./coi shell --persistent --workspace $WS
  AND start long process in tmux
  AND detach from tmux (Ctrl+B D)
  AND exit shell
  THEN container keeps running
  AND tmux session preserved
  AND can reattach later

TC-WORKFLOW-039: Background task monitoring
  GIVEN 3 containers with different builds running in tmux
  WHEN ./coi tmux capture --container claude-proj1
  AND ./coi tmux capture --container claude-proj2
  AND ./coi tmux capture --container claude-proj3
  THEN shows status of all builds
  AND can monitor progress without entering shells
```

#### 3.7 Storage and Mounting Workflows
```
TC-WORKFLOW-040: Persistent storage across workspaces
  GIVEN coi binary
  AND persistent storage directory /data/claude-storage
  WHEN ./coi shell --storage /data/claude-storage --workspace /proj1
  AND ./coi shell --storage /data/claude-storage --workspace /proj2
  THEN both containers mount same /storage
  AND can share data between workspaces
  AND storage persists independently

TC-WORKFLOW-041: Mount Claude config from host
  GIVEN ~/.claude with API key
  WHEN ./coi shell --mount-claude-config --workspace $WS
  THEN ~/.claude mounted in container
  AND API key works in container
  AND changes in container reflect on host
  AND changes on host visible in container

TC-WORKFLOW-042: Multiple mount points
  GIVEN coi binary
  WHEN ./coi shell \
    --workspace /proj \
    --storage /data \
    --mount-claude-config \
    --ssh-key ~/.ssh/id_rsa
  THEN all mounts work simultaneously:
    /workspace -> /proj
    /storage -> /data
    ~/.claude -> ~/.claude
    ~/.ssh/id_rsa -> ~/.ssh/id_rsa

TC-WORKFLOW-043: Git config integration
  GIVEN ~/.gitconfig with user details
  AND privileged mode
  WHEN ./coi shell --privileged --workspace $WS
  THEN git config mounted
  AND git commits use correct author
  AND git push works with credentials

TC-WORKFLOW-044: SSH agent forwarding
  GIVEN SSH agent running on host
  AND privileged mode with SSH key
  WHEN ./coi shell --privileged --ssh-key ~/.ssh/id_rsa
  THEN can use SSH key in container
  AND git clone via SSH works
  AND SSH agent forwarding works
```

#### 3.8 Image and Container Management Workflows
```
TC-WORKFLOW-045: Switch between sandbox and privileged
  GIVEN coi binary
  WHEN ./coi shell --workspace $WS (uses sandbox)
  AND later: ./coi shell --privileged --workspace $WS
  THEN creates separate container
  AND can switch between them
  AND both work independently

TC-WORKFLOW-046: Custom image workflow
  GIVEN custom image built on top of sandbox
  AND config specifies custom image
  WHEN ./coi shell --workspace $WS
  THEN uses custom image
  AND custom tools available

TC-WORKFLOW-047: Image rebuild with active containers
  GIVEN active containers using sandbox image
  WHEN ./coi build sandbox --force
  THEN new image built
  AND active containers continue with old image
  AND new containers use new image

TC-WORKFLOW-048: Container name collision handling
  GIVEN container "claude-abc123-1" exists
  WHEN try to create another "claude-abc123-1"
  THEN detects collision
  AND offers to reuse existing or pick new slot
  AND prevents conflicts

TC-WORKFLOW-049: Incremental image updates
  GIVEN existing sandbox image
  WHEN ./coi build sandbox (no --force)
  THEN checks for changes
  AND only rebuilds if needed
  AND saves time on unnecessary rebuilds
```

#### 3.9 Advanced Development Workflows
```
TC-WORKFLOW-050: Microservices development
  GIVEN 3 microservices in separate workspaces
  WHEN ./coi shell --slot 1 --workspace /service1
  AND ./coi shell --slot 2 --workspace /service2
  AND ./coi shell --slot 3 --workspace /service3
  THEN all services run in parallel
  AND each has isolated environment
  AND can test inter-service communication

TC-WORKFLOW-051: Frontend + Backend development
  GIVEN workspace with frontend/ and backend/
  WHEN ./coi shell --slot 1 --workspace $WS (for backend)
  AND ./coi shell --slot 2 --workspace $WS (for frontend)
  AND slot 1 runs: cd backend && npm run dev
  AND slot 2 runs: cd frontend && npm run dev
  THEN both dev servers run
  AND hot reload works
  AND can develop both simultaneously

TC-WORKFLOW-052: CI/CD simulation locally
  GIVEN coi binary
  AND .github/workflows/test.yml
  WHEN ./coi run "act" (GitHub Actions locally)
  THEN workflows run in container
  AND can test CI before pushing
  AND results match GitHub Actions

TC-WORKFLOW-053: Database development workflow
  GIVEN persistent container
  WHEN ./coi shell --persistent --workspace $WS
  AND install PostgreSQL in container
  AND create database
  AND exit
  THEN database persists between sessions
  AND data is not lost
  AND can continue development

TC-WORKFLOW-054: Dotfiles synchronization
  GIVEN custom .bashrc and .vimrc on host
  AND config to mount dotfiles
  WHEN ./coi shell --workspace $WS
  THEN dotfiles available in container
  AND custom environment works
  AND consistent across all containers

TC-WORKFLOW-055: Environment variable management
  GIVEN .env file in workspace
  WHEN ./coi shell -e $(cat .env) --workspace $WS
  THEN all env vars loaded
  AND application configuration works
  AND secrets available in container

TC-WORKFLOW-056: Multi-architecture testing
  GIVEN coi binary
  WHEN ./coi shell --arch arm64 --workspace $WS
  THEN container runs on arm64
  AND can test cross-platform compatibility
  AND build artifacts for different architectures
```

#### 3.10 Cleanup and Maintenance Workflows
```
TC-WORKFLOW-057: Periodic cleanup routine
  GIVEN many old sessions and stopped containers
  WHEN ./coi clean --sessions --older-than 30d --force
  AND ./coi clean --containers --force
  THEN old sessions removed
  AND stopped containers removed
  AND disk space reclaimed
  AND active sessions preserved

TC-WORKFLOW-058: Workspace migration
  GIVEN containers for /old/workspace
  WHEN move workspace to /new/workspace
  AND ./coi list
  THEN update workspace paths
  AND containers can find new location
  AND sessions work with new path

TC-WORKFLOW-059: Disaster recovery
  GIVEN container manually deleted
  AND saved session exists
  WHEN ./coi shell --resume $SESSION_ID
  THEN recreates container
  AND restores .claude state
  AND can continue work

TC-WORKFLOW-060: Batch operations
  GIVEN 10 workspaces
  WHEN for ws in /proj*; do ./coi run --workspace $ws "npm test"; done
  THEN tests run for all workspaces
  AND results collected
  AND containers cleaned up automatically
```

### 4. Session Management E2E Tests

**Goal**: Validate session persistence and resumption

#### 4.1 Session Creation
```
TC-SESSION-001: Session ID generation
  GIVEN coi binary
  WHEN ./coi shell --workspace $WS
  THEN unique session ID is generated
  AND session is saved to ~/.claude-on-incus/sessions/$ID

TC-SESSION-002: Session metadata saved
  GIVEN coi binary
  WHEN session completes
  THEN metadata.json includes:
    - session_id
    - workspace_path
    - timestamp
    - slot
    - privileged flag
    - persistent flag

TC-SESSION-003: .claude directory saved
  GIVEN coi binary
  AND session creates ~/.claude/config.json
  WHEN session exits
  THEN .claude directory is saved to session storage
  AND all files are preserved

TC-SESSION-004: Multiple sessions for same workspace
  GIVEN coi binary
  WHEN user creates 3 sessions for same workspace
  THEN 3 separate session IDs created
  AND each has isolated .claude state
```

#### 4.2 Session Restoration
```
TC-SESSION-005: Resume by session ID
  GIVEN saved session with ID abc123
  WHEN ./coi shell --resume abc123
  THEN session is restored
  AND .claude directory is restored
  AND container reuses same slot

TC-SESSION-006: Resume latest for workspace
  GIVEN multiple sessions for workspace
  WHEN ./coi shell --resume --workspace $WS
  THEN most recent session is restored

TC-SESSION-007: Resume with missing session
  GIVEN coi binary
  WHEN ./coi shell --resume invalid-id
  THEN exit code is 1
  AND stderr contains "session not found"
  AND suggests using 'coi list' to see sessions

TC-SESSION-008: Resume preserves environment
  GIVEN session with custom env vars
  WHEN ./coi shell --resume $SESSION_ID
  THEN environment variables are restored

TC-SESSION-009: Resume after container deleted
  GIVEN saved session
  AND container was manually deleted
  WHEN ./coi shell --resume $SESSION_ID
  THEN new container is created
  AND .claude state is restored
  AND warning about recreating container
```

#### 4.3 Session Cleanup
```
TC-SESSION-010: Automatic session save on exit
  GIVEN active session
  WHEN user exits normally
  THEN session is automatically saved
  AND .claude directory is backed up

TC-SESSION-011: Session save on Ctrl+C
  GIVEN active session
  WHEN user presses Ctrl+C
  THEN graceful cleanup runs
  AND session is saved before exit

TC-SESSION-012: Session pruning
  GIVEN 50 old sessions
  WHEN ./coi clean --sessions --older-than 30d
  THEN old sessions are removed
  AND disk space is freed

TC-SESSION-013: Session list ordering
  GIVEN multiple sessions
  WHEN ./coi list
  THEN sessions shown newest first
  AND includes last access time
```

### 5. Persistence E2E Tests

**Goal**: Validate persistent container mode

#### 5.1 Container Lifecycle
```
TC-PERSIST-001: Persistent container creation
  GIVEN coi binary
  WHEN ./coi shell --persistent --workspace $WS
  THEN container created as non-ephemeral
  AND container name follows convention
  AND container starts

TC-PERSIST-002: Persistent container reuse
  GIVEN existing persistent container
  WHEN ./coi shell --persistent --workspace $WS
  THEN existing container is restarted (not recreated)
  AND container state is preserved

TC-PERSIST-003: Persistent container stops on exit
  GIVEN active persistent container
  WHEN session exits
  THEN container is stopped
  AND container is NOT deleted
  AND can be restarted later

TC-PERSIST-004: Ephemeral container cleanup
  GIVEN active ephemeral container
  WHEN session exits
  THEN container is deleted
  AND no residual state
```

#### 5.2 State Persistence
```
TC-PERSIST-005: Installed packages persist
  GIVEN persistent container
  WHEN session 1:
    apt-get install -y jq
    exit
  AND session 2:
    which jq
  THEN jq is available without reinstall

TC-PERSIST-006: Build artifacts persist
  GIVEN persistent container
  AND workspace with Rust project
  WHEN session 1:
    cargo build
    exit
  AND session 2:
    ls target/debug/
  THEN build artifacts exist
  AND no rebuild needed

TC-PERSIST-007: npm packages persist
  GIVEN persistent container
  AND workspace with package.json
  WHEN session 1:
    npm install
    exit
  AND session 2:
    ls node_modules/
  THEN node_modules preserved
  AND no reinstall

TC-PERSIST-008: User modifications persist
  GIVEN persistent container
  WHEN session 1:
    echo 'alias ll="ls -la"' >> ~/.bashrc
    exit
  AND session 2:
    source ~/.bashrc && ll
  THEN alias works

TC-PERSIST-009: Filesystem changes persist
  GIVEN persistent container
  WHEN session 1:
    touch /tmp/testfile
    exit
  AND session 2:
    ls /tmp/testfile
  THEN file still exists
```

#### 5.3 Persistence Modes
```
TC-PERSIST-010: Persistent via config file
  GIVEN config.toml with persistent=true
  WHEN ./coi shell
  THEN container is persistent (no flag needed)

TC-PERSIST-011: Persistent via env var
  GIVEN CLAUDE_ON_INCUS_PERSISTENT=true
  WHEN ./coi shell
  THEN container is persistent

TC-PERSIST-012: Override persistent mode
  GIVEN config with persistent=true
  WHEN ./coi shell --persistent=false
  THEN container is ephemeral (flag overrides)

TC-PERSIST-013: Persistent cleanup
  GIVEN persistent containers
  WHEN ./coi clean --force
  THEN persistent containers are stopped but not deleted
```

### 6. Configuration E2E Tests

**Goal**: Validate configuration system

#### 6.1 Config File Loading
```
TC-CONFIG-001: Default config
  GIVEN no config file
  WHEN ./coi shell
  THEN uses built-in defaults
  AND image = coi-sandbox
  AND persistent = false

TC-CONFIG-002: User config loading
  GIVEN ~/.config/claude-on-incus/config.toml
  WHEN ./coi shell
  THEN user config is loaded
  AND defaults are overridden

TC-CONFIG-003: Project config loading
  GIVEN ./.claude-on-incus.toml in workspace
  WHEN ./coi shell
  THEN project config is loaded
  AND overrides user config

TC-CONFIG-004: System config loading
  GIVEN /etc/claude-on-incus/config.toml
  WHEN ./coi shell
  THEN system config is loaded
  AND user config overrides it

TC-CONFIG-005: Invalid config file
  GIVEN invalid TOML in config file
  WHEN ./coi shell
  THEN exit code is 1
  AND stderr shows parse error
  AND indicates file location
```

#### 6.2 Configuration Hierarchy
```
TC-CONFIG-006: Config precedence order
  GIVEN all config levels defined
  WHEN ./coi shell --privileged
  THEN precedence is:
    1. CLI flags (highest)
    2. Environment variables
    3. Project config
    4. User config
    5. System config
    6. Built-in defaults (lowest)

TC-CONFIG-007: Partial config override
  GIVEN user config sets image=custom
  AND project config sets persistent=true
  WHEN ./coi shell
  THEN both settings apply
  AND other defaults remain

TC-CONFIG-008: Environment variable format
  GIVEN CLAUDE_ON_INCUS_IMAGE=custom
  AND CLAUDE_ON_INCUS_PERSISTENT=true
  WHEN ./coi shell
  THEN environment variables applied
  AND override config files
```

#### 6.3 Profile Configuration
```
TC-CONFIG-009: Named profile
  GIVEN config with [profiles.rust]
  WHEN ./coi shell --profile rust
  THEN rust profile settings applied

TC-CONFIG-010: Profile inheritance
  GIVEN profile inherits from defaults
  WHEN ./coi shell --profile rust
  THEN profile settings override defaults
  AND unset profile values use defaults

TC-CONFIG-011: Invalid profile
  GIVEN config with no 'python' profile
  WHEN ./coi shell --profile python
  THEN exit code is 1
  AND stderr contains "profile not found"
  AND lists available profiles

TC-CONFIG-012: Profile with environment
  GIVEN profile with environment vars
  WHEN ./coi shell --profile rust
  THEN profile environment vars are set
  AND merge with --env flags
```

### 7. Error Handling E2E Tests

**Goal**: Validate graceful error handling

#### 7.1 Missing Dependencies
```
TC-ERROR-001: Incus not installed
  GIVEN coi binary
  AND Incus not in PATH
  WHEN ./coi shell
  THEN exit code is 1
  AND stderr contains helpful message
  AND suggests installing Incus
  AND provides installation link

TC-ERROR-002: Incus not running
  GIVEN coi binary
  AND Incus installed but not running
  WHEN ./coi shell
  THEN exit code is 1
  AND stderr suggests starting Incus daemon

TC-ERROR-003: Permission denied
  GIVEN coi binary
  AND user not in incus-admin group
  WHEN ./coi shell
  THEN exit code is 1
  AND stderr explains permission issue
  AND suggests adding user to group

TC-ERROR-004: Missing image
  GIVEN coi binary
  AND no sandbox image
  WHEN ./coi shell
  THEN exit code is 1
  AND stderr suggests 'coi build sandbox'
```

#### 7.2 Invalid Arguments
```
TC-ERROR-005: Invalid workspace path
  GIVEN coi binary
  WHEN ./coi shell --workspace /nonexistent
  THEN exit code is 1
  AND stderr contains "workspace does not exist"

TC-ERROR-006: Workspace is file not directory
  GIVEN coi binary
  AND /tmp/file.txt exists (not directory)
  WHEN ./coi shell --workspace /tmp/file.txt
  THEN exit code is 1
  AND stderr contains "workspace must be a directory"

TC-ERROR-007: Invalid slot number
  GIVEN coi binary
  WHEN ./coi shell --slot -1
  THEN exit code is 1
  AND stderr contains "invalid slot number"

TC-ERROR-008: Invalid environment variable format
  GIVEN coi binary
  WHEN ./coi shell -e INVALID
  THEN exit code is 1
  AND stderr contains "invalid format, use KEY=VALUE"

TC-ERROR-009: Conflicting flags
  GIVEN coi binary
  WHEN ./coi shell --resume $ID --slot 5
  THEN exit code is 1
  AND stderr explains conflict
  AND suggests using resume without slot

TC-ERROR-010: Missing required argument
  GIVEN coi binary
  WHEN ./coi tmux send
  THEN exit code is 1
  AND shows usage help
```

#### 7.3 Runtime Errors
```
TC-ERROR-011: Container fails to start
  GIVEN coi binary
  AND corrupted image
  WHEN ./coi shell
  THEN exit code is 1
  AND stderr contains container error
  AND suggests rebuilding image

TC-ERROR-012: Container killed during session
  GIVEN active session
  WHEN container is killed externally
  THEN session detects disconnect
  AND shows helpful error message
  AND saves session state before exit

TC-ERROR-013: Disk space full
  GIVEN coi binary
  AND no disk space available
  WHEN ./coi shell
  THEN exit code is 1
  AND error message indicates disk space issue

TC-ERROR-014: Network issues
  GIVEN coi binary
  AND network connectivity issues
  WHEN ./coi build sandbox
  THEN build fails with clear error
  AND indicates network problem

TC-ERROR-015: Workspace mount failure
  GIVEN coi binary
  AND workspace on unmounted drive
  WHEN ./coi shell --workspace /mnt/unmounted
  THEN exit code is 1
  AND error explains mount issue
```

#### 7.4 Recovery Scenarios
```
TC-ERROR-016: Corrupted session data
  GIVEN saved session with corrupted metadata
  WHEN ./coi shell --resume $SESSION_ID
  THEN shows warning about corruption
  AND asks if user wants to start fresh
  AND preserves original session data

TC-ERROR-017: Multiple containers for same slot
  GIVEN duplicate containers (manual creation)
  WHEN ./coi shell
  THEN detects conflict
  AND suggests cleanup
  AND doesn't start session

TC-ERROR-018: Stale session state
  GIVEN session saved 6 months ago
  AND container no longer exists
  WHEN ./coi shell --resume $SESSION_ID
  THEN recreates container
  AND restores .claude state
  AND shows warning about staleness
```

### 8. Parallel Operations E2E Tests

**Goal**: Validate concurrent operations

#### 8.1 Multi-Slot Parallelism
```
TC-PARALLEL-001: Start 5 parallel slots
  GIVEN coi binary
  WHEN simultaneously start:
    ./coi shell --slot 1 &
    ./coi shell --slot 2 &
    ./coi shell --slot 3 &
    ./coi shell --slot 4 &
    ./coi shell --slot 5 &
  THEN all 5 containers start successfully
  AND each has unique name
  AND no race conditions

TC-PARALLEL-002: Parallel slots share workspace
  GIVEN 3 parallel slots for workspace
  WHEN slot 1 creates file
  THEN file appears in slot 2 and 3
  AND no file conflicts

TC-PARALLEL-003: Parallel slots isolated state
  GIVEN 3 parallel slots
  WHEN slot 1 installs package
  THEN package only in slot 1
  AND not in slot 2 or 3

TC-PARALLEL-004: Parallel run commands
  GIVEN coi binary
  WHEN simultaneously:
    ./coi run --slot 1 "npm test" &
    ./coi run --slot 2 "npm build" &
    ./coi run --slot 3 "npm lint" &
  THEN all complete without interference
  AND results are correct
```

#### 8.2 Multi-Workspace Parallelism
```
TC-PARALLEL-005: Multiple workspaces
  GIVEN coi binary
  AND 3 different workspaces
  WHEN simultaneously:
    ./coi shell --workspace /proj1 &
    ./coi shell --workspace /proj2 &
    ./coi shell --workspace /proj3 &
  THEN 3 containers created
  AND each mounts correct workspace
  AND no cross-contamination

TC-PARALLEL-006: Same workspace, auto slots
  GIVEN coi binary
  WHEN simultaneously start 5 sessions for same workspace
  THEN auto-allocates slots 1-5
  AND all sessions work correctly
```

#### 8.3 Concurrent Cleanup
```
TC-PARALLEL-007: Cleanup during active sessions
  GIVEN 3 active sessions
  AND 2 stopped containers
  WHEN ./coi clean --force
  THEN only stopped containers deleted
  AND active sessions unaffected

TC-PARALLEL-008: Multiple cleanup commands
  GIVEN stopped containers
  WHEN simultaneously run:
    ./coi clean --force &
    ./coi clean --force &
  THEN both complete without error
  AND containers cleaned up exactly once
```

### 9. Performance E2E Tests

**Goal**: Validate performance characteristics

#### 9.1 Startup Performance
```
TC-PERF-001: Cold start time
  GIVEN coi binary
  AND no existing container
  WHEN ./coi shell --workspace $WS
  THEN startup completes in <5 seconds
  AND container is ready

TC-PERF-002: Warm start time (persistent)
  GIVEN existing persistent container
  WHEN ./coi shell --persistent --workspace $WS
  THEN startup completes in <2 seconds
  AND container restart is fast

TC-PERF-003: Resume time
  GIVEN saved session with 100MB .claude directory
  WHEN ./coi shell --resume $SESSION_ID
  THEN resume completes in <10 seconds
  AND .claude data restored
```

#### 9.2 Scalability
```
TC-PERF-004: Many parallel slots
  GIVEN coi binary
  WHEN start 20 parallel slots
  THEN all start successfully
  AND system remains responsive

TC-PERF-005: Large workspace
  GIVEN workspace with 10GB of data
  WHEN ./coi shell --workspace $WS
  THEN workspace mounts successfully
  AND performance acceptable

TC-PERF-006: Many sessions
  GIVEN 100 saved sessions
  WHEN ./coi list
  THEN list completes in <2 seconds
  AND output is readable
```

#### 9.3 Resource Usage
```
TC-PERF-007: Memory usage
  GIVEN coi binary
  WHEN run session
  THEN coi process uses <50MB RAM
  AND container overhead is reasonable

TC-PERF-008: Disk usage
  GIVEN persistent container
  WHEN install typical development tools
  THEN container size reasonable (<2GB)
  AND image sizes reasonable

TC-PERF-009: Cleanup efficiency
  GIVEN 50 stopped containers
  WHEN ./coi clean --force
  THEN cleanup completes in <30 seconds
  AND disk space reclaimed
```

### 10. Acceptance Tests (README Examples)

**Goal**: Verify README examples work exactly as documented

#### 10.1 Quick Start Examples
```
TC-ACCEPT-001: Quick Start - Basic
  GIVEN fresh installation
  WHEN follow Quick Start:
    make build
    ./coi build sandbox
    ./coi run "echo hello"
    ./coi shell
  THEN all commands work as documented

TC-ACCEPT-002: Quick Start - Parallel sessions
  GIVEN coi binary
  WHEN follow example:
    ./coi shell --slot 1
    ./coi shell --slot 2
  THEN both sessions work independently
```

#### 10.2 Usage Examples
```
TC-ACCEPT-003: Basic Commands - Run
  GIVEN coi binary
  WHEN ./coi run "npm test"
  THEN executes as documented

TC-ACCEPT-004: Basic Commands - Persistent
  GIVEN coi binary
  WHEN ./coi shell --persistent
  THEN container persists as documented

TC-ACCEPT-005: Basic Commands - Resume
  GIVEN saved session
  WHEN ./coi shell --resume
  THEN resumes as documented
```

#### 10.3 Persistent Mode Examples
```
TC-ACCEPT-006: Persistent Mode Workflow
  GIVEN coi binary
  WHEN follow README example:
    # First session - install tools
    ./coi shell --persistent
    sudo apt-get install -y jq ripgrep fd-find
    npm install
    exit
    # Second session - tools already there
    ./coi shell --persistent
    which jq
  THEN jq exists without reinstall

TC-ACCEPT-007: Persistent via config
  GIVEN config with persistent=true
  WHEN ./coi shell
  THEN works as documented (no flag needed)
```

#### 10.4 Configuration Examples
```
TC-ACCEPT-008: Config file example
  GIVEN ~/.config/claude-on-incus/config.toml from README
  WHEN ./coi shell
  THEN config applied as documented

TC-ACCEPT-009: Profile example
  GIVEN config with [profiles.rust]
  WHEN ./coi shell --profile rust
  THEN rust profile works as documented
```

### 11. Interactive Session E2E Tests

**Goal**: Validate interactive shell behavior

#### 11.1 Command Execution
```
TC-INTERACTIVE-001: Execute commands
  GIVEN interactive session
  WHEN send command "pwd"
  THEN output shows /workspace

TC-INTERACTIVE-002: Multi-line commands
  GIVEN interactive session
  WHEN send multi-line script
  THEN executes correctly

TC-INTERACTIVE-003: Command history
  GIVEN interactive session
  WHEN send multiple commands
  THEN command history works
  AND can use arrow keys

TC-INTERACTIVE-004: Tab completion
  GIVEN interactive session
  WHEN press tab
  THEN completion works
```

#### 11.2 File Operations
```
TC-INTERACTIVE-005: Create file in workspace
  GIVEN interactive session
  WHEN create file in /workspace
  THEN file appears on host
  AND has correct ownership

TC-INTERACTIVE-006: Edit files
  GIVEN interactive session
  WHEN edit file with vim
  THEN changes persist
  AND visible on host

TC-INTERACTIVE-007: Workspace synchronization
  GIVEN interactive session
  WHEN host creates file in workspace
  THEN file appears in container immediately
```

#### 11.3 Environment
```
TC-INTERACTIVE-008: Environment variables
  GIVEN session with -e DEBUG=1
  WHEN echo $DEBUG
  THEN outputs "1"

TC-INTERACTIVE-009: User environment
  GIVEN interactive session
  WHEN whoami
  THEN outputs "claude"

TC-INTERACTIVE-010: Working directory
  GIVEN interactive session
  WHEN pwd
  THEN outputs "/workspace"
```

### 12. Edge Cases & Stress Tests

**Goal**: Validate behavior in unusual scenarios

#### 12.1 Edge Cases
```
TC-EDGE-001: Empty workspace
  GIVEN empty workspace directory
  WHEN ./coi shell --workspace $EMPTY
  THEN session starts
  AND workspace is mounted

TC-EDGE-002: Workspace with special characters
  GIVEN workspace path with spaces and unicode
  WHEN ./coi shell --workspace "$SPECIAL_PATH"
  THEN handles correctly

TC-EDGE-003: Very long command
  GIVEN 10KB command string
  WHEN ./coi run "$LONG_COMMAND"
  THEN executes or shows reasonable error

TC-EDGE-004: Rapid session start/stop
  GIVEN coi binary
  WHEN start and stop 100 sessions rapidly
  THEN all complete without errors
  AND no resource leaks

TC-EDGE-005: Session during low disk space
  GIVEN <100MB disk space
  WHEN ./coi shell
  THEN fails gracefully
  AND shows disk space error

TC-EDGE-006: Binary moved during session
  GIVEN active session
  WHEN move coi binary to new location
  THEN session continues working
  AND can still cleanup

TC-EDGE-007: Config file changes during session
  GIVEN active session
  WHEN modify config file
  THEN session uses old config
  AND new sessions use new config

TC-EDGE-008: Container manual modification
  GIVEN persistent container
  WHEN manually modify via incus
  THEN coi handles gracefully
  AND doesn't corrupt state
```

#### 12.2 Stress Tests
```
TC-STRESS-001: Maximum slots
  GIVEN coi binary
  WHEN start 100 parallel slots
  THEN handles gracefully
  AND shows reasonable behavior

TC-STRESS-002: Many saved sessions
  GIVEN 1000 saved sessions
  WHEN ./coi list
  THEN completes in reasonable time
  AND output is usable

TC-STRESS-003: Large .claude directory
  GIVEN session with 1GB .claude data
  WHEN save and resume
  THEN handles correctly
  AND provides progress feedback

TC-STRESS-004: Long-running session
  GIVEN session running for 24 hours
  WHEN exit
  THEN saves correctly
  AND cleanup works

TC-STRESS-005: Rapid build/rebuild
  GIVEN coi binary
  WHEN rebuild images 10 times
  THEN handles correctly
  AND no state corruption
```

## Implementation Plan

### Phase 1: Foundation (Week 1)

**Goal**: Set up E2E test infrastructure

#### Tasks
1. Create `tests/e2e/` directory structure
2. Implement binary execution helpers
   - `ensureBinary()` with caching
   - `RunCLI()` for subprocess execution
   - Output capture and parsing
3. Implement interactive session helpers
   - `InteractiveSession` struct
   - Command sending
   - Output reading
4. Create test fixtures
   - Sample configs
   - Sample workspaces
   - Test data generators
5. Set up build tags for test isolation

#### Deliverables
- `tests/e2e/helpers.go` - Reusable test utilities
- `tests/e2e/fixtures/` - Test data
- Basic Makefile targets

### Phase 2: Smoke Tests (Week 1-2)

**Goal**: Implement quick sanity checks

#### Tasks
1. Implement TC-SMOKE-001 through TC-SMOKE-006
2. Add `make test-smoke` target
3. Ensure smoke tests run in <30 seconds
4. No Incus dependency for smoke tests

#### Deliverables
- `tests/e2e/smoke_test.go`
- Fast feedback for developers

### Phase 3: Command E2E Tests (Week 2-3)

**Goal**: Test each CLI command through binary

#### Tasks
1. Implement build command tests (TC-BUILD-001 to TC-BUILD-005)
2. Implement run command tests (TC-RUN-001 to TC-RUN-010)
3. Implement shell command tests (TC-SHELL-001 to TC-SHELL-010)
4. Implement list command tests (TC-LIST-001 to TC-LIST-005)
5. Implement info command tests (TC-INFO-001 to TC-INFO-004)
6. Implement clean command tests (TC-CLEAN-001 to TC-CLEAN-006)
7. Implement tmux command tests (TC-TMUX-001 to TC-TMUX-004)

#### Deliverables
- `tests/e2e/build_test.go`
- `tests/e2e/run_test.go`
- `tests/e2e/shell_test.go`
- `tests/e2e/list_test.go`
- `tests/e2e/info_test.go`
- `tests/e2e/clean_test.go`
- `tests/e2e/tmux_test.go`

### Phase 4: Workflow Tests (Week 3-4)

**Goal**: Test multi-command workflows

#### Tasks
1. Implement development workflows (TC-WORKFLOW-001 to TC-WORKFLOW-009)
2. Implement configuration workflows (TC-WORKFLOW-010 to TC-WORKFLOW-013)
3. Implement privileged workflows (TC-WORKFLOW-014 to TC-WORKFLOW-017)

#### Deliverables
- `tests/e2e/workflows_test.go`
- `tests/e2e/configuration_test.go`
- `tests/e2e/privileged_test.go`

### Phase 5: Session & Persistence Tests (Week 4-5)

**Goal**: Validate session management and persistent mode

#### Tasks
1. Implement session creation tests (TC-SESSION-001 to TC-SESSION-004)
2. Implement session restoration tests (TC-SESSION-005 to TC-SESSION-009)
3. Implement session cleanup tests (TC-SESSION-010 to TC-SESSION-013)
4. Implement persistence lifecycle tests (TC-PERSIST-001 to TC-PERSIST-004)
5. Implement state persistence tests (TC-PERSIST-005 to TC-PERSIST-009)
6. Implement persistence modes tests (TC-PERSIST-010 to TC-PERSIST-013)

#### Deliverables
- `tests/e2e/session_test.go`
- `tests/e2e/persistence_test.go`

### Phase 6: Configuration & Errors (Week 5-6)

**Goal**: Validate configuration system and error handling

#### Tasks
1. Implement config loading tests (TC-CONFIG-001 to TC-CONFIG-005)
2. Implement config hierarchy tests (TC-CONFIG-006 to TC-CONFIG-008)
3. Implement profile tests (TC-CONFIG-009 to TC-CONFIG-012)
4. Implement dependency error tests (TC-ERROR-001 to TC-ERROR-004)
5. Implement argument error tests (TC-ERROR-005 to TC-ERROR-010)
6. Implement runtime error tests (TC-ERROR-011 to TC-ERROR-015)
7. Implement recovery tests (TC-ERROR-016 to TC-ERROR-018)

#### Deliverables
- `tests/e2e/configuration_test.go`
- `tests/e2e/errors_test.go`

### Phase 7: Parallel & Performance (Week 6-7)

**Goal**: Validate concurrent operations and performance

#### Tasks
1. Implement multi-slot parallelism tests (TC-PARALLEL-001 to TC-PARALLEL-004)
2. Implement multi-workspace tests (TC-PARALLEL-005 to TC-PARALLEL-006)
3. Implement concurrent cleanup tests (TC-PARALLEL-007 to TC-PARALLEL-008)
4. Implement startup performance tests (TC-PERF-001 to TC-PERF-003)
5. Implement scalability tests (TC-PERF-004 to TC-PERF-006)
6. Implement resource tests (TC-PERF-007 to TC-PERF-009)

#### Deliverables
- `tests/e2e/parallel_test.go`
- `tests/e2e/performance_test.go`

### Phase 8: Acceptance & Interactive (Week 7-8)

**Goal**: Validate README examples and interactive sessions

#### Tasks
1. Implement Quick Start examples (TC-ACCEPT-001 to TC-ACCEPT-002)
2. Implement usage examples (TC-ACCEPT-003 to TC-ACCEPT-005)
3. Implement persistent mode examples (TC-ACCEPT-006 to TC-ACCEPT-007)
4. Implement config examples (TC-ACCEPT-008 to TC-ACCEPT-009)
5. Implement interactive command tests (TC-INTERACTIVE-001 to TC-INTERACTIVE-004)
6. Implement file operation tests (TC-INTERACTIVE-005 to TC-INTERACTIVE-007)
7. Implement environment tests (TC-INTERACTIVE-008 to TC-INTERACTIVE-010)

#### Deliverables
- `tests/e2e/acceptance_test.go`
- `tests/e2e/interactive_test.go`

### Phase 9: Edge Cases & Documentation (Week 8-9)

**Goal**: Handle edge cases and document everything

#### Tasks
1. Implement edge case tests (TC-EDGE-001 to TC-EDGE-008)
2. Implement stress tests (TC-STRESS-001 to TC-STRESS-005)
3. Create comprehensive test documentation
4. Add test examples and usage guide
5. Create troubleshooting guide for test failures

#### Deliverables
- `tests/e2e/edge_cases_test.go`
- `tests/e2e/README.md` - E2E testing guide
- `TESTING.md` - Complete testing documentation

### Phase 10: CI/CD Integration (Week 9-10)

**Goal**: Automate testing in CI/CD pipeline

#### Tasks
1. Create GitHub Actions workflows
   - Smoke tests on every push
   - E2E tests on PR
   - Full test suite nightly
2. Add test coverage reporting
3. Add performance regression detection
4. Create test result dashboards
5. Document CI/CD setup

#### Deliverables
- `.github/workflows/smoke.yml`
- `.github/workflows/e2e.yml`
- `.github/workflows/nightly.yml`
- Coverage badges in README

## Success Criteria

### Coverage Metrics
- [ ] **100% of CLI commands** have E2E tests
- [ ] **All README examples** are tested
- [ ] **90%+ of user workflows** covered
- [ ] **All error scenarios** tested
- [ ] **Binary execution** in 80%+ of tests

### Quality Metrics
- [ ] Smoke tests run in **<30 seconds**
- [ ] E2E suite runs in **<10 minutes**
- [ ] All tests are **deterministic** (no flaky tests)
- [ ] Tests are **isolated** (can run in any order)
- [ ] Tests **clean up** resources properly

### Documentation Metrics
- [ ] All test cases **documented** with clear descriptions
- [ ] Test helpers have **usage examples**
- [ ] Failure messages are **actionable**
- [ ] CI/CD setup is **documented**

### Automation Metrics
- [ ] Tests run **automatically** on every PR
- [ ] Test failures **block merges**
- [ ] Performance regressions are **detected**
- [ ] Coverage reports are **generated**

## Test Maintenance

### Test Hygiene
1. **Keep tests fast** - Use fixtures, parallelize where possible
2. **Keep tests isolated** - No shared state between tests
3. **Keep tests focused** - One assertion per test where possible
4. **Keep tests readable** - Clear naming, good comments

### Test Cleanup
1. **Always cleanup containers** - Use defer or t.Cleanup()
2. **Remove temp directories** - Use t.TempDir()
3. **Cleanup sessions** - Remove test session data
4. **Stop containers** - Don't leave running containers

### Test Evolution
1. **Add tests for bugs** - Every bug gets a regression test
2. **Update tests for features** - New features require E2E tests
3. **Remove obsolete tests** - Clean up tests for removed features
4. **Refactor duplicates** - Extract common patterns to helpers

## Appendix: Test Execution Commands

### Run Specific Test Suites
```bash
# Smoke tests only (fast feedback)
make test-smoke

# Unit tests only
make test-unit

# Integration tests (requires Incus)
make test-integration

# E2E tests (requires Incus + images)
make test-e2e

# Acceptance tests (requires full setup)
make test-acceptance

# All tests
make test-all
```

### Run Specific Test Files
```bash
# Run smoke tests
go test -v ./tests/e2e/smoke_test.go

# Run workflow tests
go test -v -tags=integration ./tests/e2e/workflows_test.go

# Run acceptance tests
go test -v -tags=integration,scenarios ./tests/e2e/acceptance_test.go
```

### Run Specific Test Cases
```bash
# Run single test
go test -v -run TestSmoke_BinaryExists ./tests/e2e/

# Run tests matching pattern
go test -v -run "TestWorkflow.*" ./tests/e2e/

# Run with race detection
go test -race -v ./tests/e2e/
```

### Coverage Reports
```bash
# Generate coverage for E2E tests
make test-coverage-e2e

# View coverage in browser
go tool cover -html=coverage-e2e.out

# Generate combined coverage
make test-coverage-all
```

### CI/CD Commands
```bash
# Pre-commit check
make test-unit && make test-integration

# Pre-PR check
make test-e2e

# Release validation
make test-all && make test-coverage-all
```

## Appendix: Test Case Summary

### Total Test Cases by Category

| Category | Test Cases | Priority |
|----------|-----------|----------|
| Smoke Tests | 6 | HIGH |
| Build Command | 5 | HIGH |
| Run Command | 10 | HIGH |
| Shell Command | 10 | HIGH |
| List Command | 5 | MEDIUM |
| Info Command | 4 | MEDIUM |
| Clean Command | 6 | MEDIUM |
| Tmux Command | 4 | LOW |
| **Workflows (Total)** | **60** | **HIGH** |
| - Basic Workflows | 17 | HIGH |
| - Multi-Shell Workflows | 6 | HIGH |
| - Resume & Continue | 8 | HIGH |
| - Tmux Integration | 8 | HIGH |
| - Storage & Mounting | 5 | HIGH |
| - Image Management | 5 | MEDIUM |
| - Advanced Development | 7 | MEDIUM |
| - Cleanup & Maintenance | 4 | MEDIUM |
| Session Management | 13 | HIGH |
| Persistence | 13 | HIGH |
| Configuration | 12 | HIGH |
| Error Handling | 18 | HIGH |
| Parallel Operations | 8 | MEDIUM |
| Performance | 9 | MEDIUM |
| Acceptance | 9 | HIGH |
| Interactive | 10 | MEDIUM |
| Edge Cases | 8 | LOW |
| Stress Tests | 5 | LOW |
| **TOTAL** | **215** | - |

### Priority Breakdown

- **HIGH Priority**: 139 test cases (65%)
- **MEDIUM Priority**: 63 test cases (29%)
- **LOW Priority**: 13 test cases (6%)

### Estimated Effort

- **Phase 1-2** (Foundation + Smoke): 1.5 weeks
- **Phase 3** (Commands): 2 weeks
- **Phase 4-5** (Workflows + Session): 2.5 weeks
- **Phase 6-7** (Config + Errors + Performance): 2 weeks
- **Phase 8-9** (Acceptance + Edge Cases): 1.5 weeks
- **Phase 10** (CI/CD): 0.5 weeks

**Total Estimated Time**: 10 weeks for full implementation

### Quick Wins (Week 1-2)

Focus on these for immediate impact:
1. Smoke tests (6 tests) - Quick validation
2. Core command tests (Build, Run, Shell) - 25 tests
3. Multi-shell workflows (6 tests) - Multiple sessions for same workspace
4. Resume & continue workflows (8 tests) - Session restoration
5. Tmux integration basics (4 tests) - Background process management
6. Error handling basics (10 tests) - Better UX

**Total Quick Wins**: 59 tests in 2 weeks

### High-Value Tests (Week 3-4)

After quick wins, prioritize:
1. Storage & mounting workflows (5 tests) - File synchronization
2. Session management (13 tests) - Full lifecycle
3. Persistence workflows (13 tests) - Container reuse
4. Configuration tests (12 tests) - Config hierarchy
5. Parallel operations (8 tests) - Multi-slot concurrency

**Total High-Value**: 51 tests
