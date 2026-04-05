package network

import (
	"os/exec"
	"testing"
)

// FirewallAvailable should return true only when firewall-cmd is installed AND
// firewalld is actually running.
func TestFirewallAvailable_Integration(t *testing.T) {
	_, lookPathErr := exec.LookPath("firewall-cmd")
	installed := lookPathErr == nil

	if !installed {
		t.Log("firewall-cmd not installed — expecting FirewallAvailable() == false")
		if FirewallAvailable() {
			t.Error("FirewallAvailable() returned true but firewall-cmd is not installed")
		}
		return
	}

	// firewall-cmd is installed; check if firewalld is running
	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--state")
	running := cmd.Run() == nil

	result := FirewallAvailable()
	if result != running {
		t.Errorf("FirewallAvailable() = %v, but firewalld running state is %v", result, running)
	}

	t.Logf("FirewallAvailable: installed=%v running=%v result=%v", installed, running, result)
}

// FirewallInstalled should return true when firewall-cmd binary exists on PATH.
func TestFirewallInstalled_Integration(t *testing.T) {
	_, lookPathErr := exec.LookPath("firewall-cmd")
	expected := lookPathErr == nil

	result := FirewallInstalled()
	if result != expected {
		t.Errorf("FirewallInstalled() = %v, but exec.LookPath says %v", result, expected)
	}

	t.Logf("FirewallInstalled: %v", result)
}

// MasqueradeEnabled should return true only when firewalld is running and
// masquerade is configured.
func TestMasqueradeEnabled_Integration(t *testing.T) {
	if !FirewallAvailable() {
		t.Log("firewalld not available — expecting MasqueradeEnabled() == false")
		if MasqueradeEnabled() {
			t.Error("MasqueradeEnabled() returned true but firewalld is not available")
		}
		return
	}

	// Query masquerade directly to compare
	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--query-masquerade")
	expected := cmd.Run() == nil

	result := MasqueradeEnabled()
	if result != expected {
		t.Errorf("MasqueradeEnabled() = %v, but direct query says %v", result, expected)
	}

	t.Logf("MasqueradeEnabled: %v", result)
}

// EnsureBridgeInTrustedZone should:
//  1. Be a no-op when firewalld is not available (no error, no change).
//  2. Be a no-op when the bridge is already in the trusted zone (changed=false).
//  3. Add the bridge to the trusted zone when it isn't, and report changed=true.
//  4. Be idempotent across back-to-back calls.
//
// The test captures the initial zone state and restores it on exit so the CI
// environment is left exactly as it was found.
func TestEnsureBridgeInTrustedZone_Integration(t *testing.T) {
	if !FirewallAvailable() {
		t.Log("firewalld not available — EnsureBridgeInTrustedZone should no-op without error")
		changed, name, err := EnsureBridgeInTrustedZone()
		if err != nil {
			t.Errorf("EnsureBridgeInTrustedZone() returned error when firewalld unavailable: %v", err)
		}
		if changed {
			t.Errorf("EnsureBridgeInTrustedZone() reported changed=true with no firewalld; got name=%q", name)
		}
		return
	}

	// Capture initial bridge zone state so we can restore it on teardown.
	initiallyInZone, bridgeName, err := BridgeInTrustedZone()
	if err != nil {
		t.Skipf("could not determine initial bridge state: %v", err)
	}
	if bridgeName == "" {
		t.Skip("bridge name could not be determined (Incus not set up?)")
	}
	t.Logf("initial state: bridge=%s inZone=%v", bridgeName, initiallyInZone)

	// Ensure we always leave the system in its original state.
	t.Cleanup(func() {
		currentInZone, _, err := BridgeInTrustedZone()
		if err != nil {
			t.Logf("cleanup: could not query current zone state: %v", err)
			return
		}
		if currentInZone == initiallyInZone {
			return
		}
		var args []string
		if initiallyInZone {
			args = []string{"-n", "firewall-cmd", "--zone=trusted", "--add-interface=" + bridgeName, "--permanent"}
		} else {
			args = []string{"-n", "firewall-cmd", "--zone=trusted", "--remove-interface=" + bridgeName, "--permanent"}
		}
		if out, err := exec.Command("sudo", args...).CombinedOutput(); err != nil {
			t.Logf("cleanup: could not restore initial zone state: %v: %s", err, string(out))
		}
		if out, err := exec.Command("sudo", "-n", "firewall-cmd", "--reload").CombinedOutput(); err != nil {
			t.Logf("cleanup: could not reload firewalld: %v: %s", err, string(out))
		}
	})

	// Force a known state: remove the bridge from the trusted zone so we can
	// exercise the "not in zone → add it" branch deterministically.
	if initiallyInZone {
		out, err := exec.Command("sudo", "-n", "firewall-cmd", "--zone=trusted", "--remove-interface="+bridgeName, "--permanent").CombinedOutput()
		if err != nil {
			t.Skipf("could not remove bridge from trusted zone (sudoers?): %v: %s", err, string(out))
		}
		if out, err := exec.Command("sudo", "-n", "firewall-cmd", "--reload").CombinedOutput(); err != nil {
			t.Skipf("could not reload firewalld after removing bridge: %v: %s", err, string(out))
		}
	}

	// Sanity: confirm the bridge really is out of the zone now.
	if stillInZone, _, err := BridgeInTrustedZone(); err != nil {
		t.Fatalf("BridgeInTrustedZone() after remove: %v", err)
	} else if stillInZone {
		t.Skip("could not remove bridge from trusted zone (CI may set it as default zone)")
	}

	// First call: should detect the missing bridge and add it.
	changed, name, err := EnsureBridgeInTrustedZone()
	if err != nil {
		t.Fatalf("EnsureBridgeInTrustedZone() first call: %v", err)
	}
	if !changed {
		t.Errorf("EnsureBridgeInTrustedZone() first call: expected changed=true, got false (bridge=%q)", name)
	}
	if name != bridgeName {
		t.Errorf("EnsureBridgeInTrustedZone() first call: bridge name = %q, want %q", name, bridgeName)
	}

	// Verify the bridge is actually in the zone now.
	if inZone, _, err := BridgeInTrustedZone(); err != nil {
		t.Fatalf("BridgeInTrustedZone() after first EnsureBridgeInTrustedZone: %v", err)
	} else if !inZone {
		t.Errorf("bridge still not in trusted zone after successful EnsureBridgeInTrustedZone")
	}

	// Second call: idempotent — should report changed=false.
	changed, name, err = EnsureBridgeInTrustedZone()
	if err != nil {
		t.Fatalf("EnsureBridgeInTrustedZone() second call: %v", err)
	}
	if changed {
		t.Errorf("EnsureBridgeInTrustedZone() second call: expected changed=false (already in zone), got true")
	}
	if name != bridgeName {
		t.Errorf("EnsureBridgeInTrustedZone() second call: bridge name = %q, want %q", name, bridgeName)
	}
}
