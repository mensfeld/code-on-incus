package session

import (
	"context"
	"testing"
)

// fakeNetworkManager records Teardown/SetupForContainer calls so tests can
// assert the session-cleanup orchestration without touching real nft state.
type fakeNetworkManager struct {
	teardownCalls int
	teardownName  string
	setupCalls    int
}

func (f *fakeNetworkManager) SetupForContainer(_ context.Context, name string) error {
	f.setupCalls++
	return nil
}

func (f *fakeNetworkManager) Teardown(_ context.Context, name string) error {
	f.teardownCalls++
	f.teardownName = name
	return nil
}

// Regression test for the intermittent auto-kill nft-rule-cleanup flake: when
// the container has already been removed (e.g. the threat responder auto-killed
// it — the responder stops+deletes the container itself), Cleanup must STILL run
// NetworkManager.Teardown as a backstop, so the per-IP nft rules are not
// orphaned. Previously the "already removed" branch only logged.
//
// The container name does not exist, so mgr.Exists() returns false and the
// already-removed branch runs regardless of whether incus is installed.
func TestCleanup_TeardownRunsWhenContainerAlreadyRemoved(t *testing.T) {
	fake := &fakeNetworkManager{}
	err := Cleanup(CleanupOptions{
		ContainerName:  "coi-nonexistent-backstop-test-0",
		Persistent:     false,
		NetworkManager: fake,
		Logger:         func(string) {},
	})
	if err != nil {
		t.Fatalf("Cleanup returned error: %v", err)
	}
	if fake.teardownCalls != 1 {
		t.Fatalf("expected Teardown to run exactly once on the already-removed path, got %d", fake.teardownCalls)
	}
	if fake.teardownName != "coi-nonexistent-backstop-test-0" {
		t.Errorf("Teardown called with wrong container name: %q", fake.teardownName)
	}
}

// In persistent mode the container is intentionally kept, so the backstop must
// NOT run (no network teardown), even when the container is already gone.
func TestCleanup_NoTeardownBackstopInPersistentMode(t *testing.T) {
	fake := &fakeNetworkManager{}
	err := Cleanup(CleanupOptions{
		ContainerName:  "coi-nonexistent-backstop-test-1",
		Persistent:     true,
		NetworkManager: fake,
		Logger:         func(string) {},
	})
	if err != nil {
		t.Fatalf("Cleanup returned error: %v", err)
	}
	if fake.teardownCalls != 0 {
		t.Errorf("expected no Teardown backstop in persistent mode, got %d calls", fake.teardownCalls)
	}
}
