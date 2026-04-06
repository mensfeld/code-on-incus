package health

import (
	"math"
	"testing"
)

func TestParseStorageValueGiB(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		// GiB values (most common)
		{"28.57GiB", 28.57},
		{" 1.35GiB ", 1.35},
		{"0.00GiB", 0},

		// MiB values (fresh pool, the bug trigger)
		{"277.69MiB", 277.69 / 1024},
		{"512.00MiB", 0.5},

		// TiB values (large pools)
		{"2.00TiB", 2048},

		// KiB values (nearly empty)
		{"100.00KiB", 100.0 / (1024 * 1024)},

		// EiB (extreme)
		{"1.00EiB", 1024 * 1024 * 1024},

		// Number only (assume GiB)
		{"42.5", 42.5},

		// Case insensitivity
		{"10.0gib", 10.0},
		{"500.0mib", 500.0 / 1024},
	}

	const (
		absEpsilon = 1e-9
		relEpsilon = 1e-6
	)

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseStorageValueGiB(tt.input)
			tolerance := math.Max(absEpsilon, relEpsilon*math.Abs(tt.want))
			if math.Abs(got-tt.want) > tolerance {
				t.Errorf("parseStorageValueGiB(%q) = %f, want %f", tt.input, got, tt.want)
			}
		})
	}
}
