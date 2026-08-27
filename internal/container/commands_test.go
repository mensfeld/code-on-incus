package container

import (
	"errors"
	"fmt"
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

// TestIsIdmapMountUnsupported guards detection of the guest-kernel idmapped-mount
// failure that triggers the #678 shift→raw.idmap fallback. It must match the
// Incus message (case-insensitively) and nothing unrelated.
func TestIsIdmapMountUnsupported(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"exact incus message", errors.New(`Failed to setup device mount "workspace": idmapping abilities are required but aren't supported on system`), true},
		{"wrapped", fmt.Errorf("failed to launch container: %w", errors.New("idmapping abilities are required but aren't supported on system")), true},
		{"uppercase", errors.New("IDMAPPING ABILITIES ARE REQUIRED"), true},
		{"unrelated start failure", errors.New("exit status 1: some other error"), false},
		{"isolation failure", errors.New("security.idmap.isolated: not enough uid/gid available"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isIdmapMountUnsupported(tc.err); got != tc.want {
				t.Errorf("isIdmapMountUnsupported(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestWithDisableShiftHint verifies the #678 hint preserves the underlying error
// (errors.Is via %w) and points the user at the disable_shift escape hatch.
func TestWithDisableShiftHint(t *testing.T) {
	base := errors.New("idmapping abilities are required but aren't supported on system")
	wrapped := withDisableShiftHint(base)
	if !errors.Is(wrapped, base) {
		t.Error("withDisableShiftHint must wrap the original error (errors.Is)")
	}
	if !strings.Contains(wrapped.Error(), "disable_shift = true") {
		t.Errorf("hint must mention the disable_shift workaround, got: %s", wrapped.Error())
	}
}

// TestStartRetryError covers the #716 fix: a start-retry branch adds the
// disable_shift hint for the #678 idmapped-mount class (keyed on the ORIGINAL
// start error) and only then, matching the other retry paths — and returns
// anything else unchanged.
func TestStartRetryError(t *testing.T) {
	idmapErr := errors.New("idmapping abilities are required but aren't supported on system")
	otherErr := errors.New("Permission denied - Failed to mount .incus-systemd-credentials")
	retryErr := errors.New("retry start failed")

	// Successful retry -> nil regardless of the original error.
	if got := startRetryError(idmapErr, nil); got != nil {
		t.Errorf("successful retry should return nil, got %v", got)
	}

	// #678 original error + failed retry -> hinted, wrapping the retry error.
	got := startRetryError(idmapErr, retryErr)
	if !errors.Is(got, retryErr) {
		t.Error("must wrap the retry error (errors.Is)")
	}
	if !strings.Contains(got.Error(), "disable_shift = true") {
		t.Errorf("idmap-class failure must get the disable_shift hint, got: %s", got.Error())
	}

	// Non-idmap original error + failed retry -> returned unchanged (no hint,
	// no misattribution).
	got = startRetryError(otherErr, retryErr)
	if got != retryErr {
		t.Errorf("non-idmap failure must be returned unchanged, got: %v", got)
	}
	if strings.Contains(got.Error(), "disable_shift") {
		t.Errorf("non-idmap failure must NOT get the disable_shift hint, got: %s", got.Error())
	}
}
