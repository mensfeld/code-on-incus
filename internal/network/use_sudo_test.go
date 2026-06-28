package network

import (
	"testing"

	"github.com/mensfeld/code-on-incus/internal/config"
)

// When use_sudo=false, NftUsable must return false WITHOUT invoking sudo, so COI
// behaves as if passwordless sudo were unavailable regardless of the real env
// (this is what makes the no-sudoers path testable on CI runners that do have
// blanket sudo).
func TestNftUsable_FalseWhenSudoDisabled(t *testing.T) {
	no := false
	cfg := &config.NetworkConfig{UseSudo: &no}
	if NftUsable(cfg) {
		t.Error("NftUsable should be false when use_sudo=false")
	}
}
