package config

import "testing"

// Regression: use_sudo must survive config layering (a trusted $COI_CONFIG /
// user config setting use_sudo=false must override the default). The CI e2e
// caught this when mergeNetworkInto dropped the field.
func TestMerge_PropagatesUseSudo(t *testing.T) {
	fa := false
	base := &Config{} // UseSudo unset (defaults true)
	other := &Config{Network: NetworkConfig{UseSudo: &fa}}
	base.Merge(other)
	if base.Network.SudoAllowed() {
		t.Fatal("Merge dropped use_sudo=false: SudoAllowed() should be false after merge")
	}
}

func TestNetworkConfig_SudoAllowed(t *testing.T) {
	tr := true
	fa := false
	cases := []struct {
		name string
		cfg  *NetworkConfig
		want bool
	}{
		{"nil config defaults true", nil, true},
		{"unset use_sudo defaults true", &NetworkConfig{}, true},
		{"explicit true", &NetworkConfig{UseSudo: &tr}, true},
		{"explicit false", &NetworkConfig{UseSudo: &fa}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cfg.SudoAllowed(); got != c.want {
				t.Errorf("SudoAllowed() = %v, want %v", got, c.want)
			}
		})
	}
}
