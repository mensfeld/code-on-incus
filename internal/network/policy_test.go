package network

import (
	"strings"
	"testing"
)

func TestAllowPolicy_SplitsLiteralsFromNames(t *testing.T) {
	p, err := NewAllowPolicy([]string{
		"api.anthropic.com",
		"US-Central1-AIPlatform.GoogleAPIs.com.",
		"8.8.8.8",
		"10.0.0.0/8",
	})
	if err != nil {
		t.Fatalf("NewAllowPolicy: %v", err)
	}

	// Literal addresses go straight to the static set; they involve no resolution.
	wantStatic := map[string]bool{"8.8.8.8/32": true, "10.0.0.0/8": true}
	got := p.StaticCIDRs()
	if len(got) != len(wantStatic) {
		t.Fatalf("StaticCIDRs() = %v, want %v", got, wantStatic)
	}
	for _, c := range got {
		if !wantStatic[c] {
			t.Errorf("unexpected static entry %q", c)
		}
	}

	// Names are resolved on the host; casing and a trailing dot are normalised so
	// the same host is not resolved (and written to /etc/hosts) twice.
	wantNames := map[string]bool{
		"api.anthropic.com":                     true,
		"us-central1-aiplatform.googleapis.com": true,
	}
	for _, n := range p.Names() {
		if !wantNames[n] {
			t.Errorf("unexpected name entry %q", n)
		}
	}
	if len(p.Names()) != len(wantNames) {
		t.Errorf("Names() = %v, want %v", p.Names(), wantNames)
	}
}

// TestAllowPolicy_RejectsWildcards is the regression guard for the bug that
// started all of this.
//
// The old resolver "supported" a wildcard by stripping the "*." and resolving the
// BASE domain — whose addresses have zero overlap with the subdomains actually
// dialled (googleapis.com resolves to 142.250.130.x; the regional Vertex endpoint
// to 172.217.112-119.4). The user got a firewall that permitted a set of
// addresses nothing would ever connect to, and no indication anything was wrong.
//
// Allowlist mode now resolves each name up front and writes the answer into the
// container's /etc/hosts, which — with DNS blocked — is the container's only
// source of addresses. A wildcard has no answer to write: you cannot know which
// subdomains will be asked for. So it is rejected, and the error says what to
// write instead.
func TestAllowPolicy_RejectsWildcards(t *testing.T) {
	for _, entry := range []string{
		"*.googleapis.com",
		"*-aiplatform.googleapis.com", // looks plausible, was silently dropped before
		"*",
		"*.",
		"foo.*.bar.com",
	} {
		_, err := NewAllowPolicy([]string{entry})
		if err == nil {
			t.Errorf("expected wildcard %q to be rejected, got nil error", entry)
			continue
		}
		// The error has to be actionable, not merely correct.
		if !strings.Contains(err.Error(), "exact hostnames") || !strings.Contains(err.Error(), "CIDR") {
			t.Errorf("the error for %q should tell the user to list exact hostnames or a CIDR, got: %v", entry, err)
		}
	}
}

func TestAllowPolicy_RejectsInvalidEntries(t *testing.T) {
	for _, entry := range []string{
		"999.1.1.1/8",   // not a valid CIDR
		"2001:db8::/32", // IPv6 CIDR — the filter table is IPv4-only
		"2001:db8::1",   // IPv6 address
	} {
		if _, err := NewAllowPolicy([]string{entry}); err == nil {
			t.Errorf("expected %q to be rejected, got nil error", entry)
		}
	}
}

func TestAllowPolicy_EmptyEntriesAreSkipped(t *testing.T) {
	p, err := NewAllowPolicy([]string{"", "   ", "api.anthropic.com"})
	if err != nil {
		t.Fatalf("NewAllowPolicy: %v", err)
	}
	if len(p.Names()) != 1 || p.Names()[0] != "api.anthropic.com" {
		t.Errorf("Names() = %v, want just api.anthropic.com", p.Names())
	}
}
