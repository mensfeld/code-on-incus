# Credential Catalog & Generic `[[credentials]]` Seeding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Task headers use stable kebab-case slugs, not integer ordinals; cite the slug (e.g. `credential-catalog`) in commit messages and cross-references, not "Task 1".

**Goal:** Let coi seed named or ad-hoc host credential files (SSH keys, tokens, config dirs) into containers for both the three builtin AI tools (Claude/OpenCode/Pi) and third-party providers like Ollama, from one shared catalog and a new profile-level `[[credentials]]` config section.

**Architecture:** A new `internal/tool/credentials` package holds an embedded TOML catalog of named bundles (`claude`, `opencode`, `pi`, `ollama`, ...). The three builtin `Tool` implementations source their existing `ToolWithConfigDirFiles` metadata from this catalog instead of hardcoded Go literals (pure refactor, no behavior change). A new `config.CredentialEntry` type (parallel to `MountEntry`/`SocketEntry`) lets profiles reference a catalog bundle by name or declare an ad-hoc host/container file pair. `session.trust.go`'s existing combined mount+socket trust-gate is extended to cover ad-hoc credential entries the same way it already covers sockets (no "within workspace" exemption); catalog-referenced entries are never gated, matching the trust level builtin tool credentials already have. A new `setupCredentials` function (push + chown + optional chmod) applies the resolved, gated entries at session setup, on both fresh sessions and resume.

**Tech Stack:** Go, BurntSushi/toml (already a dependency), Incus CLI via the existing `container.Manager` wrapper. No new dependencies.

## Global Constraints

- Go, no new third-party dependencies, reuse `github.com/BurntSushi/toml` (already imported by `internal/session/trust.go` and used throughout `internal/config`).
- Follow existing code style: plain `testing` package (no testify), table-free simple `t.Errorf`/`t.Fatalf` assertions, matching every existing test file in this repo.
- Every new/modified exported function needs a doc comment starting with its name, matching existing convention throughout `internal/session`, `internal/config`, `internal/tool`.
- Never rewrite an existing task's tests to make them pass, if a signature change breaks an existing test, update the call site to the new signature and confirm the test's original intent is preserved.
- Commit after each task once its tests pass. Do not batch commits across tasks.

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `internal/tool/credentials/catalog.toml` | Create | Embedded data: named credential bundles (`claude`, `opencode`, `pi`, `ollama`) |
| `internal/tool/credentials/catalog.go` | Create | `Bundle` struct, embed + parse, `Lookup`/`Names` |
| `internal/tool/credentials/catalog_test.go` | Create | Regression-locks catalog values against today's hardcoded tool values |
| `internal/tool/tool.go` | Modify | `ClaudeTool`'s `ToolWithConfigDirFiles` methods become catalog lookups; add shared `mustBundle` helper |
| `internal/tool/opencode.go` | Modify | `OpencodeTool`'s `ToolWithConfigDirFiles` methods become catalog lookups |
| `internal/tool/pi.go` | Modify | `PiTool`'s `ToolWithConfigDirFiles` methods become catalog lookups |
| `internal/config/config.go` | Modify | `CredentialEntry` type, `Config.Credentials`/`ProfileConfig.Credentials` fields, `ApplyProfile` merge, `Validate` checks |
| `internal/config/credential_entry_test.go` | Create | Validation + merge tests for the above |
| `internal/session/types.go` | Modify | `CredentialEntry`/`CredentialConfig` session-layer types |
| `internal/cli/credential_parser.go` | Create | `ParseCredentialConfig`, `warnDroppedCredentials` |
| `internal/cli/credential_parser_test.go` | Create | Parser tests: bundle expansion, ad-hoc expansion, unknown-bundle error |
| `internal/session/trust.go` | Modify | `untrustedCredentials`, extend `sourceFingerprint`/`trustedSources`/`FilterTrusted`/`TrustSources`/`UntrustedSourcePaths` to cover credentials |
| `internal/session/trust_test.go` | Modify (full replacement) | Update every call site for the new signatures + add credential-specific cases |
| `internal/cli/run.go` | Modify | `gateRunForwarding`'s `FilterTrusted` call updated for the new signature (mechanical, run path doesn't pre-filter credentials) |
| `internal/cli/trust.go` | Modify | `runTrust`/`runUntrust` parse and thread `CredentialConfig` so `coi trust`/`coi untrust` cover ad-hoc credentials |
| `internal/session/setup_credentials.go` | Create | `setupCredentials`: push + chown + optional chmod per entry |
| `internal/session/setup_credentials_integration_test.go` | Create | Real-container integration test (skipped without local Incus), mirrors `context_file_integration_test.go` |
| `internal/session/setup.go` | Modify | `SetupOptions.CredentialConfig` field; extend the existing trust-gate block; call `setupCredentials` on resume and on fresh-session setup |
| `internal/cli/phases_shell.go` | Modify | Parse `CredentialConfig` alongside mounts/sockets, thread into `SetupOptions` |

---

### Task: credential-catalog - Embedded Credential Bundle Catalog

**Files:**
- Create: `internal/tool/credentials/catalog.toml`
- Create: `internal/tool/credentials/catalog.go`
- Test: `internal/tool/credentials/catalog_test.go`

**Interfaces:**
- Produces: `credentials.Bundle{ConfigDir, Files, StateFile, SandboxSettingsFile, AlwaysSetup, AutoContextFile, Mode string/[]string/bool}`, `credentials.Lookup(name string) (Bundle, bool)`, `credentials.Names() []string`, consumed by Task `builtin-tool-catalog-wiring` and Task `session-credential-parser`.

- [ ] **Step 1: Write the catalog data file**

Create `internal/tool/credentials/catalog.toml`:

```toml
# Named credential bundles. `claude`, `opencode`, and `pi` back the builtin
# Tool implementations' ToolWithConfigDirFiles methods (internal/tool), these
# values must stay in sync with what setupCLIConfig expects. Other entries
# (e.g. `ollama`) are third-party bundles referenced from profile
# [[credentials]] entries via `bundle = "<name>"`; only config_dir, files, and
# (optionally) mode apply to those, sandbox_settings_file/state_file/
# always_setup/auto_context_file are only meaningful for tools consumed
# through internal/tool's ToolWithConfigDirFiles interface.

[claude]
config_dir = ".claude"
files = [".credentials.json", "config.yml", "settings.json", "CLAUDE.md"]
state_file = ".claude.json"
sandbox_settings_file = "settings.json"
always_setup = false
auto_context_file = ".claude/CLAUDE.md"

[opencode]
config_dir = ".config/opencode"
files = ["opencode.json", "tui.json"]
sandbox_settings_file = "opencode.json"
always_setup = true

[pi]
config_dir = ".pi/agent"
files = ["settings.json", "models.json", "auth.json", "AGENTS.md"]
sandbox_settings_file = "settings.json"
always_setup = true

[ollama]
config_dir = ".ollama"
files = ["id_ed25519"]
mode = "0600"
```

- [ ] **Step 2: Write the catalog loader**

Create `internal/tool/credentials/catalog.go`:

```go
// Package credentials holds the embedded catalog of named credential
// bundles: which host config directory and files to copy into a container
// for a given tool or third-party provider. Builtin AI tools (claude,
// opencode, pi) source their ToolWithConfigDirFiles metadata from here;
// profile [[credentials]] entries can reference any bundle by name.
package credentials

import (
	_ "embed"
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"
)

//go:embed catalog.toml
var catalogTOML string

// Bundle describes a named, coi-known set of host credential files: which
// directory they live in on both host and container (relative to the
// respective home directory), which files to copy, and how to treat them
// once copied.
type Bundle struct {
	ConfigDir           string   `toml:"config_dir"`
	Files               []string `toml:"files"`
	StateFile           string   `toml:"state_file"`
	SandboxSettingsFile string   `toml:"sandbox_settings_file"`
	AlwaysSetup         bool     `toml:"always_setup"`
	AutoContextFile     string   `toml:"auto_context_file"`
	Mode                string   `toml:"mode"`
}

var catalog map[string]Bundle

func init() {
	if _, err := toml.Decode(catalogTOML, &catalog); err != nil {
		panic(fmt.Sprintf("credentials: embedded catalog.toml is invalid: %v", err))
	}
}

// Lookup returns the named bundle and whether it exists in the catalog.
func Lookup(name string) (Bundle, bool) {
	b, ok := catalog[name]
	return b, ok
}

// Names returns the sorted list of known bundle names, for error messages.
func Names() []string {
	names := make([]string, 0, len(catalog))
	for n := range catalog {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
```

- [ ] **Step 3: Write the regression-lock test**

Create `internal/tool/credentials/catalog_test.go`:

```go
package credentials

import (
	"reflect"
	"testing"
)

func TestLookup_KnownBundles(t *testing.T) {
	for _, name := range []string{"claude", "opencode", "pi", "ollama"} {
		if _, ok := Lookup(name); !ok {
			t.Errorf("Lookup(%q): expected bundle to exist", name)
		}
	}
}

func TestLookup_UnknownBundle(t *testing.T) {
	if _, ok := Lookup("not-a-real-bundle"); ok {
		t.Fatal(`Lookup("not-a-real-bundle"): expected ok=false`)
	}
}

func TestNames_Sorted(t *testing.T) {
	names := Names()
	if !reflect.DeepEqual(names, []string{"claude", "ollama", "opencode", "pi"}) {
		t.Errorf("Names() = %v, want sorted [claude ollama opencode pi]", names)
	}
}

// TestClaudeBundle_MatchesHardcodedValues locks the claude catalog entry to
// the values ClaudeTool hardcoded before the catalog existed, a regression
// guard for the refactor (task builtin-tool-catalog-wiring) that points
// ClaudeTool's ToolWithConfigDirFiles methods at this bundle instead.
func TestClaudeBundle_MatchesHardcodedValues(t *testing.T) {
	b, ok := Lookup("claude")
	if !ok {
		t.Fatal("claude bundle not found")
	}
	if b.ConfigDir != ".claude" {
		t.Errorf("ConfigDir = %q, want %q", b.ConfigDir, ".claude")
	}
	want := []string{".credentials.json", "config.yml", "settings.json", "CLAUDE.md"}
	if !reflect.DeepEqual(b.Files, want) {
		t.Errorf("Files = %v, want %v", b.Files, want)
	}
	if b.StateFile != ".claude.json" {
		t.Errorf("StateFile = %q, want %q", b.StateFile, ".claude.json")
	}
	if b.SandboxSettingsFile != "settings.json" {
		t.Errorf("SandboxSettingsFile = %q, want %q", b.SandboxSettingsFile, "settings.json")
	}
	if b.AlwaysSetup {
		t.Error("AlwaysSetup = true, want false")
	}
	if b.AutoContextFile != ".claude/CLAUDE.md" {
		t.Errorf("AutoContextFile = %q, want %q", b.AutoContextFile, ".claude/CLAUDE.md")
	}
}

func TestOpencodeBundle_MatchesHardcodedValues(t *testing.T) {
	b, ok := Lookup("opencode")
	if !ok {
		t.Fatal("opencode bundle not found")
	}
	if b.ConfigDir != ".config/opencode" {
		t.Errorf("ConfigDir = %q, want %q", b.ConfigDir, ".config/opencode")
	}
	want := []string{"opencode.json", "tui.json"}
	if !reflect.DeepEqual(b.Files, want) {
		t.Errorf("Files = %v, want %v", b.Files, want)
	}
	if b.SandboxSettingsFile != "opencode.json" {
		t.Errorf("SandboxSettingsFile = %q, want %q", b.SandboxSettingsFile, "opencode.json")
	}
	if b.StateFile != "" {
		t.Errorf("StateFile = %q, want empty", b.StateFile)
	}
	if !b.AlwaysSetup {
		t.Error("AlwaysSetup = false, want true")
	}
}

func TestPiBundle_MatchesHardcodedValues(t *testing.T) {
	b, ok := Lookup("pi")
	if !ok {
		t.Fatal("pi bundle not found")
	}
	if b.ConfigDir != ".pi/agent" {
		t.Errorf("ConfigDir = %q, want %q", b.ConfigDir, ".pi/agent")
	}
	want := []string{"settings.json", "models.json", "auth.json", "AGENTS.md"}
	if !reflect.DeepEqual(b.Files, want) {
		t.Errorf("Files = %v, want %v", b.Files, want)
	}
	if b.SandboxSettingsFile != "settings.json" {
		t.Errorf("SandboxSettingsFile = %q, want %q", b.SandboxSettingsFile, "settings.json")
	}
	if b.StateFile != "" {
		t.Errorf("StateFile = %q, want empty", b.StateFile)
	}
	if !b.AlwaysSetup {
		t.Error("AlwaysSetup = false, want true")
	}
}

func TestOllamaBundle_Shape(t *testing.T) {
	b, ok := Lookup("ollama")
	if !ok {
		t.Fatal("ollama bundle not found")
	}
	if b.ConfigDir != ".ollama" {
		t.Errorf("ConfigDir = %q, want %q", b.ConfigDir, ".ollama")
	}
	want := []string{"id_ed25519"}
	if !reflect.DeepEqual(b.Files, want) {
		t.Errorf("Files = %v, want %v", b.Files, want)
	}
	if b.Mode != "0600" {
		t.Errorf("Mode = %q, want %q", b.Mode, "0600")
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tool/credentials/...`
Expected: PASS (all tests above pass against the catalog.toml written in Step 1, this is a data file plus its own regression lock, not a red/green TDD cycle).

- [ ] **Step 5: Commit**

```bash
git add internal/tool/credentials/
git commit -m "$(cat <<'EOF'
Add embedded credential bundle catalog (task credential-catalog)

Introduces internal/tool/credentials with named bundles for claude,
opencode, pi (mirroring today's hardcoded ToolWithConfigDirFiles values
exactly) plus a new curated ollama bundle. Not yet wired into the builtin
tools, that's task builtin-tool-catalog-wiring.

Part of #549.
EOF
)"
```

---

### Task: builtin-tool-catalog-wiring - Point Builtin Tools at the Catalog

**Files:**
- Modify: `internal/tool/tool.go:176-194` (ClaudeTool's `ToolWithConfigDirFiles` methods + `AutoContextFile`)
- Modify: `internal/tool/opencode.go:83-97` (OpencodeTool's `ToolWithConfigDirFiles` methods)
- Modify: `internal/tool/pi.go:92-107` (PiTool's `ToolWithConfigDirFiles` methods)

**Interfaces:**
- Consumes: `credentials.Lookup(name string) (credentials.Bundle, bool)` from task `credential-catalog`.
- Produces: no new exported surface. `ClaudeTool`/`OpencodeTool`/`PiTool` keep their existing method signatures; only the method *bodies* change. Existing callers (`internal/session/setup_config.go`, `internal/cli/phases_shell.go`, etc.) are unaffected.

This task is a pure refactor with an existing regression safety net: `internal/tool/tool_test.go`, `internal/tool/opencode_test.go`, and `internal/tool/pi_test.go` already assert the exact values these methods must keep returning (`TestClaudeTool_EssentialConfigFiles_IncludesCLAUDEMD`, `TestOpencodeTool_EssentialConfigFiles`, `TestPiTool_EssentialConfigFiles`, `TestClaudeTool_AutoContextFile`, and the `ConfigDirName`/`SandboxSettingsFileName`/`StateConfigFileName`/`AlwaysSetupConfig` assertions in each file). No new tests are needed: this task's "test" step is confirming those existing tests still pass unchanged.

- [ ] **Step 1: Confirm the existing characterization tests pass before touching anything**

Run: `go test ./internal/tool/... -run 'EssentialConfigFiles|ConfigDirName|SandboxSettingsFileName|StateConfigFileName|AlwaysSetupConfig|AutoContextFile' -v`
Expected: PASS (baseline, against the current hardcoded implementation).

- [ ] **Step 2: Add the shared `mustBundle` helper to `internal/tool/tool.go`**

Add this function near the top of `internal/tool/tool.go` (after the imports, before `ClaudeTool`), and add the import:

```go
import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/mensfeld/code-on-incus/internal/tool/credentials"
)
```

```go
// mustBundle looks up a named credential bundle from the embedded catalog
// (internal/tool/credentials). Panics if missing: a builtin tool
// referencing an unknown bundle name is a programming error (a typo in this
// package or a catalog entry that was renamed/removed), not a runtime
// condition to recover from.
func mustBundle(name string) credentials.Bundle {
	b, ok := credentials.Lookup(name)
	if !ok {
		panic(fmt.Sprintf("tool: unknown credential bundle %q", name))
	}
	return b
}
```

(`fmt` is not currently imported by `tool.go`; add it to the import block as shown above.)

- [ ] **Step 3: Replace `ClaudeTool`'s `ToolWithConfigDirFiles` methods and `AutoContextFile`**

In `internal/tool/tool.go`, replace lines 176-194 (from the `EssentialConfigFiles` doc comment through the `AutoContextFile` method):

```go
// EssentialConfigFiles implements ToolWithConfigDirFiles.
func (c *ClaudeTool) EssentialConfigFiles() []string {
	return mustBundle("claude").Files
}

// SandboxSettingsFileName implements ToolWithConfigDirFiles.
func (c *ClaudeTool) SandboxSettingsFileName() string { return mustBundle("claude").SandboxSettingsFile }

// StateConfigFileName implements ToolWithConfigDirFiles.
// Claude uses ~/.claude.json as a sibling state file next to ~/.claude/.
func (c *ClaudeTool) StateConfigFileName() string { return mustBundle("claude").StateFile }

// AlwaysSetupConfig implements ToolWithConfigDirFiles.
// Claude needs credentials from ~/.claude, so skip setup when host dir is missing.
func (c *ClaudeTool) AlwaysSetupConfig() bool { return mustBundle("claude").AlwaysSetup }

// AutoContextFile implements ToolWithAutoContextFile.
// Claude Code automatically reads ~/.claude/CLAUDE.md as user-level instructions.
func (c *ClaudeTool) AutoContextFile() string { return mustBundle("claude").AutoContextFile }
```

Also replace the `ConfigDirName` method (line 94-96):

```go
func (c *ClaudeTool) ConfigDirName() string {
	return mustBundle("claude").ConfigDir
}
```

- [ ] **Step 4: Replace `OpencodeTool`'s `ToolWithConfigDirFiles` methods**

In `internal/tool/opencode.go`, replace the `ConfigDirName` method (line 19):

```go
// ConfigDirName returns the XDG-standard config directory for opencode.
func (c *OpencodeTool) ConfigDirName() string { return mustBundle("opencode").ConfigDir }
```

Replace lines 83-97 (`EssentialConfigFiles` through `AlwaysSetupConfig`):

```go
// EssentialConfigFiles implements ToolWithConfigDirFiles.
func (c *OpencodeTool) EssentialConfigFiles() []string {
	return mustBundle("opencode").Files
}

// SandboxSettingsFileName implements ToolWithConfigDirFiles.
func (c *OpencodeTool) SandboxSettingsFileName() string { return mustBundle("opencode").SandboxSettingsFile }

// StateConfigFileName implements ToolWithConfigDirFiles.
// Opencode has no sibling state file.
func (c *OpencodeTool) StateConfigFileName() string { return mustBundle("opencode").StateFile }

// AlwaysSetupConfig implements ToolWithConfigDirFiles.
// Opencode needs sandbox permission bypass even without host config dir.
func (c *OpencodeTool) AlwaysSetupConfig() bool { return mustBundle("opencode").AlwaysSetup }
```

- [ ] **Step 5: Replace `PiTool`'s `ToolWithConfigDirFiles` methods**

In `internal/tool/pi.go`, replace the `ConfigDirName` method (line 22):

```go
// ConfigDirName returns the config directory for pi.
// Pi stores config in ~/.pi/agent/ (controlled by PI_CODING_AGENT_DIR).
func (p *PiTool) ConfigDirName() string { return mustBundle("pi").ConfigDir }
```

Replace lines 92-107 (`EssentialConfigFiles` through `AlwaysSetupConfig`):

```go
// EssentialConfigFiles implements ToolWithConfigDirFiles.
func (p *PiTool) EssentialConfigFiles() []string {
	return mustBundle("pi").Files
}

// SandboxSettingsFileName implements ToolWithConfigDirFiles.
func (p *PiTool) SandboxSettingsFileName() string { return mustBundle("pi").SandboxSettingsFile }

// StateConfigFileName implements ToolWithConfigDirFiles.
// Pi has no sibling state file.
func (p *PiTool) StateConfigFileName() string { return mustBundle("pi").StateFile }

// AlwaysSetupConfig implements ToolWithConfigDirFiles.
// Pi needs config dir setup even without host config dir so that
// settings.json exists for sandbox context injection.
func (p *PiTool) AlwaysSetupConfig() bool { return mustBundle("pi").AlwaysSetup }
```

- [ ] **Step 6: Run the full tool package tests, and the wider suite, to confirm no regressions**

Run: `go test ./internal/tool/... ./internal/session/... ./internal/cli/...`
Expected: PASS (every existing test, including the characterization tests from Step 1, and `internal/session`'s `setup_claude_test.go`/`setup_opencode_test.go`/`setup_pi_test.go` which exercise these tools indirectly, passes unchanged).

- [ ] **Step 7: Commit**

```bash
git add internal/tool/tool.go internal/tool/opencode.go internal/tool/pi.go
git commit -m "$(cat <<'EOF'
Point builtin tools at the credential catalog (task builtin-tool-catalog-wiring)

ClaudeTool/OpencodeTool/PiTool's ToolWithConfigDirFiles methods now look up
their config-dir metadata from internal/tool/credentials instead of
hardcoding it: same values, single source of truth shared with the new
profile-level [[credentials]] feature. Pure refactor: existing tests are
unchanged and pass.

Part of #549.
EOF
)"
```

---

### Task: config-credential-entry - Profile & Config Schema

**Files:**
- Modify: `internal/config/config.go` (add `CredentialEntry`, `Config.Credentials`, `ProfileConfig.Credentials`, `ApplyProfile` merge, `Validate` checks; add `strconv` import)
- Test: `internal/config/credential_entry_test.go`

**Interfaces:**
- Produces: `config.CredentialEntry{Bundle, Host, Container, Mode string; Untrusted bool; SourcePath string}`, `Config.Credentials []CredentialEntry`, `ProfileConfig.Credentials []CredentialEntry`, consumed by task `session-credential-parser`.

- [ ] **Step 1: Write the failing validation tests**

Create `internal/config/credential_entry_test.go`:

```go
package config

import "testing"

func TestProfileConfig_Validate_CredentialsBundleOnly(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{{Bundle: "ollama"}}}
	if err := p.Validate("test"); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestProfileConfig_Validate_CredentialsAdHocOnly(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{
		{Host: "~/.aws/credentials", Container: "/home/code/.aws/credentials"},
	}}
	if err := p.Validate("test"); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestProfileConfig_Validate_CredentialsRejectsBundleAndAdHocTogether(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{
		{Bundle: "ollama", Host: "~/x", Container: "/x"},
	}}
	if err := p.Validate("test"); err == nil {
		t.Fatal("expected error when both bundle and host/container are set")
	}
}

func TestProfileConfig_Validate_CredentialsRejectsNeitherBundleNorAdHoc(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{{}}}
	if err := p.Validate("test"); err == nil {
		t.Fatal("expected error when neither bundle nor host/container is set")
	}
}

func TestProfileConfig_Validate_CredentialsRejectsAdHocMissingContainer(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{{Host: "~/x"}}}
	if err := p.Validate("test"); err == nil {
		t.Fatal("expected error when container path is missing")
	}
}

func TestProfileConfig_Validate_CredentialsRejectsAdHocMissingHost(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{{Container: "/x"}}}
	if err := p.Validate("test"); err == nil {
		t.Fatal("expected error when host path is missing")
	}
}

func TestProfileConfig_Validate_CredentialsRejectsInvalidMode(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{{Bundle: "ollama", Mode: "0999"}}}
	if err := p.Validate("test"); err == nil {
		t.Fatal("expected error for invalid octal mode")
	}
}

func TestProfileConfig_Validate_CredentialsAcceptsValidMode(t *testing.T) {
	p := &ProfileConfig{Credentials: []CredentialEntry{{Bundle: "ollama", Mode: "0600"}}}
	if err := p.Validate("test"); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestApplyProfile_MergesCredentials(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]ProfileConfig{
			"test": {Credentials: []CredentialEntry{{Bundle: "ollama"}}},
		},
	}
	if err := cfg.ApplyProfile("test"); err != nil {
		t.Fatalf("ApplyProfile() error = %v", err)
	}
	if len(cfg.Credentials) != 1 || cfg.Credentials[0].Bundle != "ollama" {
		t.Fatalf("Credentials not merged: %+v", cfg.Credentials)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/... -run Credential -v`
Expected: FAIL with compile errors (`CredentialEntry` undefined, `ProfileConfig.Credentials` undefined, `Config.Credentials` undefined).

- [ ] **Step 3: Add `strconv` to the imports in `internal/config/config.go`**

```go
import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/BurntSushi/toml"
)
```

- [ ] **Step 4: Add the `CredentialEntry` type**

Add directly after the `SocketEntry` type definition (after line 360) in `internal/config/config.go`:

```go
// CredentialEntry represents a single host credential source to copy into a
// container: either a reference to a named catalog bundle (see
// internal/tool/credentials), or an ad-hoc host/container file pair.
// Exactly one of Bundle, or Host+Container, must be set.
type CredentialEntry struct {
	Bundle    string `toml:"bundle"`    // catalog bundle name, e.g. "ollama"
	Host      string `toml:"host"`      // ad-hoc host path (supports ~ expansion)
	Container string `toml:"container"` // ad-hoc container path (must be absolute)
	Mode      string `toml:"mode"`      // optional chmod mode, e.g. "0600"

	// Untrusted/SourcePath are set programmatically (never from TOML) when
	// this entry came from an untrusted, project-scope config file. Only
	// ad-hoc entries (Bundle == "") are ever marked Untrusted, a bundle
	// reference can only select a name from coi's own vetted catalog, not an
	// attacker-chosen host path, so it carries the same trust level the
	// builtin tool credential seeding already has.
	Untrusted  bool   `toml:"-"`
	SourcePath string `toml:"-"`
}
```

- [ ] **Step 5: Add `Config.Credentials` and `ProfileConfig.Credentials` fields**

In the `Config` struct, add after the `Sockets` field (line 34):

```go
	Sockets            []SocketEntry            `toml:"sockets"`
	Credentials        []CredentialEntry        `toml:"credentials"`
```

In the `ProfileConfig` struct, add after the `Sockets` field (line 292):

```go
	Sockets     []SocketEntry     `toml:"sockets"`
	Credentials []CredentialEntry `toml:"credentials"`
```

- [ ] **Step 6: Merge profile credentials in `ApplyProfile`**

In `internal/config/config.go`, add right after the existing `Sockets` merge block (after line ~931, `c.Sockets = append(c.Sockets, profile.Sockets...)`):

```go
	if len(profile.Credentials) > 0 {
		c.Credentials = append(c.Credentials, profile.Credentials...)
	}
```

- [ ] **Step 7: Add validation in `ProfileConfig.Validate`**

In `internal/config/config.go`, add right after the existing mount-entry validation loop (after line ~1400, the closing `}` of the `for i, m := range p.Mounts` loop):

```go
	// Validate credential entries: exactly one of bundle or host+container.
	for i, cr := range p.Credentials {
		hasBundle := cr.Bundle != ""
		hasAdHoc := cr.Host != "" || cr.Container != ""
		if hasBundle && hasAdHoc {
			return fmt.Errorf("profile '%s': credentials[%d] must set either 'bundle' or 'host'+'container', not both", name, i)
		}
		if !hasBundle && !hasAdHoc {
			return fmt.Errorf("profile '%s': credentials[%d] must set either 'bundle' or 'host'+'container'", name, i)
		}
		if hasAdHoc {
			if cr.Host == "" {
				return fmt.Errorf("profile '%s': credentials[%d] is missing 'host' path", name, i)
			}
			if cr.Container == "" {
				return fmt.Errorf("profile '%s': credentials[%d] is missing 'container' path", name, i)
			}
		}
		if cr.Mode != "" {
			if _, err := strconv.ParseUint(cr.Mode, 8, 32); err != nil {
				return fmt.Errorf("profile '%s': credentials[%d] has invalid 'mode' %q (must be an octal file mode, e.g. \"0600\"): %w", name, i, cr.Mode, err)
			}
		}
	}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS (the new tests pass, and every pre-existing `internal/config` test still passes).

- [ ] **Step 9: Commit**

```bash
git add internal/config/config.go internal/config/credential_entry_test.go
git commit -m "$(cat <<'EOF'
Add CredentialEntry config schema (task config-credential-entry)

New [[credentials]] section for Config and ProfileConfig, parallel to
[[mounts]]/[[sockets]]: either bundle = "<catalog name>" or an ad-hoc
host/container pair, with an optional chmod mode. Validated for mutual
exclusivity and octal mode format.

Part of #549.
EOF
)"
```

---

### Task: session-credential-parser - Session Types & Config Parsing

**Files:**
- Modify: `internal/session/types.go` (add `CredentialEntry`, `CredentialConfig`)
- Create: `internal/cli/credential_parser.go`
- Test: `internal/cli/credential_parser_test.go`

**Interfaces:**
- Consumes: `config.CredentialEntry`/`Config.Credentials` from task `config-credential-entry`; `credentials.Lookup`/`credentials.Names` from task `credential-catalog`.
- Produces: `session.CredentialEntry{HostPath, ContainerPath, Mode, BundleName string; Untrusted bool; SourcePath string}`, `session.CredentialConfig{Entries []CredentialEntry}`, `cli.ParseCredentialConfig(cfg *config.Config) (*session.CredentialConfig, error)`, `cli.warnDroppedCredentials(dropped []session.CredentialEntry)`, consumed by task `trust-gating-credentials` and task `setup-credentials-application`.

- [ ] **Step 1: Add the session-layer types**

In `internal/session/types.go`, add after the `SocketConfig` type (end of file):

```go
// CredentialEntry represents a single host file to copy into a container: a
// host path pushed to a container path, chowned to the container's code
// user, and chmod'd to Mode if set. Expanded either from a named catalog
// bundle (see internal/tool/credentials) or an ad-hoc profile entry.
type CredentialEntry struct {
	HostPath string // Absolute path on host (expanded)
	// ContainerPath is either absolute (ad-hoc entries, used as-is, exactly
	// like MountEntry.ContainerPath) or relative to the container's home
	// directory (bundle entries, resolved by setupCredentials at apply
	// time, since the home directory, e.g. /root vs /home/code, isn't known
	// until the container exists).
	ContainerPath string
	Mode          string // Optional chmod mode, e.g. "0600"; "" = leave as pushed

	// BundleName is set when this entry was expanded from a named catalog
	// bundle. Bundle-sourced entries are never gated by trust, see
	// Untrusted below.
	BundleName string

	// Untrusted is true when this entry came from an untrusted (project-scope)
	// config file AND is an ad-hoc entry (BundleName == ""). SourcePath is
	// that file's absolute path. Used to gate ad-hoc credential entries whose
	// source config isn't trusted, behind `coi trust`, mirroring
	// SocketEntry, since a credential file (like a forwarded socket) has no
	// "within workspace" notion that would make it safe to leave ungated.
	Untrusted  bool
	SourcePath string
}

// CredentialConfig holds all credential entries for a session.
type CredentialConfig struct {
	Entries []CredentialEntry
}
```

- [ ] **Step 2: Write the failing parser tests**

Create `internal/cli/credential_parser_test.go`:

```go
package cli

import (
	"path/filepath"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/config"
)

func TestParseCredentialConfig_Empty(t *testing.T) {
	cc, err := ParseCredentialConfig(&config.Config{})
	if err != nil {
		t.Fatalf("ParseCredentialConfig: %v", err)
	}
	if len(cc.Entries) != 0 {
		t.Fatalf("expected no entries, got %d", len(cc.Entries))
	}
}

func TestParseCredentialConfig_BundleExpandsToFilesAndStateFile(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := &config.Config{
		Credentials: []config.CredentialEntry{{Bundle: "claude"}},
	}
	cc, err := ParseCredentialConfig(cfg)
	if err != nil {
		t.Fatalf("ParseCredentialConfig: %v", err)
	}
	// claude bundle: 4 essential files + 1 state file (.claude.json) = 5 entries.
	if len(cc.Entries) != 5 {
		t.Fatalf("expected 5 entries for claude bundle, got %d: %+v", len(cc.Entries), cc.Entries)
	}
	for _, e := range cc.Entries {
		if e.BundleName != "claude" {
			t.Errorf("BundleName = %q, want %q", e.BundleName, "claude")
		}
		if !filepath.IsAbs(e.HostPath) {
			t.Errorf("HostPath not absolute: %q", e.HostPath)
		}
	}
	foundState := false
	for _, e := range cc.Entries {
		if e.ContainerPath == ".claude.json" {
			foundState = true
		}
	}
	if !foundState {
		t.Errorf("expected a .claude.json state-file entry, got %+v", cc.Entries)
	}
}

func TestParseCredentialConfig_BundleAppliesModeAndContainerPath(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := &config.Config{Credentials: []config.CredentialEntry{{Bundle: "ollama"}}}
	cc, err := ParseCredentialConfig(cfg)
	if err != nil {
		t.Fatalf("ParseCredentialConfig: %v", err)
	}
	if len(cc.Entries) != 1 {
		t.Fatalf("expected 1 entry for ollama bundle, got %d", len(cc.Entries))
	}
	e := cc.Entries[0]
	if e.Mode != "0600" {
		t.Errorf("Mode = %q, want %q", e.Mode, "0600")
	}
	wantContainer := filepath.Join(".ollama", "id_ed25519")
	if e.ContainerPath != wantContainer {
		t.Errorf("ContainerPath = %q, want %q", e.ContainerPath, wantContainer)
	}
	if filepath.IsAbs(e.ContainerPath) {
		t.Error("bundle-sourced ContainerPath should be home-relative, not absolute")
	}
	wantHost := filepath.Join("/home/tester", ".ollama", "id_ed25519")
	if e.HostPath != wantHost {
		t.Errorf("HostPath = %q, want %q", e.HostPath, wantHost)
	}
}

func TestParseCredentialConfig_UnknownBundleErrors(t *testing.T) {
	cfg := &config.Config{Credentials: []config.CredentialEntry{{Bundle: "not-a-bundle"}}}
	if _, err := ParseCredentialConfig(cfg); err == nil {
		t.Fatal("expected error for unknown bundle name")
	}
}

func TestParseCredentialConfig_AdHocExpandsAndCarriesTrustFields(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := &config.Config{
		Credentials: []config.CredentialEntry{
			{Host: "~/.aws/credentials", Container: "/home/code/.aws/credentials", Mode: "0600", Untrusted: true, SourcePath: "/ws/.coi/config.toml"},
		},
	}
	cc, err := ParseCredentialConfig(cfg)
	if err != nil {
		t.Fatalf("ParseCredentialConfig: %v", err)
	}
	if len(cc.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(cc.Entries))
	}
	e := cc.Entries[0]
	if e.HostPath != "/home/tester/.aws/credentials" {
		t.Errorf("HostPath = %q, want expanded", e.HostPath)
	}
	if e.ContainerPath != "/home/code/.aws/credentials" {
		t.Errorf("ContainerPath = %q, want %q", e.ContainerPath, "/home/code/.aws/credentials")
	}
	if e.BundleName != "" {
		t.Errorf("BundleName = %q, want empty for ad-hoc entry", e.BundleName)
	}
	if !e.Untrusted || e.SourcePath != "/ws/.coi/config.toml" {
		t.Errorf("trust metadata not carried: %+v", e)
	}
}

func TestParseCredentialConfig_AdHocRejectsRelativeContainerPath(t *testing.T) {
	cfg := &config.Config{
		Credentials: []config.CredentialEntry{{Host: "/abs/host", Container: "relative/path"}},
	}
	if _, err := ParseCredentialConfig(cfg); err == nil {
		t.Fatal("expected error for relative container path")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/cli/... -run ParseCredentialConfig -v`
Expected: FAIL with "undefined: ParseCredentialConfig".

- [ ] **Step 4: Write `internal/cli/credential_parser.go`**

```go
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/mensfeld/code-on-incus/internal/tool/credentials"
)

// warnDroppedCredentials prints a per-entry warning for untrusted, unapproved
// ad-hoc credential entries. Only ad-hoc entries are ever dropped here;
// catalog-referenced entries are never gated (see session.CredentialEntry.Untrusted).
func warnDroppedCredentials(dropped []session.CredentialEntry) {
	for _, c := range dropped {
		fmt.Fprintf(os.Stderr,
			"WARNING: ignoring untrusted credential entry from project config %s:\n"+
				"         host %q -> %q would be copied into the container.\n"+
				"         Run 'coi trust' to approve it (re-approval is required if the\n"+
				"         config later changes), or set %s=1 for CI/automation.\n",
			c.SourcePath, c.HostPath, c.ContainerPath, session.TrustEnvVar)
	}
}

// ParseCredentialConfig creates a CredentialConfig from config file
// [[credentials]] entries. A bundle reference (entry.Bundle != "") expands
// into one session.CredentialEntry per file in the bundle, plus its state
// file if any (rooted at the container home directory rather than under the
// bundle's config dir, matching StateConfigFileName's existing semantics). An
// ad-hoc entry expands into exactly one session.CredentialEntry with an
// absolute container path, used as-is (like ParseMountConfig).
func ParseCredentialConfig(cfg *config.Config) (*session.CredentialConfig, error) {
	cc := &session.CredentialConfig{Entries: []session.CredentialEntry{}}

	for i, entry := range cfg.Credentials {
		if entry.Bundle != "" {
			bundle, ok := credentials.Lookup(entry.Bundle)
			if !ok {
				return nil, fmt.Errorf("credentials[%d]: unknown bundle %q (known bundles: %v)", i, entry.Bundle, credentials.Names())
			}
			for _, filename := range bundle.Files {
				cc.Entries = append(cc.Entries, session.CredentialEntry{
					HostPath:      config.ExpandPath(filepath.Join("~", bundle.ConfigDir, filename)),
					ContainerPath: filepath.Join(bundle.ConfigDir, filename),
					Mode:          bundle.Mode,
					BundleName:    entry.Bundle,
				})
			}
			if bundle.StateFile != "" {
				cc.Entries = append(cc.Entries, session.CredentialEntry{
					HostPath:      config.ExpandPath(filepath.Join("~", bundle.StateFile)),
					ContainerPath: bundle.StateFile,
					Mode:          bundle.Mode,
					BundleName:    entry.Bundle,
				})
			}
			continue
		}

		hostPath := config.ExpandPath(entry.Host)
		absHost, err := filepath.Abs(hostPath)
		if err != nil {
			return nil, fmt.Errorf("invalid credentials[%d] host path '%s': %w", i, entry.Host, err)
		}
		if !filepath.IsAbs(entry.Container) {
			return nil, fmt.Errorf("credentials[%d] container path must be absolute: %s", i, entry.Container)
		}
		cc.Entries = append(cc.Entries, session.CredentialEntry{
			HostPath:      absHost,
			ContainerPath: filepath.Clean(entry.Container),
			Mode:          entry.Mode,
			Untrusted:     entry.Untrusted,
			SourcePath:    entry.SourcePath,
		})
	}

	return cc, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/session/... ./internal/cli/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/session/types.go internal/cli/credential_parser.go internal/cli/credential_parser_test.go
git commit -m "$(cat <<'EOF'
Add session credential types and ParseCredentialConfig (task session-credential-parser)

session.CredentialEntry/CredentialConfig mirror MountEntry/SocketEntry.
ParseCredentialConfig resolves [[credentials]] bundle references against
the catalog (fail-fast on an unknown name) and expands ad-hoc entries the
same way ParseMountConfig does.

Not yet trust-gated or applied at session setup, that's tasks
trust-gating-credentials and setup-credentials-application.

Part of #549.
EOF
)"
```

---

### Task: trust-gating-credentials - Extend Trust Gating to Ad-Hoc Credentials

**Files:**
- Modify: `internal/session/trust.go`
- Modify: `internal/session/trust_test.go` (full replacement)
- Modify: `internal/cli/run.go:576` (mechanical signature update)
- Modify: `internal/cli/trust.go` (`runTrust`/`runUntrust` wiring)

**Interfaces:**
- Consumes: `session.CredentialEntry`/`CredentialConfig` from task `session-credential-parser`; `cli.ParseCredentialConfig` from the same task.
- Produces: `FilterTrusted(mc *MountConfig, sc *SocketConfig, cc *CredentialConfig, workspace string) (*MountConfig, []MountEntry, *SocketConfig, []SocketEntry, *CredentialConfig, []CredentialEntry)`, `TrustSources(mc, sc, cc, workspace) ([]string, error)`, `UntrustedSourcePaths(mc, sc, cc) []string`, consumed by task `setup-credentials-application` and by `internal/cli/trust.go`/`internal/cli/run.go` (already updated in this task).

This task changes the signature of five existing functions in `internal/session/trust.go` (`sourceFingerprint`, `trustedSources`, `FilterTrusted`, `TrustSources`, `UntrustedSourcePaths`), each gaining a `creds`/`cc` parameter. Every existing call site, in `trust.go` itself, `trust_test.go`, `internal/cli/run.go`, and `internal/cli/trust.go`, must be updated in the same commit; the code will not compile otherwise. Because `trust_test.go` calls these functions in ~20 places, Step 2 below is a full-file replacement rather than a line-by-line diff, to avoid an inconsistent partial edit.

- [ ] **Step 1: Add `untrustedCredentials` and extend the five functions in `internal/session/trust.go`**

Add this function directly after `untrustedSockets` (after line 188):

```go
// untrustedCredentials returns the untrusted ad-hoc credential entries, the
// gated set. Like a forwarded socket, a credential entry has no "within
// workspace" notion, so ALL untrusted ad-hoc entries need approval. Entries
// resolved from a named catalog bundle are never marked Untrusted by
// ParseCredentialConfig (their host path is fixed by coi's own catalog, not
// chosen by the referencing config); BundleName == "" is checked here too as
// a second line of defense.
func untrustedCredentials(creds []CredentialEntry) []CredentialEntry {
	var out []CredentialEntry
	for _, c := range creds {
		if c.Untrusted && c.HostPath != "" && c.BundleName == "" {
			out = append(out, c)
		}
	}
	return out
}
```

Replace `sourceFingerprint` (lines 190-206) with:

```go
// sourceFingerprint returns a stable sha256 over the gated mounts, sockets,
// and ad-hoc credential entries a single source declares (order-independent).
// Adding/removing/changing any gated resource changes the hash and re-arms
// the trust prompt; unrelated config edits do not. Lines are type-prefixed
// and %q-quoted so distinct sets cannot collide via embedded separators.
func sourceFingerprint(mounts []MountEntry, sockets []SocketEntry, creds []CredentialEntry) string {
	lines := make([]string, 0, len(mounts)+len(sockets)+len(creds))
	for _, m := range mounts {
		lines = append(lines, fmt.Sprintf("mount:%q|%q|%t", m.HostPath, m.ContainerPath, m.Readonly))
	}
	for _, s := range sockets {
		lines = append(lines, fmt.Sprintf("socket:%q|%q|%q", s.HostPath, s.ContainerPath, s.EnvVar))
	}
	for _, c := range creds {
		lines = append(lines, fmt.Sprintf("credential:%q|%q|%q", c.HostPath, c.ContainerPath, c.Mode))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}
```

Replace `trustedSources` (lines 208-235) with:

```go
// trustedSources computes, per source path, whether the source's gated
// mounts, sockets, and ad-hoc credentials are all approved (combined
// fingerprint matches the store).
func trustedSources(mc *MountConfig, sc *SocketConfig, cc *CredentialConfig, workspace string, store map[string]string) map[string]bool {
	mountsBySrc := map[string][]MountEntry{}
	if mc != nil {
		for _, m := range escapingUntrustedMounts(mc.Mounts, workspace) {
			mountsBySrc[m.SourcePath] = append(mountsBySrc[m.SourcePath], m)
		}
	}
	socketsBySrc := map[string][]SocketEntry{}
	if sc != nil {
		for _, s := range untrustedSockets(sc.Sockets) {
			socketsBySrc[s.SourcePath] = append(socketsBySrc[s.SourcePath], s)
		}
	}
	credsBySrc := map[string][]CredentialEntry{}
	if cc != nil {
		for _, c := range untrustedCredentials(cc.Entries) {
			credsBySrc[c.SourcePath] = append(credsBySrc[c.SourcePath], c)
		}
	}
	srcs := map[string]bool{}
	for src := range mountsBySrc {
		srcs[src] = true
	}
	for src := range socketsBySrc {
		srcs[src] = true
	}
	for src := range credsBySrc {
		srcs[src] = true
	}
	out := map[string]bool{}
	for src := range srcs {
		out[src] = store[src] != "" && store[src] == sourceFingerprint(mountsBySrc[src], socketsBySrc[src], credsBySrc[src])
	}
	return out
}
```

Replace `FilterTrusted` (lines 237-288) with:

```go
// FilterTrusted removes untrusted mounts that escape the workspace, untrusted
// forwarded sockets, and untrusted ad-hoc credential entries, unless their
// source's combined fingerprint is recorded as trusted (or COI_TRUST_ALL is
// set). Returns filtered configs plus the dropped resources (so the caller
// can warn). Trusted-scope and in-workspace mounts are never gated;
// catalog-referenced credential entries (BundleName != "") are never gated.
func FilterTrusted(mc *MountConfig, sc *SocketConfig, cc *CredentialConfig, workspace string) (*MountConfig, []MountEntry, *SocketConfig, []SocketEntry, *CredentialConfig, []CredentialEntry) {
	if TrustAllViaEnv() {
		return mc, nil, sc, nil, cc, nil
	}
	// Cheap exit when there's nothing gated, before loading the store.
	var escMounts []MountEntry
	if mc != nil {
		escMounts = escapingUntrustedMounts(mc.Mounts, workspace)
	}
	var untSockets []SocketEntry
	if sc != nil {
		untSockets = untrustedSockets(sc.Sockets)
	}
	var untCreds []CredentialEntry
	if cc != nil {
		untCreds = untrustedCredentials(cc.Entries)
	}
	if len(escMounts) == 0 && len(untSockets) == 0 && len(untCreds) == 0 {
		return mc, nil, sc, nil, cc, nil
	}

	store, _ := loadTrustStore() // missing/unreadable store → nothing trusted
	trusted := trustedSources(mc, sc, cc, workspace, store)

	keptMC := mc
	var droppedMounts []MountEntry
	if mc != nil {
		keptMC = &MountConfig{Mounts: make([]MountEntry, 0, len(mc.Mounts))}
		for _, m := range mc.Mounts {
			if m.Untrusted && m.HostPath != "" && hostEscapesWorkspace(workspace, m.HostPath) && !trusted[m.SourcePath] {
				droppedMounts = append(droppedMounts, m)
				continue
			}
			keptMC.Mounts = append(keptMC.Mounts, m)
		}
	}

	keptSC := sc
	var droppedSockets []SocketEntry
	if sc != nil {
		keptSC = &SocketConfig{Sockets: make([]SocketEntry, 0, len(sc.Sockets))}
		for _, s := range sc.Sockets {
			if s.Untrusted && s.HostPath != "" && !trusted[s.SourcePath] {
				droppedSockets = append(droppedSockets, s)
				continue
			}
			keptSC.Sockets = append(keptSC.Sockets, s)
		}
	}

	keptCC := cc
	var droppedCreds []CredentialEntry
	if cc != nil {
		keptCC = &CredentialConfig{Entries: make([]CredentialEntry, 0, len(cc.Entries))}
		for _, c := range cc.Entries {
			if c.Untrusted && c.HostPath != "" && c.BundleName == "" && !trusted[c.SourcePath] {
				droppedCreds = append(droppedCreds, c)
				continue
			}
			keptCC.Entries = append(keptCC.Entries, c)
		}
	}

	return keptMC, droppedMounts, keptSC, droppedSockets, keptCC, droppedCreds
}
```

Replace `TrustSources` (lines 290-330) with:

```go
// TrustSources records trust for every source that declares gated mounts,
// sockets, and/or ad-hoc credentials in mc/sc/cc, pinning the current
// combined fingerprint. Returns the set of source paths that were trusted.
func TrustSources(mc *MountConfig, sc *SocketConfig, cc *CredentialConfig, workspace string) ([]string, error) {
	mountsBySrc := map[string][]MountEntry{}
	if mc != nil {
		for _, m := range escapingUntrustedMounts(mc.Mounts, workspace) {
			mountsBySrc[m.SourcePath] = append(mountsBySrc[m.SourcePath], m)
		}
	}
	socketsBySrc := map[string][]SocketEntry{}
	if sc != nil {
		for _, s := range untrustedSockets(sc.Sockets) {
			socketsBySrc[s.SourcePath] = append(socketsBySrc[s.SourcePath], s)
		}
	}
	credsBySrc := map[string][]CredentialEntry{}
	if cc != nil {
		for _, c := range untrustedCredentials(cc.Entries) {
			credsBySrc[c.SourcePath] = append(credsBySrc[c.SourcePath], c)
		}
	}
	srcs := map[string]bool{}
	for src := range mountsBySrc {
		srcs[src] = true
	}
	for src := range socketsBySrc {
		srcs[src] = true
	}
	for src := range credsBySrc {
		srcs[src] = true
	}
	if len(srcs) == 0 {
		return nil, nil
	}
	store, err := loadTrustStore()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(srcs))
	for src := range srcs {
		store[src] = sourceFingerprint(mountsBySrc[src], socketsBySrc[src], credsBySrc[src])
		out = append(out, src)
	}
	sort.Strings(out)
	if err := saveTrustStore(store); err != nil {
		return nil, err
	}
	return out, nil
}
```

Replace `UntrustedSourcePaths` (lines 359-388) with:

```go
// UntrustedSourcePaths returns the distinct source config paths of untrusted
// mounts, sockets, and ad-hoc credential entries, the keys under which trust
// is recorded. `coi untrust` uses this to revoke by the exact stored key
// (resolved at load) rather than reconstructing it, which would diverge on
// symlinked/alias/non-default paths.
func UntrustedSourcePaths(mc *MountConfig, sc *SocketConfig, cc *CredentialConfig) []string {
	seen := map[string]bool{}
	var out []string
	add := func(src string) {
		if src != "" && !seen[src] {
			seen[src] = true
			out = append(out, src)
		}
	}
	if mc != nil {
		for _, m := range mc.Mounts {
			if m.Untrusted {
				add(m.SourcePath)
			}
		}
	}
	if sc != nil {
		for _, s := range sc.Sockets {
			if s.Untrusted {
				add(s.SourcePath)
			}
		}
	}
	if cc != nil {
		for _, c := range cc.Entries {
			if c.Untrusted && c.BundleName == "" {
				add(c.SourcePath)
			}
		}
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 2: Replace `internal/session/trust_test.go` in full**

```go
package session

import (
	"os"
	"path/filepath"
	"testing"
)

func tm(host, container string, ro, untrusted bool, src string) MountEntry {
	return MountEntry{
		HostPath:      host,
		ContainerPath: container,
		Readonly:      ro,
		Untrusted:     untrusted,
		SourcePath:    src,
	}
}

func ts(host, container, env string, untrusted bool, src string) SocketEntry {
	return SocketEntry{
		HostPath:      host,
		ContainerPath: container,
		EnvVar:        env,
		Untrusted:     untrusted,
		SourcePath:    src,
	}
}

func tc(host, container, mode string, untrusted bool, src string) CredentialEntry {
	return CredentialEntry{
		HostPath:      host,
		ContainerPath: container,
		Mode:          mode,
		Untrusted:     untrusted,
		SourcePath:    src,
	}
}

// filterMounts is a thin shim over FilterTrusted for the mounts-only tests.
func filterMounts(mc *MountConfig, ws string) (*MountConfig, []MountEntry) {
	keptMC, dropped, _, _, _, _ := FilterTrusted(mc, nil, nil, ws)
	return keptMC, dropped
}

// filterCreds is a thin shim over FilterTrusted for the credentials-only tests.
func filterCreds(cc *CredentialConfig, ws string) (*CredentialConfig, []CredentialEntry) {
	_, _, _, _, keptCC, dropped := FilterTrusted(nil, nil, cc, ws)
	return keptCC, dropped
}

func TestSourceFingerprint_OrderIndependentAndSensitive(t *testing.T) {
	a := []MountEntry{tm("/h1", "/c1", false, true, "s"), tm("/h2", "/c2", true, true, "s")}
	b := []MountEntry{tm("/h2", "/c2", true, true, "s"), tm("/h1", "/c1", false, true, "s")}
	if sourceFingerprint(a, nil, nil) != sourceFingerprint(b, nil, nil) {
		t.Error("fingerprint should be order-independent")
	}
	roChanged := []MountEntry{tm("/h1", "/c1", true, true, "s"), tm("/h2", "/c2", true, true, "s")}
	if sourceFingerprint(a, nil, nil) == sourceFingerprint(roChanged, nil, nil) {
		t.Error("fingerprint should change when a readonly flag changes")
	}
	removed := []MountEntry{tm("/h1", "/c1", false, true, "s")}
	if sourceFingerprint(a, nil, nil) == sourceFingerprint(removed, nil, nil) {
		t.Error("fingerprint should change when a mount is removed")
	}
}

func TestSourceFingerprint_CoversSockets(t *testing.T) {
	mounts := []MountEntry{tm("/h1", "/c1", false, true, "s")}
	sockA := []SocketEntry{ts("/run/a.sock", "/c/a.sock", "A_SOCK", true, "s")}
	sockB := []SocketEntry{ts("/run/b.sock", "/c/a.sock", "A_SOCK", true, "s")}
	if sourceFingerprint(mounts, nil, nil) == sourceFingerprint(mounts, sockA, nil) {
		t.Error("adding a socket should change the fingerprint")
	}
	if sourceFingerprint(mounts, sockA, nil) == sourceFingerprint(mounts, sockB, nil) {
		t.Error("changing a socket host path should change the fingerprint")
	}
	sockTwoA := []SocketEntry{
		ts("/run/a.sock", "/c/a.sock", "A", true, "s"),
		ts("/run/b.sock", "/c/b.sock", "B", true, "s"),
	}
	sockTwoB := []SocketEntry{
		ts("/run/b.sock", "/c/b.sock", "B", true, "s"),
		ts("/run/a.sock", "/c/a.sock", "A", true, "s"),
	}
	if sourceFingerprint(mounts, sockTwoA, nil) != sourceFingerprint(mounts, sockTwoB, nil) {
		t.Error("fingerprint should be order-independent across sockets")
	}
}

func TestSourceFingerprint_CoversCredentials(t *testing.T) {
	mounts := []MountEntry{tm("/h1", "/c1", false, true, "s")}
	credA := []CredentialEntry{tc("/home/u/.ollama/id_ed25519", ".ollama/id_ed25519", "0600", true, "s")}
	credB := []CredentialEntry{tc("/home/u/.ollama/id_ed25519", ".ollama/id_ed25519", "0644", true, "s")}
	if sourceFingerprint(mounts, nil, nil) == sourceFingerprint(mounts, nil, credA) {
		t.Error("adding a credential entry should change the fingerprint")
	}
	if sourceFingerprint(mounts, nil, credA) == sourceFingerprint(mounts, nil, credB) {
		t.Error("changing a credential entry's mode should change the fingerprint")
	}
}

func TestFilterTrusted_GatesOnlyEscapingUntrusted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")

	mc := &MountConfig{Mounts: []MountEntry{
		tm(filepath.Join(ws, "sub"), "/c-in", false, true, src),
		tm(outside, "/c-out", false, true, src),
		tm(outside, "/c-trusted-scope", false, false, ""),
	}}

	kept, dropped := filterMounts(mc, ws)
	if len(dropped) != 1 || dropped[0].ContainerPath != "/c-out" {
		t.Fatalf("expected only the escaping untrusted mount dropped, got %+v", dropped)
	}
	if len(kept.Mounts) != 2 {
		t.Fatalf("expected 2 kept mounts, got %d", len(kept.Mounts))
	}
}

func TestFilterTrusted_GatesUntrustedSockets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")

	sc := &SocketConfig{Sockets: []SocketEntry{
		ts("/run/host.sock", "/c/host.sock", "BROKER", true, src),
		ts("/run/trusted.sock", "/c/t.sock", "T", false, ""),
	}}

	_, _, keptSC, dropped, _, _ := FilterTrusted(nil, sc, nil, ws)
	if len(dropped) != 1 || dropped[0].EnvVar != "BROKER" {
		t.Fatalf("expected the untrusted socket dropped, got %+v", dropped)
	}
	if len(keptSC.Sockets) != 1 || keptSC.Sockets[0].EnvVar != "T" {
		t.Fatalf("expected only the trusted-scope socket kept, got %+v", keptSC.Sockets)
	}
}

func TestFilterTrusted_GatesUntrustedAdHocCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")

	cc := &CredentialConfig{Entries: []CredentialEntry{
		tc("/home/u/.aws/credentials", "/home/code/.aws/credentials", "0600", true, src),
		tc("/home/u/.gh/token", "/home/code/.gh/token", "", false, ""),
	}}

	kept, dropped := filterCreds(cc, ws)
	if len(dropped) != 1 || dropped[0].ContainerPath != "/home/code/.aws/credentials" {
		t.Fatalf("expected the untrusted ad-hoc credential dropped, got %+v", dropped)
	}
	if len(kept.Entries) != 1 || kept.Entries[0].ContainerPath != "/home/code/.gh/token" {
		t.Fatalf("expected only the trusted-scope credential kept, got %+v", kept.Entries)
	}
}

func TestFilterTrusted_NeverGatesBundleCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")

	// Untrusted=true but BundleName set: a bundle reference from untrusted
	// project config must never be gated, matching the trust level builtin
	// tool credentials already have.
	cc := &CredentialConfig{Entries: []CredentialEntry{
		{HostPath: "/home/u/.ollama/id_ed25519", ContainerPath: ".ollama/id_ed25519", BundleName: "ollama", Untrusted: true, SourcePath: src},
	}}

	kept, dropped := filterCreds(cc, ws)
	if len(dropped) != 0 {
		t.Fatalf("bundle-sourced credential must never be gated, got dropped=%+v", dropped)
	}
	if len(kept.Entries) != 1 {
		t.Fatalf("expected the bundle credential kept, got %+v", kept.Entries)
	}
}

func TestFilterTrusted_TrustAllEnvBypasses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "1")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{
		tm(outside, "/c", false, true, src),
	}}
	sc := &SocketConfig{Sockets: []SocketEntry{
		ts("/run/host.sock", "/c/host.sock", "BROKER", true, src),
	}}
	cc := &CredentialConfig{Entries: []CredentialEntry{
		tc("/home/u/.aws/credentials", "/home/code/.aws/credentials", "0600", true, src),
	}}
	_, droppedM, _, droppedS, _, droppedC := FilterTrusted(mc, sc, cc, ws)
	if len(droppedM) != 0 || len(droppedS) != 0 || len(droppedC) != 0 {
		t.Fatalf("COI_TRUST_ALL=1 should bypass gating, got droppedM=%+v droppedS=%+v droppedC=%+v", droppedM, droppedS, droppedC)
	}
}

func TestTrustThenRevokeOnMountChange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{tm(outside, "/c", false, true, src)}}

	if _, dropped := filterMounts(mc, ws); len(dropped) != 1 {
		t.Fatal("escaping untrusted mount should be gated before trust")
	}

	sources, err := TrustSources(mc, nil, nil, ws)
	if err != nil || len(sources) != 1 || sources[0] != src {
		t.Fatalf("TrustSources: sources=%v err=%v", sources, err)
	}

	if _, dropped := filterMounts(mc, ws); len(dropped) != 0 {
		t.Fatal("mount should be allowed after trust")
	}

	changed := &MountConfig{Mounts: []MountEntry{
		tm(outside, "/c", false, true, src),
		tm(outside, "/c2", false, true, src),
	}}
	if _, dropped := filterMounts(changed, ws); len(dropped) != 2 {
		t.Fatalf("changed mount set should re-arm gating, got dropped=%d", len(dropped))
	}
}

func TestTrust_CombinedMountSocketAndCredentialSource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{tm(outside, "/c", false, true, src)}}
	sc := &SocketConfig{Sockets: []SocketEntry{ts("/run/host.sock", "/c/host.sock", "BROKER", true, src)}}
	cc := &CredentialConfig{Entries: []CredentialEntry{tc("/home/u/.aws/credentials", "/home/code/.aws/credentials", "0600", true, src)}}

	_, dM, _, dS, _, dC := FilterTrusted(mc, sc, cc, ws)
	if len(dM) != 1 || len(dS) != 1 || len(dC) != 1 {
		t.Fatalf("all three should be gated before trust, dM=%d dS=%d dC=%d", len(dM), len(dS), len(dC))
	}

	sources, err := TrustSources(mc, sc, cc, ws)
	if err != nil || len(sources) != 1 {
		t.Fatalf("TrustSources: sources=%v err=%v", sources, err)
	}
	_, dM, _, dS, _, dC = FilterTrusted(mc, sc, cc, ws)
	if len(dM) != 0 || len(dS) != 0 || len(dC) != 0 {
		t.Fatalf("all three should be trusted after approval, dM=%d dS=%d dC=%d", len(dM), len(dS), len(dC))
	}

	// Changing the credential entry alone re-arms the combined fingerprint.
	ccChanged := &CredentialConfig{Entries: []CredentialEntry{tc("/home/u/.aws/credentials", "/home/code/.aws/credentials", "0644", true, src)}}
	_, dM, _, dS, _, dC = FilterTrusted(mc, sc, ccChanged, ws)
	if len(dM) != 1 || len(dS) != 1 || len(dC) != 1 {
		t.Fatalf("changing the credential entry should re-arm gating for the whole source, dM=%d dS=%d dC=%d", len(dM), len(dS), len(dC))
	}
}

func TestUntrustSources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{tm(outside, "/c", false, true, src)}}

	if _, err := TrustSources(mc, nil, nil, ws); err != nil {
		t.Fatal(err)
	}
	if _, dropped := filterMounts(mc, ws); len(dropped) != 0 {
		t.Fatal("should be trusted")
	}

	n, err := UntrustSources([]string{src})
	if err != nil || n != 1 {
		t.Fatalf("UntrustSources n=%d err=%v", n, err)
	}
	if _, dropped := filterMounts(mc, ws); len(dropped) != 1 {
		t.Fatal("should be gated again after untrust")
	}
}

func TestHostEscapesWorkspace_Symlinks(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()

	if err := os.Symlink(outside, filepath.Join(ws, "link")); err != nil {
		t.Fatal(err)
	}
	if !hostEscapesWorkspace(ws, filepath.Join(ws, "link")) {
		t.Error("in-workspace symlink to an outside dir must be detected as escaping")
	}
	if !hostEscapesWorkspace(ws, filepath.Join(ws, "link", "sub")) {
		t.Error("a path through an in-workspace symlink must be escaping")
	}

	if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(ws, "dangling")); err != nil {
		t.Fatal(err)
	}
	if !hostEscapesWorkspace(ws, filepath.Join(ws, "dangling")) {
		t.Error("dangling symlink pointing outside must be escaping")
	}

	realDir := filepath.Join(ws, "realdir")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if hostEscapesWorkspace(ws, realDir) {
		t.Error("a real in-workspace dir must not be escaping")
	}
	if hostEscapesWorkspace(ws, filepath.Join(ws, "notyet")) {
		t.Error("a non-existent in-workspace path must not be escaping")
	}
}

func TestFilterTrusted_SymlinkEscapeGated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ws, "link")); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{tm(filepath.Join(ws, "link"), "/c", false, true, src)}}
	_, dropped := filterMounts(mc, ws)
	if len(dropped) != 1 {
		t.Fatalf("symlink-escaping untrusted mount must be gated, dropped=%d", len(dropped))
	}
}

func TestFilterTrusted_GatesReadonlyEscaping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	mc := &MountConfig{Mounts: []MountEntry{
		tm(outside, "/c", true, true, filepath.Join(ws, ".coi", "config.toml")),
	}}
	if _, dropped := filterMounts(mc, ws); len(dropped) != 1 {
		t.Fatalf("read-only escaping untrusted mount must be gated, dropped=%d", len(dropped))
	}
}

func TestUntrustedSourcePaths(t *testing.T) {
	ws := t.TempDir()
	srcA := filepath.Join(ws, ".coi", "config.toml")
	srcB := filepath.Join(ws, ".coi", "profiles", "dev", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{
		tm("/x", "/cx", false, true, srcA),
		tm("/y", "/cy", false, true, srcA),
		tm("/w", "/cw", false, false, ""),
	}}
	sc := &SocketConfig{Sockets: []SocketEntry{
		ts("/z", "/cz", "Z", true, srcB),
		ts("/t", "/ct", "T", false, ""),
	}}
	got := UntrustedSourcePaths(mc, sc, nil)
	want := []string{srcA, srcB}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("UntrustedSourcePaths = %v, want sorted distinct %v", got, want)
	}
}

func TestUntrustedSourcePaths_IncludesCredentialsButNotBundles(t *testing.T) {
	ws := t.TempDir()
	srcA := filepath.Join(ws, ".coi", "config.toml")
	cc := &CredentialConfig{Entries: []CredentialEntry{
		tc("/x", "/cx", "", true, srcA),
		{HostPath: "/y", ContainerPath: "/cy", BundleName: "ollama", Untrusted: true, SourcePath: srcA}, // bundle -> excluded
	}}
	got := UntrustedSourcePaths(nil, nil, cc)
	if len(got) != 1 || got[0] != srcA {
		t.Fatalf("UntrustedSourcePaths = %v, want [%s]", got, srcA)
	}
}

func TestFilterTrusted_NoEscapingIsNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	mc := &MountConfig{Mounts: []MountEntry{
		tm(filepath.Join(ws, "a"), "/a", false, true, filepath.Join(ws, ".coi", "config.toml")),
		tm("/anywhere", "/b", false, false, ""),
	}}
	kept, dropped := filterMounts(mc, ws)
	if len(dropped) != 0 || len(kept.Mounts) != 2 {
		t.Fatalf("no escaping untrusted mounts should mean no gating; dropped=%d kept=%d",
			len(dropped), len(kept.Mounts))
	}
}
```

- [ ] **Step 3: Update `internal/cli/run.go`'s `gateRunForwarding`**

The run path doesn't pre-filter credentials early (no pre-start ordering need; see task `setup-credentials-application`), so this call site passes `nil` and discards the two new return values. In `internal/cli/run.go`, replace line 576:

```go
	keptMC, droppedM, keptSC, droppedS, _, _ := session.FilterTrusted(mc, sc, nil, workspace)
```

- [ ] **Step 4: Update `internal/cli/trust.go`'s `runTrust` and `runUntrust`**

In `runTrust` (`internal/cli/trust.go`), replace lines 61-82:

```go
	mc, err := ParseMountConfig(a.cfg)
	if err != nil {
		return fmt.Errorf("invalid mount configuration: %w", err)
	}
	sc, err := ParseSocketConfig(a.cfg)
	if err != nil {
		return fmt.Errorf("invalid socket configuration: %w", err)
	}
	cc, err := ParseCredentialConfig(a.cfg)
	if err != nil {
		return fmt.Errorf("invalid credential configuration: %w", err)
	}

	sources, err := session.TrustSources(mc, sc, cc, absWorkspace)
	if err != nil {
		return fmt.Errorf("failed to record trust: %w", err)
	}
	if len(sources) == 0 {
		fmt.Fprintln(os.Stderr,
			"Nothing to trust: no project-config mounts resolve outside the workspace, no sockets "+
				"are forwarded, and no ad-hoc credential entries are configured.")
		return nil
	}
	for _, s := range sources {
		fmt.Fprintf(os.Stderr, "Trusted out-of-workspace mounts, forwarded sockets, and credential entries from %s\n", s)
	}
	return nil
```

In `runUntrust` (`internal/cli/trust.go`), replace lines 117-129:

```go
	mc, err := ParseMountConfig(a.cfg)
	if err != nil {
		return fmt.Errorf("invalid mount configuration: %w", err)
	}
	sc, err := ParseSocketConfig(a.cfg)
	if err != nil {
		return fmt.Errorf("invalid socket configuration: %w", err)
	}
	cc, err := ParseCredentialConfig(a.cfg)
	if err != nil {
		return fmt.Errorf("invalid credential configuration: %w", err)
	}

	// Revoke by the exact stored keys (the loaded SourcePaths), plus the
	// conventional project-config path as a fallback for the case where the
	// config no longer declares the previously-trusted mounts, sockets, or
	// credential entries.
	sources := session.UntrustedSourcePaths(mc, sc, cc)
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/session/... ./internal/cli/... -v`
Expected: PASS (all new and pre-existing tests pass; `go build ./...` succeeds, confirming every call site compiles).

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/session/trust.go internal/session/trust_test.go internal/cli/run.go internal/cli/trust.go
git commit -m "$(cat <<'EOF'
Extend trust gating to ad-hoc credential entries (task trust-gating-credentials)

Ad-hoc [[credentials]] entries from untrusted project-scope config are
gated the same way forwarded sockets already are (no "within workspace"
exemption: a credential file has no legitimate in-workspace case).
Catalog-referenced entries (bundle = "...") are never gated, matching the
trust level builtin tool credential seeding already has, since the host
path is fixed by coi's own catalog rather than chosen by the untrusted
config. `coi trust`/`coi untrust` now cover credentials too.

Part of #549.
EOF
)"
```

---

### Task: setup-credentials-application - Apply Credentials at Session Setup

**Files:**
- Create: `internal/session/setup_credentials.go`
- Test: `internal/session/setup_credentials_integration_test.go`
- Modify: `internal/session/setup.go` (`SetupOptions.CredentialConfig` field, trust-gate block, two `setupCredentials` call sites)
- Modify: `internal/cli/phases_shell.go` (parse `CredentialConfig`, thread into `SetupOptions`)

**Interfaces:**
- Consumes: `session.CredentialEntry`/`CredentialConfig` (task `session-credential-parser`), `FilterTrusted` (task `trust-gating-credentials`), `container.ContainerManager` (`PushFile`, `Chown`, `ExecArgs`, `ExecCommand`), `container.CodeUID`.
- Produces: `setupCredentials(mgr container.ContainerManager, homeDir string, entries []CredentialEntry, logger func(string)) error`, internal to `internal/session`, called only from `setup.go`.

- [ ] **Step 1: Write `internal/session/setup_credentials.go`**

```go
package session

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// setupCredentials copies each configured credential entry (catalog bundle or
// ad-hoc) from host to container, chowns it to the container's code user, and
// chmods it if Mode is set. Tolerant of a missing host file (e.g. the user
// hasn't signed into the referenced provider yet), logs and skips rather
// than failing the whole session. Safe to call again on session resume: each
// entry is independently idempotent (re-push, re-chown, re-chmod).
func setupCredentials(mgr container.ContainerManager, homeDir string, entries []CredentialEntry, logger func(string)) error {
	for _, entry := range entries {
		if _, err := os.Stat(entry.HostPath); err != nil {
			logger(fmt.Sprintf("  - Skipping credential %s (not found on host)", entry.HostPath))
			continue
		}

		dest := entry.ContainerPath
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(homeDir, dest)
		}

		destDir := filepath.Dir(dest)
		mkdirCmd := fmt.Sprintf("mkdir -p %s", destDir)
		if _, err := mgr.ExecCommand(mkdirCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			return fmt.Errorf("failed to create %s: %w", destDir, err)
		}

		logger(fmt.Sprintf("  - Copying credential %s -> %s", entry.HostPath, dest))
		if err := mgr.PushFile(entry.HostPath, dest); err != nil {
			logger(fmt.Sprintf("  - Warning: Failed to copy %s: %v", entry.HostPath, err))
			continue
		}

		if homeDir != "/root" {
			if err := mgr.Chown(dest, container.CodeUID, container.CodeUID); err != nil {
				logger(fmt.Sprintf("  - Warning: Failed to chown %s: %v", dest, err))
			}
		}

		if entry.Mode != "" {
			if err := mgr.ExecArgs([]string{"chmod", entry.Mode, dest}, container.ExecCommandOptions{}); err != nil {
				logger(fmt.Sprintf("  - Warning: Failed to chmod %s to %s: %v", dest, entry.Mode, err))
			}
		}
	}
	return nil
}
```

- [ ] **Step 2: Add `SetupOptions.CredentialConfig` and wire the trust-gate block in `internal/session/setup.go`**

Add the field in `SetupOptions` (`internal/session/setup.go`), right after `SocketConfig` (line 35):

```go
	SocketConfig          *SocketConfig // Forwarded host unix sockets
	CredentialConfig      *CredentialConfig // Configured [[credentials]] entries (catalog + ad-hoc)
```

Replace the existing trust-gate block (lines 322-341):

```go
		// Defense-in-depth: gate untrusted out-of-workspace mounts, untrusted
		// forwarded sockets, AND untrusted ad-hoc credential entries here,
		// where a freshly-launched container is set up, so any caller of
		// session.Setup that didn't pre-filter is still covered.
		// (On container reuse this block is skipped along with the rest of setup;
		// resources persist from creation; the run/shell reuse paths warn that
		// trust changes need a recreate.) Idempotent with the CLI-level gate: on
		// the normal CLI flow these are already filtered, so this drops nothing.
		gatedMC, droppedM, gatedSC, droppedS, gatedCC, droppedC := FilterTrusted(opts.MountConfig, opts.SocketConfig, opts.CredentialConfig, opts.WorkspacePath)
		for _, m := range droppedM {
			opts.Logger(fmt.Sprintf(
				"Warning: ignoring untrusted mount from %s: %s -> %s (resolves outside the workspace; run 'coi trust' or set %s=1)",
				m.SourcePath, m.HostPath, m.ContainerPath, TrustEnvVar))
		}
		for _, s := range droppedS {
			opts.Logger(fmt.Sprintf(
				"Warning: ignoring untrusted socket from %s: %s -> %s (run 'coi trust' or set %s=1)",
				s.SourcePath, s.HostPath, s.ContainerPath, TrustEnvVar))
		}
		for _, c := range droppedC {
			opts.Logger(fmt.Sprintf(
				"Warning: ignoring untrusted credential entry from %s: %s -> %s (run 'coi trust' or set %s=1)",
				c.SourcePath, c.HostPath, c.ContainerPath, TrustEnvVar))
		}
		opts.MountConfig = gatedMC
		opts.SocketConfig = gatedSC
		opts.CredentialConfig = gatedCC
```

- [ ] **Step 3: Call `setupCredentials` on resume**

In `internal/session/setup.go`, right after the existing resume block closes (after line 652, the `}` closing `if opts.ResumeFromID != "" && opts.Tool != nil && opts.Tool.ConfigDirName() != "" {`), add a new independent step (independent of `opts.Tool`, so it applies even for a tool with no config dir):

```go
	// 9.5 Refresh/copy configured [[credentials]] entries (catalog bundles and
	// ad-hoc). Independent of which Tool is selected. Re-run on every resume
	// (idempotent) so a rotated host credential stays in sync; on a fresh
	// session it's applied once at step 11 alongside CLI tool config.
	if opts.ResumeFromID != "" && opts.CredentialConfig != nil && len(opts.CredentialConfig.Entries) > 0 {
		opts.Logger("Refreshing configured credentials...")
		if err := setupCredentials(result.Manager, result.HomeDir, opts.CredentialConfig.Entries, opts.Logger); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Could not refresh credentials: %v", err))
		}
	}
```

- [ ] **Step 4: Call `setupCredentials` on fresh-session setup**

In `internal/session/setup.go`, right after the existing step-11 block closes (after line 696, the `}` closing `if opts.Tool != nil {` for the CLI-config setup), add:

```go
	// 11.5 Setup configured [[credentials]] entries (skip if resuming - the
	// refresh above already handled it; skip on container reuse - persists
	// from creation, matching how step 11 handles the builtin tool config).
	if !skipLaunch && opts.ResumeFromID == "" && opts.CredentialConfig != nil && len(opts.CredentialConfig.Entries) > 0 {
		opts.Logger("Setting up configured credentials...")
		if err := setupCredentials(result.Manager, result.HomeDir, opts.CredentialConfig.Entries, opts.Logger); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to setup credentials: %v", err))
		}
	}
```

- [ ] **Step 5: Parse and thread `CredentialConfig` in `internal/cli/phases_shell.go`**

Right after the existing `socketConfig, err := ParseSocketConfig(a.cfg)` block (after line 314), add:

```go
			credConfig, err := ParseCredentialConfig(a.cfg)
			if err != nil {
				return nil, fmt.Errorf("invalid credential configuration: %w", err)
			}
```

In the `session.SetupOptions{...}` literal (starting line 327), add the field right after `SocketConfig: socketConfig,` (line 334):

```go
				MountConfig:           mountConfig,
				SocketConfig:          socketConfig,
				CredentialConfig:      credConfig,
```

- [ ] **Step 6: Write the integration test**

Create `internal/session/setup_credentials_integration_test.go`:

```go
package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// TestSetupCredentials_Integration pushes an ad-hoc credential file into a
// real container, verifying content, ownership, and chmod. Skipped without a
// local Incus daemon (skipUnlessContextFileTestable, shared with
// context_file_integration_test.go).
func TestSetupCredentials_Integration(t *testing.T) {
	skipUnlessContextFileTestable(t)

	tmpHost := t.TempDir()
	hostFile := filepath.Join(tmpHost, "id_ed25519")
	if err := os.WriteFile(hostFile, []byte("fake-key-material"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := launchContextTestContainer(t, "coi-test-credentials")

	entries := []CredentialEntry{
		{HostPath: hostFile, ContainerPath: filepath.Join(".ollama", "id_ed25519"), Mode: "0600"},
	}
	var logs []string
	logger := func(msg string) { logs = append(logs, msg); t.Logf("[credentials] %s", msg) }

	homeDir := "/home/" + container.CodeUser
	if err := setupCredentials(mgr, homeDir, entries, logger); err != nil {
		t.Fatalf("setupCredentials: %v", err)
	}

	destPath := filepath.Join(homeDir, ".ollama", "id_ed25519")
	out, err := mgr.ExecCommand(fmt.Sprintf("cat %s", destPath), container.ExecCommandOptions{Capture: true})
	if err != nil {
		t.Fatalf("reading pushed file: %v", err)
	}
	if out != "fake-key-material" {
		t.Errorf("pushed file content = %q, want %q", out, "fake-key-material")
	}

	perms, err := mgr.ExecCommand(fmt.Sprintf("stat -c %%a %s", destPath), container.ExecCommandOptions{Capture: true})
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perms != "600" {
		t.Errorf("mode = %q, want %q", perms, "600")
	}

	owner, err := mgr.ExecCommand(fmt.Sprintf("stat -c %%u %s", destPath), container.ExecCommandOptions{Capture: true})
	if err != nil {
		t.Fatalf("stat owner: %v", err)
	}
	wantUID := fmt.Sprintf("%d", container.CodeUID)
	if owner != wantUID {
		t.Errorf("owner uid = %q, want %q", owner, wantUID)
	}
}
```

This test uses `fmt.Sprintf`, so add `"fmt"` to the import block:

```go
import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/container"
)
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/session/... ./internal/cli/... -v`
Expected: PASS for everything except `TestSetupCredentials_Integration`, which SKIPs unless a local Incus daemon and the `coi-default` image are available (matches the existing skip behavior of every other `*_integration_test.go` in this package).

Run: `go build ./...`
Expected: no errors.

If Incus is available locally, additionally run: `go test ./internal/session/... -run TestSetupCredentials_Integration -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/session/setup_credentials.go internal/session/setup_credentials_integration_test.go internal/session/setup.go internal/cli/phases_shell.go
git commit -m "$(cat <<'EOF'
Apply configured credentials at session setup (task setup-credentials-application)

setupCredentials pushes, chowns, and (if Mode is set) chmods each gated
CredentialEntry. Runs on every session resume (idempotent refresh) and once
on fresh-session setup (skipped on persistent-container reuse, matching the
existing CLI-config setup step). Wired into the same trust-gate chokepoint
as mounts/sockets in session.Setup, and parsed alongside them in
phases_shell.go: coi shell/run now seed [[credentials]] end to end.

Closes #549.
EOF
)"
```

---

## Final Verification

After all six tasks are committed:

- [ ] Run the full test suite: `go test ./...`
Expected: PASS (integration tests requiring a local Incus daemon will SKIP if unavailable, as they do today).

- [ ] Run `go vet ./...`
Expected: no issues.

- [ ] Manually sanity-check the new profile syntax parses: create a scratch profile with

```toml
[[credentials]]
bundle = "ollama"

[[credentials]]
host = "~/.some-token"
container = "/home/code/.some-token"
mode = "0600"
```

and run `coi profile show <name>` to confirm it loads and validates without error (this exercises `ProfileConfig.Validate` end to end, including the new credential mutual-exclusivity and mode checks).
