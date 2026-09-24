package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mensfeld/code-on-incus/internal/tool"
	"github.com/mensfeld/code-on-incus/internal/vmhost"
)

// macKeychainHint returns a user-facing hint (or "") advising how to bring a
// tool's macOS-Keychain-stored login into the container. On macOS coi runs in
// the Linux guest VM and cannot read the Mac login Keychain (#818), so when a
// tool keeps its credential there — and no credential file exists at the
// resolved host config dir for coi to seed — it tells the user the exact
// command to materialize it on the Mac. Returns "" on Linux (KindUnknown), for
// tools without a Keychain credential, or when the credential file is already
// present (it will be seeded, so no hint is needed).
func macKeychainHint(t tool.Tool, cliConfigPath string) string {
	if cliConfigPath == "" || vmhost.Detect() == vmhost.KindUnknown {
		return ""
	}
	kc, ok := t.(tool.ToolWithKeychainCredential)
	if !ok {
		return ""
	}
	service, credFile := kc.KeychainCredential()
	if service == "" || credFile == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(cliConfigPath, credFile)); err == nil {
		return "" // credential file present -> it will be seeded, no hint needed
	}
	return formatMacKeychainHint(t.Name(), t.ConfigDirName(), service, credFile)
}

// formatMacKeychainHint builds the hint message. Pure, so the wording is
// unit-testable without a VM.
func formatMacKeychainHint(toolName, configDirName, service, credFile string) string {
	return fmt.Sprintf(`Note: %s stores its login in the macOS Keychain, which coi (running inside the Linux VM) cannot read — this session may start logged out.
To bring your login across, run this ON YOUR MAC, then start coi again:

  security find-generic-password -s %q -w > ~/%s/%s

The token refreshes periodically; re-run if you get logged out again. See the macOS Setup Guide ("Claude auth on macOS").`,
		toolName, service, configDirName, credFile)
}

// printMacKeychainHint writes the hint (if any) to stderr. Called from the
// shell/run seeding paths once cliConfigPath has been VM-resolved.
func printMacKeychainHint(t tool.Tool, cliConfigPath string) {
	if hint := macKeychainHint(t, cliConfigPath); hint != "" {
		fmt.Fprintln(os.Stderr, "\n"+hint)
	}
}
