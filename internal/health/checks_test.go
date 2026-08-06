package health

import (
	"math"
	"strings"
	"testing"
)

func TestParseDefaultGatewayFromProcRoute(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantIP  string
		wantErr bool
	}{
		{
			name: "typical default route",
			input: "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
				"eth0\t00000000\t010080AC\t0003\t0\t0\t100\t00000000\t0\t0\t0\n",
			wantIP: "172.128.0.1",
		},
		{
			name: "default route among others",
			input: "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
				"eth0\t000080AC\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0\n" +
				"eth0\t00000000\t010080AC\t0003\t0\t0\t100\t00000000\t0\t0\t0\n",
			wantIP: "172.128.0.1",
		},
		{
			name: "192.168.x gateway",
			input: "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
				"eth0\t00000000\t0101A8C0\t0003\t0\t0\t0\t00000000\t0\t0\t0\n",
			wantIP: "192.168.1.1",
		},
		{
			name: "multiple default routes picks lowest metric",
			input: "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
				"eth0\t00000000\t010080AC\t0003\t0\t0\t64\t00000000\t0\t0\t0\n" +
				"eth0\t00000000\t0201A8C0\t0003\t0\t0\t0A\t00000000\t0\t0\t0\n",
			wantIP: "192.168.1.2",
		},
		{
			name: "zero gateway skipped",
			input: "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
				"eth0\t00000000\t00000000\t0001\t0\t0\t0\t00000000\t0\t0\t0\n" +
				"eth0\t00000000\t010080AC\t0003\t0\t0\t64\t00000000\t0\t0\t0\n",
			wantIP: "172.128.0.1",
		},
		{
			name: "only zero gateway returns error",
			input: "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
				"eth0\t00000000\t00000000\t0001\t0\t0\t0\t00000000\t0\t0\t0\n",
			wantErr: true,
		},
		{
			name:    "no default route",
			input:   "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n",
			wantErr: true,
		},
		{
			name:    "empty content",
			input:   "",
			wantErr: true,
		},
		{
			name:    "header only no newline",
			input:   "Iface\tDestination\tGateway",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDefaultGatewayFromProcRoute(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got IP %q", got)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.wantIP {
				t.Errorf("got %q, want %q", got, tt.wantIP)
			}
		})
	}
}

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

func TestParsePoolInfo(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantDriver string
		wantUsed   float64
		wantTotal  float64
		wantErr    bool
	}{
		{
			name: "dir pool",
			input: `info:
  description: ""
  driver: dir
  name: default
  space used: 1.61GiB
  total space: 28.57GiB
used by: {}`,
			wantDriver: "dir",
			wantUsed:   1.61,
			wantTotal:  28.57,
		},
		{
			name: "zfs pool",
			input: `info:
  description: ""
  driver: zfs
  name: zfs-pool
  space used: 277.69MiB
  total space: 47.94GiB`,
			wantDriver: "zfs",
			wantUsed:   277.69 / 1024,
			wantTotal:  47.94,
		},
		{
			name:    "unparseable output",
			input:   "some error text",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := parsePoolInfo(tt.input)
			if (u.err != nil) != tt.wantErr {
				t.Fatalf("parsePoolInfo() err = %v, wantErr %v", u.err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if u.driver != tt.wantDriver {
				t.Errorf("driver = %q, want %q", u.driver, tt.wantDriver)
			}
			if math.Abs(u.usedGiB-tt.wantUsed) > 1e-6 {
				t.Errorf("usedGiB = %f, want %f", u.usedGiB, tt.wantUsed)
			}
			if math.Abs(u.totalGiB-tt.wantTotal) > 1e-6 {
				t.Errorf("totalGiB = %f, want %f", u.totalGiB, tt.wantTotal)
			}
		})
	}
}

// A pool on the `dir` driver must be flagged as a warning even when its usage
// is fine: with no unpacked image volume to clone from, every launch re-unpacks
// the full image, dominating session startup (#659). CoW drivers stay OK.
func TestCheckIncusStoragePools_DirDriverWarns(t *testing.T) {
	orig := gatherPoolUsage
	defer func() { gatherPoolUsage = orig }()

	tests := []struct {
		name       string
		usage      poolUsage
		wantStatus CheckStatus
	}{
		{
			name:       "dir driver with healthy usage warns",
			usage:      poolUsage{driver: "dir", usedGiB: 5, totalGiB: 100},
			wantStatus: StatusWarning,
		},
		{
			name:       "dir driver with critical usage stays failed",
			usage:      poolUsage{driver: "dir", usedGiB: 99, totalGiB: 100},
			wantStatus: StatusFailed,
		},
		{
			name:       "zfs driver with healthy usage is ok",
			usage:      poolUsage{driver: "zfs", usedGiB: 5, totalGiB: 100},
			wantStatus: StatusOK,
		},
		{
			name:       "btrfs driver with healthy usage is ok",
			usage:      poolUsage{driver: "btrfs", usedGiB: 5, totalGiB: 100},
			wantStatus: StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gatherPoolUsage = func(pool string) poolUsage { return tt.usage }

			result := CheckIncusStoragePools([]string{"testpool"})
			if result.Status != tt.wantStatus {
				t.Errorf("status = %s, want %s (message: %s)", result.Status, tt.wantStatus, result.Message)
			}

			entry, ok := result.Details["testpool"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected details[testpool] map, got %T", result.Details["testpool"])
			}
			if entry["driver"] != tt.usage.driver {
				t.Errorf("details driver = %v, want %q", entry["driver"], tt.usage.driver)
			}

			// The usage line names the driver so `coi health` text output
			// answers "which driver is this pool on?" at a glance.
			if want := "testpool (" + tt.usage.driver + "):"; !strings.Contains(result.Message, want) {
				t.Errorf("message %q should contain %q", result.Message, want)
			}
		})
	}
}
