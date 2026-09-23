//go:build integration

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/tool"
	"github.com/mensfeld/code-on-incus/internal/vmhost"
)

// TestMacHomeConfigSeeding_Integration reproduces the macOS/Colima credential
// gap and its Fix A resolution end-to-end: on a Mac VM the guest home (e.g.
// /home/lima) has no ~/.claude, while the Mac user's real config is visible
// under a shared /Users mount. vmhost.ResolveHostConfigDir must pick the shared
// path, and seeding from it must land the credentials inside a real container —
// so `coi shell` boots an authenticated tool instead of a fresh auth flow.
//
// The VM-detection + /proc/mounts scan of HostToolConfigDir can't be driven
// from a test (the temp "Mac home" isn't a real virtiofs mount under /Users),
// so we exercise the pure resolver against a real filesystem and then feed its
// choice through the same setupCLIConfig path production uses.
func TestMacHomeConfigSeeding_Integration(t *testing.T) {
	skipUnlessContextFileTestable(t)

	// Guest home: exists but has an EMPTY .claude (fresh VM) — must NOT win.
	guestHome := t.TempDir()
	guestClaude := filepath.Join(guestHome, ".claude")
	if err := os.MkdirAll(guestClaude, 0o755); err != nil {
		t.Fatal(err)
	}

	// Shared Mac home: the user's real, populated config.
	macHome := t.TempDir()
	macClaude := filepath.Join(macHome, ".claude")
	if err := os.MkdirAll(macClaude, 0o755); err != nil {
		t.Fatal(err)
	}
	const creds = `{"claudeAiOauth":{"accessToken":"fake-mac-keychain-token"}}`
	if err := os.WriteFile(filepath.Join(macClaude, ".credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
	// A sibling ~/.claude.json (onboarding state) lives next to the dir; seeding
	// resolves it relative to the chosen config dir's parent.
	const stateJSON = `{"hasCompletedOnboarding":true}`
	if err := os.WriteFile(filepath.Join(macHome, ".claude.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// Selection: empty guest .claude must lose to the populated shared Mac home.
	nonEmpty := func(p string) bool {
		entries, err := os.ReadDir(p)
		return err == nil && len(entries) > 0
	}
	resolved := vmhost.ResolveHostConfigDir(guestClaude, []string{macClaude}, nonEmpty)
	if resolved != macClaude {
		t.Fatalf("ResolveHostConfigDir picked %q, want shared Mac home %q", resolved, macClaude)
	}

	// Seed from the resolved path into a real container, exactly as the shell/run
	// pipelines do once cliConfigPath has been VM-resolved.
	claude, err := tool.Get("claude")
	if err != nil {
		t.Fatalf("Get(claude): %v", err)
	}
	tcf, ok := claude.(tool.ToolWithConfigDirFiles)
	if !ok {
		t.Fatal("ClaudeTool must implement ToolWithConfigDirFiles")
	}

	mgr := launchContextTestContainer(t, "coi-test-machome-seed")
	homeDir := "/home/" + container.CodeUser
	logger := func(msg string) { t.Logf("[machome] %s", msg) }

	if err := setupCLIConfig(mgr, resolved, homeDir, tcf, logger); err != nil {
		t.Fatalf("setupCLIConfig: %v", err)
	}

	// Credentials landed inside the container from the shared Mac home.
	credPath := filepath.Join(homeDir, ".claude", ".credentials.json")
	gotCreds, err := mgr.ExecCommand("cat "+credPath, container.ExecCommandOptions{Capture: true})
	if err != nil {
		t.Fatalf("reading seeded credentials: %v", err)
	}
	if gotCreds != creds {
		t.Errorf("seeded credentials = %q, want %q", gotCreds, creds)
	}

	// The sibling state file (~/.claude.json) was seeded too — this is what
	// suppresses the theme/onboarding prompt.
	statePath := filepath.Join(homeDir, ".claude.json")
	gotState, err := mgr.ExecCommand(fmt.Sprintf("cat %s 2>/dev/null || true", statePath), container.ExecCommandOptions{Capture: true})
	if err != nil {
		t.Fatalf("reading seeded state file: %v", err)
	}
	if gotState == "" {
		t.Errorf("state file %s was not seeded from the shared Mac home", statePath)
	}
}
