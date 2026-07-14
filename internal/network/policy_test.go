package network

import (
	"strings"
	"testing"
)

func TestAllowPolicy_Matching(t *testing.T) {
	p, err := NewAllowPolicy([]string{
		"api.anthropic.com",
		"*.googleapis.com",
		"8.8.8.8",
		"10.0.0.0/8",
	})
	if err != nil {
		t.Fatalf("NewAllowPolicy: %v", err)
	}

	tests := []struct {
		name string
		want bool
		why  string
	}{
		// This is the case the old resolve-and-pin path got wrong. It handled
		// "*.googleapis.com" by resolving the *base* domain, googleapis.com,
		// whose addresses have zero overlap with the regional Vertex frontends
		// the container actually dials — so a Claude-on-Vertex session was
		// firewalled off from the very endpoint the user had allowlisted.
		{"us-central1-aiplatform.googleapis.com", true, "wildcard must cover regional API subdomains"},
		{"oauth2.googleapis.com", true, "wildcard must cover sibling subdomains"},
		{"a.b.c.googleapis.com", true, "wildcard must cover subdomains at any depth"},
		{"googleapis.com", true, "a wildcard covers its own base domain"},

		{"api.anthropic.com", true, "exact entry"},
		{"API.Anthropic.COM", true, "matching is case-insensitive"},
		{"api.anthropic.com.", true, "a trailing dot is normalised away"},

		{"evil.com", false, "unlisted domain"},
		{"anthropic.com", false, "an exact entry does not imply its parent"},
		{"sub.api.anthropic.com", false, "an exact entry does not imply its subdomains"},
		// The suffix test must not degrade into a substring test.
		{"notgoogleapis.com", false, "a wildcard must not match a sibling that merely ends in the same string"},
		{"googleapis.com.evil.com", false, "a wildcard must not match when its base is a prefix label"},
	}

	for _, tc := range tests {
		if got := p.Allows(tc.name); got != tc.want {
			t.Errorf("Allows(%q) = %v, want %v — %s", tc.name, got, tc.want, tc.why)
		}
	}
}

func TestAllowPolicy_SplitsLiteralsFromNames(t *testing.T) {
	p, err := NewAllowPolicy([]string{"api.anthropic.com", "*.googleapis.com", "8.8.8.8", "10.0.0.0/8"})
	if err != nil {
		t.Fatalf("NewAllowPolicy: %v", err)
	}

	// Literal addresses go straight to the static set; they need no DNS.
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

	// Names are what the DNS proxy enforces. A wildcard is reduced to its base
	// for prewarming purposes.
	wantNames := map[string]bool{"api.anthropic.com": true, "googleapis.com": true}
	for _, n := range p.Names() {
		if !wantNames[n] {
			t.Errorf("unexpected name entry %q", n)
		}
	}
	if len(p.Names()) != len(wantNames) {
		t.Errorf("Names() = %v, want %v", p.Names(), wantNames)
	}
}

// TestAllowPolicy_RejectsPartialWildcard covers the silent-skip footgun.
//
// "*-aiplatform.googleapis.com" looks like a reasonable way to allow every
// regional Vertex endpoint, and it is exactly what someone reaching for Vertex
// would write. The old resolver did not recognise it as a wildcard, passed it to
// DNS verbatim, watched it fail to resolve, and then dropped it from the
// allowlist without stopping — leaving a firewall that quietly did not cover what
// the config asked for. A config that cannot be honoured must fail loudly.
func TestAllowPolicy_RejectsPartialWildcard(t *testing.T) {
	_, err := NewAllowPolicy([]string{"*-aiplatform.googleapis.com"})
	if err == nil {
		t.Fatal("expected a partial-label wildcard to be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "*.") {
		t.Errorf("the error should show the supported wildcard form, got: %v", err)
	}
}

func TestAllowPolicy_RejectsInvalidEntries(t *testing.T) {
	for _, entry := range []string{
		"*",              // bare wildcard
		"*.",             // wildcard with no base
		"999.1.1.1/8",    // not a valid CIDR
		"2001:db8::/32",  // IPv6 CIDR — the filter table is IPv4-only
		"2001:db8::1",    // IPv6 address
		"foo.*.bar.com",  // wildcard in a middle label
		"*.*.google.com", // multiple wildcards
	} {
		if _, err := NewAllowPolicy([]string{entry}); err == nil {
			t.Errorf("expected %q to be rejected, got nil error", entry)
		}
	}
}

func TestAllowPolicy_EmptyNameIsNeverAllowed(t *testing.T) {
	p, err := NewAllowPolicy([]string{"*.example.com"})
	if err != nil {
		t.Fatalf("NewAllowPolicy: %v", err)
	}
	for _, name := range []string{"", "."} {
		if p.Allows(name) {
			t.Errorf("Allows(%q) = true, want false", name)
		}
	}
}
