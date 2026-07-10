package cli

import "testing"

func TestResolveStatusFilter(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		running bool
		stopped bool
		want    string
		wantErr bool
	}{
		{name: "no filter", want: ""},
		{name: "running shorthand", running: true, want: "Running"},
		{name: "stopped shorthand", stopped: true, want: "Stopped"},
		{name: "status running", status: "running", want: "Running"},
		{name: "status stopped", status: "stopped", want: "Stopped"},
		{name: "status frozen", status: "frozen", want: "Frozen"},
		{name: "status is case-insensitive", status: "RUNNING", want: "Running"},
		{name: "status mixed case", status: "Frozen", want: "Frozen"},
		{name: "invalid status", status: "paused", wantErr: true},
		{name: "running and stopped conflict", running: true, stopped: true, wantErr: true},
		{name: "running and status conflict", running: true, status: "running", wantErr: true},
		{name: "stopped and status conflict", stopped: true, status: "frozen", wantErr: true},
		{name: "all three conflict", running: true, stopped: true, status: "running", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveStatusFilter(tt.status, tt.running, tt.stopped)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveStatusFilter(%q, %v, %v) expected error, got %q",
						tt.status, tt.running, tt.stopped, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveStatusFilter(%q, %v, %v) unexpected error: %v",
					tt.status, tt.running, tt.stopped, err)
			}
			if got != tt.want {
				t.Errorf("resolveStatusFilter(%q, %v, %v) = %q, want %q",
					tt.status, tt.running, tt.stopped, got, tt.want)
			}
		})
	}
}

func TestFilterContainersByStatus(t *testing.T) {
	containers := []ContainerInfo{
		{Name: "coi-aaa-1", Status: "Running"},
		{Name: "coi-bbb-1", Status: "Stopped"},
		{Name: "coi-ccc-1", Status: "Running"},
		{Name: "coi-ddd-1", Status: "Frozen"},
	}

	tests := []struct {
		name   string
		status string
		want   []string
	}{
		{name: "running only", status: "Running", want: []string{"coi-aaa-1", "coi-ccc-1"}},
		{name: "stopped only", status: "Stopped", want: []string{"coi-bbb-1"}},
		{name: "frozen only", status: "Frozen", want: []string{"coi-ddd-1"}},
		{name: "case-insensitive match", status: "running", want: []string{"coi-aaa-1", "coi-ccc-1"}},
		{name: "no matches", status: "Error", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterContainersByStatus(containers, tt.status)
			if len(got) != len(tt.want) {
				t.Fatalf("filterContainersByStatus(%q) returned %d containers, want %d: %+v",
					tt.status, len(got), len(tt.want), got)
			}
			for i, c := range got {
				if c.Name != tt.want[i] {
					t.Errorf("filterContainersByStatus(%q)[%d] = %q, want %q",
						tt.status, i, c.Name, tt.want[i])
				}
			}
		})
	}

	t.Run("empty input stays empty", func(t *testing.T) {
		if got := filterContainersByStatus(nil, "Running"); len(got) != 0 {
			t.Errorf("expected empty result for nil input, got %+v", got)
		}
	})

	t.Run("original slice is not mutated", func(t *testing.T) {
		filterContainersByStatus(containers, "Running")
		if len(containers) != 4 {
			t.Errorf("input slice was mutated: %+v", containers)
		}
	})
}
