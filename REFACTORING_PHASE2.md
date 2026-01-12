# Variable Renaming - Phase 2

## Summary
Completed renaming of remaining Claude-specific variable names to CLI-agnostic terminology. This builds on Phase 1 (function renames) and completes the internal API cleanup.

## Changes Made

### Variables Renamed (3 types, ~20 occurrences)

1. **`claudeDir` → `stateDir`** (7 occurrences)
   - References to the `.claude` state directory
   - Now uses generic "state" terminology

2. **`claudePath` → `statePath`** (6 occurrences)
   - Path variables pointing to state directories
   - Consistent with `stateDir` naming

3. **`claudeJsonPath` → `stateConfigPath`** (8 occurrences)
   - References to `.claude.json` configuration file
   - More descriptive of what it actually is (config for state)

## Files Modified (4)

1. `internal/session/setup.go` - 30 lines changed
   - Primary location of state management
   - Config setup and credential injection

2. `internal/session/cleanup.go` - 12 lines changed
   - Session cleanup and state preservation
   - Directory handling

3. `internal/cli/info.go` - 6 lines changed
   - Session info display
   - State directory size calculation

4. `internal/cli/list.go` - 4 lines changed
   - Session listing
   - State directory checks

## Total Changes
- **52 lines modified** across 4 files
- **~20 variable occurrences** renamed
- **0 breaking changes** - all internal

## Verification
- ✅ Code compiles successfully
- ✅ Binary runs: `coi version` works
- ✅ No old variable names remain in internal/ code
- ✅ All changes are internal only

## Combined with Phase 1

### Complete Internal API Cleanup:
**Functions:** (Phase 1)
- `runClaude()` → `runCLI()`
- `runClaudeInTmux()` → `runCLIInTmux()`
- `GetClaudeSessionID()` → `GetCLISessionID()`
- `setupClaudeConfig()` → `setupCLIConfig()`

**Variables:** (Phase 1)
- `claudeBinary` → `cliBinary`
- `claudeCmd` → `cliCmd`
- `claudeSessionID` → `cliSessionID`
- `hostClaudeConfigPath` → `hostCLIConfigPath`

**Variables:** (Phase 2 - this commit)
- `claudeDir` → `stateDir`
- `claudePath` → `statePath`
- `claudeJsonPath` → `stateConfigPath`

**Struct Fields:** (Phase 1)
- `SetupOptions.ClaudeConfigPath` → `SetupOptions.CLIConfigPath`

## What's Left?

### Still Claude-Specific (Intentional):
1. **Directory path literals**: `".claude"` (hardcoded)
   - Will be configurable in next phase
   
2. **Default binary name**: `"claude"` (hardcoded)
   - Will be configurable in next phase

3. **User-facing text**: Help messages, descriptions
   - Will be addressed in separate cosmetic update

4. **Comments**: Some documentation comments
   - Will be updated in cosmetic pass

## Next Steps

Now that internal APIs are fully generic, we can:

1. **Add configuration support** (Phase 3)
   ```toml
   [cli]
   binary = "aider"
   state_dir = "~/.aider"
   ```

2. **Update user-facing text** (Phase 4)
   - Help messages
   - Command descriptions
   - Log messages

3. **Test with other tools** (Phase 5)
   - Validate with aider
   - Validate with cursor
   - Document tool-specific quirks

## Status
✅ Internal API is now fully CLI-agnostic
✅ Ready for configuration-based tool selection
✅ Zero breaking changes throughout
