package monitor

import "testing"

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
