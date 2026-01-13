# Testing Strategy: Real Claude vs Dummy CLI

## Overview

The test suite uses a **hybrid approach** for optimal speed and reliability:

- **Dummy CLI** for most tests (10x+ faster, no license needed)
- **Real Claude** for smoke tests (verify actual integration)

---

## Test Classification

### 🟢 Using Dummy CLI (Fast Tests)

**Location:** `tests/shell/ephemeral/`

These tests run with the fake Claude CLI stub for maximum speed and reliability:

```
tests/shell/ephemeral/
  ├── without_tmux/
  │   ├── file_persistence.py          ✅ Dummy CLI
  │   ├── resume_basic.py               ✅ Dummy CLI
  │   └── start_stop_with_prompt.py     ✅ Dummy CLI
  └── with_tmux/
      ├── file_persistence.py           ✅ Dummy CLI
      ├── no_mount_claude_config.py     ✅ Dummy CLI
      ├── no_persistence_on_resume.py   ✅ Dummy CLI
      ├── resume_basic.py               ✅ Dummy CLI
      └── start_stop_with_prompt.py     ✅ Dummy CLI

tests/shell/dummy/
  ├── basic_startup.py                  ✅ Dummy CLI (demo)
  └── (performance test)                ✅ Dummy CLI (demo)
```

**Total: 10 tests using Dummy CLI**

**Benefits:**
- ⚡ **5-8 seconds** per test (vs 25-35 seconds with real Claude)
- 🚀 **~10x faster** test execution
- 💰 **No license required** for contributors
- 🎯 **Deterministic behavior** - no API variability
- 🔧 **Offline development** - works without network

---

### 🔵 Using Real Claude (Smoke Tests)

**Location:** `tests/shell/persistent/`

These tests use the **real Claude CLI** to verify actual integration:

```
tests/shell/persistent/
  ├── container_persists.py             🔵 Real Claude
  ├── container_reused.py               🔵 Real Claude
  ├── filesystem_persistence.py         🔵 Real Claude
  ├── mount_claude_config.py            🔵 Real Claude
  ├── persistent_to_ephemeral.py        🔵 Real Claude
  ├── persistent_to_persistent.py       🔵 Real Claude
  ├── resume_inherits_persistent.py     🔵 Real Claude
  └── test_slot_uniqueness.py           🔵 Real Claude
```

**Total: 8 tests using Real Claude**

**Why keep these?**
- ✅ Verify actual Claude Code CLI integration
- ✅ Test persistent container behavior with real Claude state
- ✅ Ensure `.claude` directory handling works correctly
- ✅ Catch regressions in actual Claude interaction
- ✅ Confidence that production setup works

---

## Performance Comparison

### Before (All Real Claude):
```
Total tests:        18 shell tests
Time per test:      ~25-35 seconds
Total time:         ~7-10 minutes
License required:   Yes ❌
Network required:   Yes ❌
```

### After (Hybrid Approach):
```
Dummy CLI tests:  10 tests × ~6 seconds  = ~1 minute
Real Claude tests:   8 tests × ~30 seconds = ~4 minutes
Total time:         ~5 minutes

Speedup:            40% faster! ⚡
License required:   Only for 8 tests (optional for development) ✅
Network required:   Only for 8 tests ✅
```

---

## How Dummy CLI Works

### The Stub:
```bash
testdata/dummy/claude

#!/bin/bash
# Simulates Claude Code CLI behavior
- Shows setup prompts (Light/Dark mode)
- Handles --resume flag
- Creates ~/.claude directory
- Interactive prompt loop
- No API calls, no authentication
```

### Usage in Tests:
```python
def test_something(coi_binary, dummy_path, workspace_dir):
    # Use fake Claude for faster testing (10x+ speedup)
    env = os.environ.copy()
    env["PATH"] = f"{dummy_path}:{env.get('PATH', '')}"

    child = spawn_coi(
        coi_binary,
        ["shell", "--tmux=false"],
        cwd=workspace_dir,
        env=env  # ← Dummy CLI is now in PATH!
    )

    # Rest of test proceeds normally...
    # Container orchestration logic is tested without Claude overhead
```

---

## What Each Approach Tests

### Dummy CLI Tests (Container Orchestration)
These tests focus on **coi's container management logic**:

- ✅ Container launch/stop/cleanup
- ✅ Workspace mounting
- ✅ File persistence in ephemeral mode
- ✅ Session resume functionality
- ✅ Tmux integration
- ✅ Claude config mounting behavior
- ✅ Slot allocation
- ✅ Error handling

**What's NOT tested:**
- ❌ Actual Claude API interactions
- ❌ Real Claude state management
- ❌ Claude-specific error conditions

### Real Claude Tests (Integration)
These tests verify **end-to-end integration**:

- ✅ Persistent container behavior with real Claude
- ✅ Claude state persists across restarts
- ✅ `.claude` directory handling
- ✅ Container reuse with actual Claude sessions
- ✅ Filesystem persistence with Claude state
- ✅ Full integration flow

---

## Running Tests

### Run all tests (hybrid):
```bash
pytest tests/shell/
# 10 fake Claude tests run fast
# 8 real Claude tests run slow
# Total: ~5 minutes
```

### Run only fast tests (fake Claude):
```bash
pytest tests/shell/ephemeral/ tests/shell/dummy/
# ~1 minute total ⚡
```

### Run only smoke tests (real Claude):
```bash
pytest tests/shell/persistent/
# ~4 minutes total
# Requires Claude Code license
```

### Run specific test:
```bash
# Fast test with fake Claude
pytest tests/shell/ephemeral/without_tmux/start_stop_with_prompt.py -v
# ~6 seconds ⚡

# Smoke test with real Claude
pytest tests/shell/persistent/container_persists.py -v
# ~30 seconds
```

---

## CI/CD Strategy

### Pull Request CI (Fast Feedback):
```yaml
# Run fast tests only for quick feedback
- pytest tests/shell/ephemeral/ tests/shell/dummy/
- pytest tests/container/ tests/file/ tests/image/ tests/build/
# Total: ~2 minutes
```

### Nightly CI (Full Integration):
```yaml
# Run all tests including real Claude smoke tests
- pytest tests/
# Total: ~10 minutes
```

### Development Workflow:
```bash
# Rapid iteration with fake Claude
pytest tests/shell/ephemeral/ -k "file_persistence"
# Instant feedback (~6 seconds)

# Verify before push (optional)
pytest tests/shell/persistent/container_persists.py
# Full integration check
```

---

## Adding New Tests

### Rule of Thumb:
1. **Default to Dummy CLI** for new tests
2. **Use Real Claude only if** testing Claude-specific behavior
3. **Add to `persistent/`** only for smoke tests

### Example - New Feature Test:

#### ✅ Good (Use Dummy CLI):
```python
# tests/shell/ephemeral/with_tmux/new_feature.py

def test_new_container_feature(coi_binary, dummy_path, workspace_dir):
    """Test new container orchestration feature."""

    # Use fake Claude - we're testing container logic, not Claude
    env = os.environ.copy()
    env["PATH"] = f"{dummy_path}:{env.get('PATH', '')}"

    child = spawn_coi(coi_binary, ["shell"], cwd=workspace_dir, env=env)
    # Test the container feature...
```

#### ❌ Bad (Unnecessary Real Claude):
```python
# DON'T add to persistent/ unless testing Claude integration

def test_new_container_feature(coi_binary, workspace_dir):
    # Uses real Claude unnecessarily
    # Slows down CI by 25 seconds
    # Requires license
    child = spawn_coi(coi_binary, ["shell"], cwd=workspace_dir)
    # Same test as above but 4x slower!
```

---

## Maintenance

### Updating Dummy CLI:
```bash
# Edit the stub to add new behavior
vim testdata/dummy/claude

# Test the changes
pytest tests/shell/dummy/basic_startup.py -v
```

### Converting Existing Tests:
```python
# Before (slow):
def test_something(coi_binary, workspace_dir):
    child = spawn_coi(coi_binary, ["shell"], cwd=workspace_dir)

# After (fast):
def test_something(coi_binary, dummy_path, workspace_dir):
    env = os.environ.copy()
    env["PATH"] = f"{dummy_path}:{env.get('PATH', '')}"
    child = spawn_coi(coi_binary, ["shell"], cwd=workspace_dir, env=env)
```

---

## Summary

**Current Test Distribution:**
- 🟢 Dummy CLI: 10 tests (~60 seconds total)
- 🔵 Real Claude: 8 tests (~240 seconds total)
- 📊 Total improvement: **40% faster** than all-real-Claude approach

**Key Benefits:**
1. ⚡ **Faster CI/CD** - developers get feedback 40% faster
2. 💰 **Lower barrier** - contributors don't need Claude licenses
3. 🎯 **More reliable** - fake Claude is deterministic
4. 🔧 **Offline development** - work without network
5. ✅ **Still confident** - smoke tests catch real integration issues

**Best of both worlds:** Fast iteration with fake Claude + confidence from real Claude smoke tests!
