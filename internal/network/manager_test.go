package network

import (
	"errors"
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/logger"
)

// stubNft records which nftRuler methods are called, in order.
// Set failOn to the method name to make that call return an error.
type stubNft struct {
	calls  []string
	failOn string
}

func (s *stubNft) ApplyAllowlist(_ *config.NetworkConfig, allowedIPs []string) error {
	s.calls = append(s.calls, "ApplyAllowlist")
	if s.failOn == "ApplyAllowlist" {
		return errors.New("stub: ApplyAllowlist failed")
	}
	return nil
}

func (s *stubNft) ApplyRestricted(_ *config.NetworkConfig) error {
	s.calls = append(s.calls, "ApplyRestricted")
	if s.failOn == "ApplyRestricted" {
		return errors.New("stub: ApplyRestricted failed")
	}
	return nil
}

func (s *stubNft) RemoveRules() error {
	s.calls = append(s.calls, "RemoveRules")
	if s.failOn == "RemoveRules" {
		return errors.New("stub: RemoveRules failed")
	}
	return nil
}

func (s *stubNft) ReplaceAllowlist(_ *config.NetworkConfig, _ []string) error {
	s.calls = append(s.calls, "ReplaceAllowlist")
	if s.failOn == "ReplaceAllowlist" {
		return errors.New("stub: ReplaceAllowlist failed")
	}
	return nil
}

func (s *stubNft) called(method string) bool {
	for _, c := range s.calls {
		if c == method {
			return true
		}
	}
	return false
}

// newManagerForRefreshTest builds a Manager wired for refreshAllowedIPs tests.
// Use raw IPv4 strings as "domains" to avoid real DNS lookups: ResolveDomain
// returns them immediately without a network call.
func newManagerForRefreshTest(t *testing.T, stub *stubNft, cachedIPs map[string][]string, domains []string) *Manager {
	t.Helper()
	cache := &IPCache{
		Domains: cachedIPs,
		TTLs:    make(map[string]uint32),
	}
	return &Manager{
		config:       &config.NetworkConfig{AllowedDomains: domains},
		nft:          stub,
		resolver:     NewResolver(cache),
		cacheManager: NewCacheManager(t.TempDir()),
		logger:       logger.NewDiscard(),
	}
}

// TestRefreshAllowedIPs_CallsReplaceAllowlistOnChange verifies that when the
// resolved IPs differ from the cache, refreshAllowedIPs calls ReplaceAllowlist
// and never calls the old Remove+reapply sequence.
func TestRefreshAllowedIPs_CallsReplaceAllowlistOnChange(t *testing.T) {
	stub := &stubNft{}
	// Cache holds "9.9.9.9"; resolving the raw IP "8.8.8.8" returns "8.8.8.8",
	// so IPsUnchanged returns false and the update path runs.
	m := newManagerForRefreshTest(t, stub,
		map[string][]string{"8.8.8.8": {"9.9.9.9"}},
		[]string{"8.8.8.8"},
	)

	if _, err := m.refreshAllowedIPs(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !stub.called("ReplaceAllowlist") {
		t.Errorf("ReplaceAllowlist was not called; calls: %v", stub.calls)
	}
	if stub.called("RemoveRules") {
		t.Errorf("RemoveRules must not be called from refreshAllowedIPs; calls: %v", stub.calls)
	}
	// The old 3-step sequence was: ApplyAllowlist, RemoveRules, ApplyAllowlist.
	// After the fix neither RemoveRules nor a bare ApplyAllowlist should appear.
	for _, c := range stub.calls {
		if c == "ApplyAllowlist" {
			t.Errorf("ApplyAllowlist should not be called directly from refreshAllowedIPs; calls: %v", stub.calls)
			break
		}
	}
}

// TestRefreshAllowedIPs_NoNftCallsWhenUnchanged verifies that when resolved IPs
// match the cache, refreshAllowedIPs makes no nft calls at all.
func TestRefreshAllowedIPs_NoNftCallsWhenUnchanged(t *testing.T) {
	stub := &stubNft{}
	// Cache and resolution both return "8.8.8.8" → IPsUnchanged = true.
	m := newManagerForRefreshTest(t, stub,
		map[string][]string{"8.8.8.8": {"8.8.8.8"}},
		[]string{"8.8.8.8"},
	)

	if _, err := m.refreshAllowedIPs(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stub.calls) != 0 {
		t.Errorf("expected no nft calls when IPs are unchanged, got: %v", stub.calls)
	}
}

// TestRefreshAllowedIPs_PropagatesReplaceAllowlistError verifies that a failure
// in ReplaceAllowlist is surfaced as an error from refreshAllowedIPs.
func TestRefreshAllowedIPs_PropagatesReplaceAllowlistError(t *testing.T) {
	stub := &stubNft{failOn: "ReplaceAllowlist"}
	m := newManagerForRefreshTest(t, stub,
		map[string][]string{"8.8.8.8": {"9.9.9.9"}},
		[]string{"8.8.8.8"},
	)

	_, err := m.refreshAllowedIPs()
	if err == nil {
		t.Fatal("expected an error when ReplaceAllowlist fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to update nft rules") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestRefreshAllowedIPs_ReplaceAllowlistCalledOnce verifies that even when
// multiple domains change, ReplaceAllowlist is called exactly once.
func TestRefreshAllowedIPs_ReplaceAllowlistCalledOnce(t *testing.T) {
	stub := &stubNft{}
	m := newManagerForRefreshTest(t, stub,
		map[string][]string{
			"1.1.1.1": {"9.9.9.9"}, // stale IP for domain "1.1.1.1"
			"8.8.8.8": {"9.9.9.9"}, // stale IP for domain "8.8.8.8"
		},
		[]string{"1.1.1.1", "8.8.8.8"},
	)

	if _, err := m.refreshAllowedIPs(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, c := range stub.calls {
		if c == "ReplaceAllowlist" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected ReplaceAllowlist called exactly once, got %d; calls: %v", count, stub.calls)
	}
}
