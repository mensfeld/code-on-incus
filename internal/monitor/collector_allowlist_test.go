package monitor

import (
	"reflect"
	"testing"
)

// M1: the collector must prefer the live-allowlist provider when it yields
// entries, and fall back to the static snapshot when the provider is nil or
// returns nothing — so it is never worse than the pre-provider behavior.
func TestCollector_CurrentAllowedCIDRs(t *testing.T) {
	static := []string{"1.1.1.1/32"}

	// No provider → static snapshot.
	c := NewCollector("c", "", "/ws", static)
	if got := c.currentAllowedCIDRs(); !reflect.DeepEqual(got, static) {
		t.Errorf("no provider: got %v want %v", got, static)
	}

	// Provider yields entries → live wins.
	live := []string{"2.2.2.2/32", "3.3.3.3/32"}
	c.SetAllowedCIDRsProvider(func() []string { return live })
	if got := c.currentAllowedCIDRs(); !reflect.DeepEqual(got, live) {
		t.Errorf("live provider: got %v want %v", got, live)
	}

	// Provider returns nothing (read failed / empty) → fall back to static.
	c.SetAllowedCIDRsProvider(func() []string { return nil })
	if got := c.currentAllowedCIDRs(); !reflect.DeepEqual(got, static) {
		t.Errorf("empty provider fallback: got %v want %v", got, static)
	}
}
