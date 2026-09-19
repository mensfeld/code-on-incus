package health

import (
	"strings"
	"testing"
)

func TestEvaluateKernelMitigations(t *testing.T) {
	// A normal hardened command line is OK.
	clean := "BOOT_IMAGE=/boot/vmlinuz-6.8.0-45-generic root=UUID=abc ro quiet splash"
	if c := evaluateKernelMitigations(clean); c.Status != StatusOK {
		t.Errorf("clean cmdline should be OK, got %s: %s", c.Status, c.Message)
	}
	// Empty input degrades to OK.
	if c := evaluateKernelMitigations(""); c.Status != StatusOK {
		t.Errorf("empty cmdline should be OK, got %s", c.Status)
	}

	// The wholesale switch warns and is named in the message.
	c := evaluateKernelMitigations("BOOT_IMAGE=/boot/vmlinuz root=/dev/sda1 mitigations=off quiet")
	if c.Status != StatusWarning {
		t.Fatalf("mitigations=off should warn, got %s: %s", c.Status, c.Message)
	}
	if !strings.Contains(c.Message, "mitigations=off") {
		t.Errorf("message should name the flag, got %q", c.Message)
	}

	// Per-vulnerability disables warn too.
	for _, tok := range []string{"nopti", "nospectre_v2", "l1tf=off", "mds=off", "nosmap"} {
		if c := evaluateKernelMitigations("root=/dev/sda1 " + tok + " ro"); c.Status != StatusWarning {
			t.Errorf("%s should warn, got %s", tok, c.Status)
		}
	}

	// Multiple flags are all collected in details.
	c = evaluateKernelMitigations("mitigations=off nopti nospectre_v2")
	flags, ok := c.Details["flags"].([]string)
	if !ok || len(flags) != 3 {
		t.Errorf("expected 3 collected flags, got %v", c.Details["flags"])
	}

	// Exact-token matching: substrings and similar-looking values must NOT match.
	for _, cmdline := range []string{
		"root=/dev/sda1 mitigations=auto",            // explicit auto is fine
		"root=/dev/sda1 mitigations=auto,nosmt",      // auto,nosmt strengthens
		"root=/dev/sda1 somemodule.nopti_like=1",     // substring lookalike
		"root=/dev/sda1 spectre_v2=retpoline",        // an ENABLED mitigation choice
		"root=/dev/sda1 l1tf=full",                   // stronger, not off
		"root=/dev/sda1 crashkernel=mitigations=off", // not its own token... (single field, no match)
	} {
		if c := evaluateKernelMitigations(cmdline); c.Status != StatusOK {
			t.Errorf("%q should not warn, got %s: %s", cmdline, c.Status, c.Message)
		}
	}
}
