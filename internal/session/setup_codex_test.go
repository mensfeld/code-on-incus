package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/tool"
)

// fakeCodexSetupManager is an in-memory ContainerManager covering exactly what
// setupCLIConfig/injectCredentials touch for a tool WITHOUT a sandbox settings
// file or sibling state file: mkdir (ExecCommand and ExecArgs forms), PushFile,
// CreateFile, Chown, chown -R, and the `cat` read used on resume. Every other
// method is inherited from the embedded (nil) interface, so an unexpected call
// panics loudly instead of passing silently.
type fakeCodexSetupManager struct {
	container.ContainerManager
	files        map[string]string
	createdFiles []string // paths written via CreateFile (settings injection writes)
}

func newFakeCodexSetupManager() *fakeCodexSetupManager {
	return &fakeCodexSetupManager{files: map[string]string{}}
}

func (f *fakeCodexSetupManager) ExecCommand(cmd string, _ container.ExecCommandOptions) (string, error) {
	switch {
	case strings.HasPrefix(cmd, "mkdir -p"), strings.HasPrefix(cmd, "chown -R"):
		return "", nil
	case strings.HasPrefix(cmd, "cat "):
		return f.files[strings.Fields(cmd)[1]], nil
	default:
		return "", nil
	}
}

func (f *fakeCodexSetupManager) ExecArgs(args []string, _ container.ExecCommandOptions) error {
	// mkdir -p / chmod from seedHostFile
	return nil
}

func (f *fakeCodexSetupManager) PushFile(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	f.files[dest] = string(data)
	return nil
}

func (f *fakeCodexSetupManager) CreateFile(path, content string) error {
	f.files[path] = content
	f.createdFiles = append(f.createdFiles, path)
	return nil
}

func (f *fakeCodexSetupManager) Chown(_ string, _, _ int) error { return nil }

func codexToolWithConfigDirFiles(t *testing.T) tool.ToolWithConfigDirFiles {
	t.Helper()
	c, err := tool.Get("codex")
	if err != nil {
		t.Fatalf("Get(codex): %v", err)
	}
	tcf, ok := c.(tool.ToolWithConfigDirFiles)
	if !ok {
		t.Fatal("CodexTool must implement ToolWithConfigDirFiles")
	}
	return tcf
}

// TestSetupCLIConfig_Codex_SeedsFilesVerbatim verifies the codex seeding
// contract: auth.json, config.toml, and AGENTS.md are copied from the host
// byte-identical — in particular config.toml is TOML and must NEVER go through
// the JSON settings merge (codex has no sandbox_settings_file precisely so the
// JSON-only merge machinery cannot touch it).
func TestSetupCLIConfig_Codex_SeedsFilesVerbatim(t *testing.T) {
	hostDir := t.TempDir()
	const tomlContent = "# user config\nmodel = \"gpt-5-codex\"\n[sandbox_workspace_write]\nnetwork_access = true\n"
	const authContent = `{"tokens": {"access_token": "secret"}}`
	const agentsContent = "# My global codex instructions\n"
	for name, content := range map[string]string{
		"config.toml": tomlContent,
		"auth.json":   authContent,
		"AGENTS.md":   agentsContent,
	} {
		if err := os.WriteFile(filepath.Join(hostDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	mgr := newFakeCodexSetupManager()
	if err := setupCLIConfig(mgr, hostDir, "/home/code", codexToolWithConfigDirFiles(t), func(string) {}); err != nil {
		t.Fatalf("setupCLIConfig failed: %v", err)
	}

	for name, want := range map[string]string{
		"config.toml": tomlContent,
		"auth.json":   authContent,
		"AGENTS.md":   agentsContent,
	} {
		got, ok := mgr.files["/home/code/.codex/"+name]
		if !ok {
			t.Errorf("%s was not seeded into /home/code/.codex/", name)
			continue
		}
		if got != want {
			t.Errorf("%s was modified during seeding:\n got: %q\nwant: %q", name, got, want)
		}
	}

	// No settings injection may happen: codex has no JSON settings file, so no
	// CreateFile writes (which is how merged settings land) are expected at all.
	if len(mgr.createdFiles) != 0 {
		t.Errorf("setupCLIConfig wrote synthesized files %v; codex must seed host files verbatim only", mgr.createdFiles)
	}
	// And nothing may appear outside ~/.codex (e.g. a sibling state file like
	// ~/.claude.json — codex has none).
	for path := range mgr.files {
		if !strings.HasPrefix(path, "/home/code/.codex/") {
			t.Errorf("unexpected file created outside ~/.codex: %s", path)
		}
	}
}

// TestSetupCLIConfig_Codex_MissingHostFilesSkipped covers the keyring case: the
// host stores codex credentials in the OS keyring, so ~/.codex exists but has
// no auth.json. Seeding must skip missing files without failing the session.
func TestSetupCLIConfig_Codex_MissingHostFilesSkipped(t *testing.T) {
	hostDir := t.TempDir() // empty ~/.codex — no auth.json, config.toml, or AGENTS.md

	mgr := newFakeCodexSetupManager()
	var logs []string
	logger := func(msg string) { logs = append(logs, msg) }

	if err := setupCLIConfig(mgr, hostDir, "/home/code", codexToolWithConfigDirFiles(t), logger); err != nil {
		t.Fatalf("setupCLIConfig must tolerate missing host files, got: %v", err)
	}
	if len(mgr.files) != 0 {
		t.Errorf("no host files existed, but container files were created: %v", mgr.files)
	}
	skips := 0
	for _, msg := range logs {
		if strings.Contains(msg, "Skipping") {
			skips++
		}
	}
	if skips != 3 {
		t.Errorf("expected 3 skip logs (auth.json, config.toml, AGENTS.md), got %d in %q", skips, logs)
	}
}

// TestInjectCredentials_Codex_RefreshesWithoutSettingsWrite verifies the resume
// path: host credential files are re-pushed (fresh auth) and, because codex has
// no sandbox settings and no state file, no merged-settings writes occur.
func TestInjectCredentials_Codex_RefreshesWithoutSettingsWrite(t *testing.T) {
	hostDir := t.TempDir()
	const refreshedAuth = `{"tokens": {"access_token": "rotated"}}`
	if err := os.WriteFile(filepath.Join(hostDir, "auth.json"), []byte(refreshedAuth), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := newFakeCodexSetupManager()
	// Simulate in-container state from the previous session.
	mgr.files["/home/code/.codex/auth.json"] = `{"tokens": {"access_token": "stale"}}`

	if err := injectCredentials(mgr, hostDir, "/home/code", codexToolWithConfigDirFiles(t), func(string) {}); err != nil {
		t.Fatalf("injectCredentials failed: %v", err)
	}

	if got := mgr.files["/home/code/.codex/auth.json"]; got != refreshedAuth {
		t.Errorf("auth.json not refreshed on resume: %q", got)
	}
	if len(mgr.createdFiles) != 0 {
		t.Errorf("injectCredentials wrote synthesized files %v; codex resume must only re-push host files", mgr.createdFiles)
	}
}
