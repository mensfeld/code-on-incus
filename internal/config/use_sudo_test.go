package config

import "testing"

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
