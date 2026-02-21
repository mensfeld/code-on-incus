# Security Monitoring Test Fixes - Progress Report

## Session Summary
Debugging and fixing security monitoring integration tests for PR #125.

## Key Breakthrough: PHP Detection Bug Found and Fixed

### Root Cause
The PHP reverse shell detection was failing because `strings.Contains("fsockopen", "socket")` returns **FALSE**.

- `"fsockopen"` does NOT contain the substring `"socket"` (it's "fsock" + "open", not "f" + "socket" + "open")
- Python works: `"socket.socket"` DOES contain `"socket"` ✅
- Perl works: `"use IO::Socket"` DOES contain `"socket"` ✅
- PHP fails: `"fsockopen"` does NOT contain `"socket"` ❌

### The Fix (Commit 7a93a8b)
Changed the network detection check from:
```go
strings.Contains(cmdLower, "socket")
```

To:
```go
strings.Contains(cmdLower, "sock") // Matches socket, fsockopen, etc.
```

This now catches:
- `socket.socket` (Python) ✅
- `IO::Socket` (Perl) ✅
- `fsockopen` (PHP) ✅

## Current Status

### Tests Fixed: 26/30 passing (87%)

**Confirmed Working:**
- ✅ Netcat reverse shells
- ✅ Bash reverse shells
- ✅ Python reverse shells
- ✅ Perl reverse shells
- ✅ Environment variable scanning
- ✅ Automated container responses (kill/pause)
- ✅ Configuration-based monitoring
- ✅ Audit logging (JSONL format)

### Tests Remaining: 4 failures

**1. PHP reverse shell detection** (Commit 7a93a8b - awaiting CI confirmation)
- Root cause: Identified and fixed
- Expected: Will pass in current CI run

**2-4. Filesystem monitoring** (3 tests)
- `test_large_file_read_triggers_auto_pause`
- `test_file_read_at_threshold_triggers`
- `test_file_read_above_threshold_triggers`
- Issue: Cgroup I/O stats may not be available
- Debug logging added (Commit bb7e3f9) to investigate

## Commits Made This Session

1. **5251e3f** - Add debug logging for PHP/scripting language processes
2. **7a93a8b** - Fix PHP reverse shell detection - check for 'sock' instead of 'socket'
3. **bb7e3f9** - Add debug logging for filesystem I/O monitoring

## Next Steps

1. ✅ Confirm PHP fix passes CI (run 22056758042 - in progress)
2. 🔍 Investigate filesystem I/O monitoring with debug logs (run 22056804364 - pending)
3. 🎯 Fix remaining filesystem monitoring tests
4. 🚀 Achieve 100% pass rate (30/30 tests)

## Technical Notes

### Why Filesystem Tests Might Fail
- Cgroup I/O stats collection may be failing silently
- Code has fallback: if `readIOStats()` fails, sets `IOReadMB = 0`
- If always 0, delta is always 0, so thresholds never trigger
- Need to check if `io.stat` file exists/is readable in CI containers

### Debug Logging Added
- `[cgroup]` logs: I/O stat collection success/failure
- `[filesystem]` logs: Read deltas and rates
- `[detector]` logs: Process matching for scripting languages
