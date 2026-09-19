package monitor

import "testing"

// counterDelta must clamp a backwards-going cumulative counter to 0 instead of
// underflowing. A cgroup io counter reads lower than the prior sample when
// CollectResourceStats' io-error branch returns IOReadMB=0; unguarded unsigned
// subtraction would wrap to ~2^64 bytes and spuriously trip large-read
// detection, freezing a healthy container.
func TestCounterDelta(t *testing.T) {
	cases := []struct {
		name              string
		current, previous uint64
		want              uint64
	}{
		{"normal increase", 100, 40, 60},
		{"no change", 100, 100, 0},
		{"backwards (transient io-error zero)", 0, 50 * 1024 * 1024, 0},
		{"backwards (counter reset)", 10, 1_000_000, 0},
		{"from zero baseline", 60, 0, 60},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := counterDelta(tc.current, tc.previous); got != tc.want {
				t.Errorf("counterDelta(%d, %d) = %d, want %d", tc.current, tc.previous, got, tc.want)
			}
		})
	}
}

func TestParseDfBMOutput(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantUsed    float64
		wantTotal   float64
		wantPercent float64
	}{
		{
			name:        "typical tmpfs",
			input:       "Filesystem     1M-blocks  Used Available Use% Mounted on\ntmpfs              2048M  100M     1948M   5% /tmp\n",
			wantUsed:    100,
			wantTotal:   2048,
			wantPercent: 5,
		},
		{
			name:        "full tmpfs",
			input:       "Filesystem     1M-blocks  Used Available Use% Mounted on\ntmpfs               512M  512M        0M 100% /tmp\n",
			wantUsed:    512,
			wantTotal:   512,
			wantPercent: 100,
		},
		{
			name:        "empty output",
			input:       "",
			wantUsed:    0,
			wantTotal:   0,
			wantPercent: 0,
		},
		{
			name:        "header only",
			input:       "Filesystem     1M-blocks  Used Available Use% Mounted on\n",
			wantUsed:    0,
			wantTotal:   0,
			wantPercent: 0,
		},
		{
			name:        "large tmpfs",
			input:       "Filesystem     1M-blocks    Used Available Use% Mounted on\ntmpfs             16384M  8192M     8192M  50% /tmp\n",
			wantUsed:    8192,
			wantTotal:   16384,
			wantPercent: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			used, total, pct := parseDfBMOutput(tt.input)
			if used != tt.wantUsed {
				t.Errorf("used: got %v want %v", used, tt.wantUsed)
			}
			if total != tt.wantTotal {
				t.Errorf("total: got %v want %v", total, tt.wantTotal)
			}
			if pct != tt.wantPercent {
				t.Errorf("percent: got %v want %v", pct, tt.wantPercent)
			}
		})
	}
}
