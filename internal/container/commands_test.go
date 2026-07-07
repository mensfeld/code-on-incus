package container

import (
	"os"
	"strings"
	"testing"
)

// Under `go test` stdin is not a terminal (pipe or /dev/null), so the
// pass-through branch is exercised directly; the TTY branch is its logical
// complement (StdinIsTerminal() == true → nil).
func TestStreamedStdin_NonTerminalPassesThrough(t *testing.T) {
	if StdinIsTerminal() {
		t.Skip("test environment has a TTY stdin; cannot exercise the pipe branch")
	}
	if got := streamedStdin(); got != os.Stdin {
		t.Errorf("non-terminal stdin must be attached for piped input, got %v", got)
	}
}

// The invariant the streamed exec relies on: a TTY stdin must NOT be attached
// (nil keeps incus exec non-interactive: no PTY raw mode, Ctrl+C still
// signals coi, stdin-reading commands get EOF). This test pins the mapping
// between the two functions rather than the environment.
func TestStreamedStdin_TerminalMapsToNil(t *testing.T) {
	if StdinIsTerminal() {
		if got := streamedStdin(); got != nil {
			t.Errorf("terminal stdin must not be attached (would enter PTY mode), got %v", got)
		}
	} else {
		if got := streamedStdin(); got == nil {
			t.Error("non-terminal stdin should be attached")
		}
	}
}

// The IPv4-only networkd config (issue #548) must declare the directives that
// keep systemd-networkd from wedging when IPv6 is disabled, and sort before
// netplan's 10-netplan-eth0.network so networkd uses it.
func TestNetworkdIPv4OnlyConfig_HasKeyDirectives(t *testing.T) {
	for _, want := range []string{
		"Name=eth0",
		"DHCP=ipv4",
		"LinkLocalAddressing=no",
		"IPv6AcceptRA=no",
		"RequiredFamilyForOnline=ipv4",
		"UseMTU=true",    // don't drop the DHCP-supplied MTU (netplan would keep it)
		"UseDomains=yes", // don't drop DHCP search domains
	} {
		if !strings.Contains(networkdIPv4OnlyConfig, want) {
			t.Errorf("networkdIPv4OnlyConfig missing %q", want)
		}
	}
}

// The fix depends on this file sorting before netplan's generated
// 10-netplan-eth0.network; guard the load-bearing prefix so a rename can't
// silently revive the #548 hang (the directive test above would stay green).
func TestNetworkdConfigFilename_SortsBeforeNetplan(t *testing.T) {
	if !strings.HasSuffix(networkdConfigFilename, ".network") {
		t.Errorf("networkd config must be a .network file, got %q", networkdConfigFilename)
	}
	// systemd-networkd applies the lexicographically-first matching file; netplan
	// generates 10-netplan-*.network, so ours must sort strictly before "10".
	if networkdConfigFilename >= "10" {
		t.Errorf("networkd config filename %q must sort before netplan's 10-netplan-eth0.network", networkdConfigFilename)
	}
}
