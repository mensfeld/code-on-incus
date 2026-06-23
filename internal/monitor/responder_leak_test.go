package monitor

import (
	"fmt"
	"io"
	"os"
	"testing"
)

// Regression tests for the issue #372 class: the responder's non-fatal cleanup
// warnings on the auto-kill path must be routed to the onError callback (which
// the daemons wire to the session logger), never written to os.Stderr — which
// during a `coi shell` is the user's attached terminal, where it corrupts the
// AI tool's TUI.

func TestResponder_ReportErrorRoutesToCallbackNotStderr(t *testing.T) {
	old := os.Stderr
	rp, wp, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = wp
	defer func() { os.Stderr = old }()

	r := NewResponder("c", false, false, nil, nil)
	var got []error
	r.SetOnError(func(e error) { got = append(got, e) })
	r.reportError(fmt.Errorf("nft cleanup failed: boom"))

	_ = wp.Close()
	os.Stderr = old
	data, _ := io.ReadAll(rp)

	if len(data) != 0 {
		t.Errorf("cleanup warning leaked to stderr: %q", data)
	}
	if len(got) != 1 || got[0] == nil {
		t.Fatalf("expected exactly one error delivered via callback, got %v", got)
	}
}

// Without a callback set, reportError must be silent (no panic, no stderr):
// best-effort cleanup warnings must never fall back to a terminal sink.
func TestResponder_ReportErrorSilentWithoutCallback(t *testing.T) {
	old := os.Stderr
	rp, wp, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = wp
	defer func() { os.Stderr = old }()

	r := NewResponder("c", false, false, nil, nil)
	r.reportError(fmt.Errorf("should vanish")) // must not panic

	_ = wp.Close()
	os.Stderr = old
	data, _ := io.ReadAll(rp)
	if len(data) != 0 {
		t.Errorf("reportError wrote to stderr without a callback set: %q", data)
	}
}
