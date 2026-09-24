package cli

import (
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/tool"
)

func TestFormatMacKeychainHint(t *testing.T) {
	got := formatMacKeychainHint("claude", ".claude", "Claude Code-credentials", ".credentials.json")

	// The exact command the user must run on the Mac, including the service name
	// and the destination path, must appear verbatim.
	wantCmd := `security find-generic-password -s "Claude Code-credentials" -w > ~/.claude/.credentials.json`
	if !strings.Contains(got, wantCmd) {
		t.Errorf("hint missing the materialize command.\n got:\n%s\n want substring:\n%s", got, wantCmd)
	}
	for _, want := range []string{"claude", "Keychain", "ON YOUR MAC"} {
		if !strings.Contains(got, want) {
			t.Errorf("hint missing %q; got:\n%s", want, got)
		}
	}
}

// The hint is driven by the tool's KeychainCredential(); make sure Claude
// actually advertises the Keychain service + credential filename, or the whole
// hint silently no-ops.
func TestClaudeExposesKeychainCredential(t *testing.T) {
	c, err := tool.Get("claude")
	if err != nil {
		t.Fatalf("tool.Get(claude): %v", err)
	}
	kc, ok := c.(tool.ToolWithKeychainCredential)
	if !ok {
		t.Fatal("ClaudeTool must implement ToolWithKeychainCredential")
	}
	service, filename := kc.KeychainCredential()
	if service != "Claude Code-credentials" {
		t.Errorf("keychain service = %q, want %q", service, "Claude Code-credentials")
	}
	if filename != ".credentials.json" {
		t.Errorf("keychain file = %q, want %q", filename, ".credentials.json")
	}
}

// A tool without a Keychain credential (codex uses a file-based auth.json) must
// not implement the interface, so the hint never fires for it.
func TestCodexHasNoKeychainCredential(t *testing.T) {
	c, err := tool.Get("codex")
	if err != nil {
		t.Fatalf("tool.Get(codex): %v", err)
	}
	if kc, ok := c.(tool.ToolWithKeychainCredential); ok {
		if s, f := kc.KeychainCredential(); s != "" || f != "" {
			t.Errorf("codex should not advertise a keychain credential, got (%q, %q)", s, f)
		}
	}
}
