package session

import (
	"errors"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/config"
)

// fakeTmpfsSizer records how ApplyTmpfsSizing drives the container.
type fakeTmpfsSizer struct {
	explicitSize   string // arg passed to SetTmpfsSize (explicit path)
	ifUnboundedArg string // arg passed to SetTmpfsSizeIfUnbounded (default path)
	applied        bool   // value SetTmpfsSizeIfUnbounded returns
	err            error
}

func (f *fakeTmpfsSizer) SetTmpfsSize(size string) error {
	f.explicitSize = size
	return f.err
}

func (f *fakeTmpfsSizer) SetTmpfsSizeIfUnbounded(size string) (bool, error) {
	f.ifUnboundedArg = size
	return f.applied, f.err
}

func TestApplyTmpfsSizing(t *testing.T) {
	log := func(string) {}

	t.Run("explicit tmpfs_size is applied verbatim, no default probe", func(t *testing.T) {
		f := &fakeTmpfsSizer{}
		cfg := &config.LimitsConfig{}
		cfg.Disk.TmpfsSize = "8GiB"
		ApplyTmpfsSizing(f, cfg, log)
		if f.explicitSize != "8GiB" {
			t.Errorf("SetTmpfsSize arg = %q, want 8GiB", f.explicitSize)
		}
		if f.ifUnboundedArg != "" {
			t.Errorf("default probe must not run when tmpfs_size is explicit, got %q", f.ifUnboundedArg)
		}
	})

	t.Run("unset tmpfs_size falls back to the opportunistic default", func(t *testing.T) {
		f := &fakeTmpfsSizer{applied: true}
		cfg := &config.LimitsConfig{} // TmpfsSize == ""
		ApplyTmpfsSizing(f, cfg, log)
		if f.ifUnboundedArg != defaultTmpfsSize {
			t.Errorf("default probe arg = %q, want %q", f.ifUnboundedArg, defaultTmpfsSize)
		}
		if f.explicitSize != "" {
			t.Errorf("explicit setter must not run on the default path, got %q", f.explicitSize)
		}
	})

	t.Run("nil limits config still applies the default", func(t *testing.T) {
		f := &fakeTmpfsSizer{}
		ApplyTmpfsSizing(f, nil, log)
		if f.ifUnboundedArg != defaultTmpfsSize {
			t.Errorf("nil config should apply default cap, got arg %q", f.ifUnboundedArg)
		}
	})

	t.Run("errors are swallowed (non-fatal)", func(t *testing.T) {
		f := &fakeTmpfsSizer{err: errors.New("boom")}
		cfg := &config.LimitsConfig{}
		cfg.Disk.TmpfsSize = "2GiB"
		ApplyTmpfsSizing(f, cfg, log) // must not panic
	})
}
