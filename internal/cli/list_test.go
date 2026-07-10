package cli

import "testing"

func TestResolveStatusFilter(t *testing.T) {
	tests := []struct {
		name      string
		statusSet bool
		status    string
		running   bool
		stopped   bool
		want      string
		wantErr   bool
	}{
		{name: "no filter", want: ""},
		{name: "running shorthand", running: true, want: "running"},
		{name: "stopped shorthand", stopped: true, want: "stopped"},
		{name: "status running", statusSet: true, status: "running", want: "running"},
		{name: "status stopped", statusSet: true, status: "stopped", want: "stopped"},
		{name: "status frozen", statusSet: true, status: "frozen", want: "frozen"},
		{name: "status error", statusSet: true, status: "error", want: "error"},
		{name: "status stopping", statusSet: true, status: "stopping", want: "stopping"},
		{name: "status is case-insensitive", statusSet: true, status: "RUNNING", want: "running"},
		{name: "status mixed case", statusSet: true, status: "Frozen", want: "frozen"},
		{name: "invalid status", statusSet: true, status: "paused", wantErr: true},
		{name: "explicitly empty status", statusSet: true, status: "", wantErr: true},
		{name: "running and stopped conflict", running: true, stopped: true, wantErr: true},
		{name: "running and status conflict", running: true, statusSet: true, status: "running", wantErr: true},
		{name: "running and empty status conflict", running: true, statusSet: true, status: "", wantErr: true},
		{name: "stopped and status conflict", stopped: true, statusSet: true, status: "frozen", wantErr: true},
		{name: "all three conflict", running: true, stopped: true, statusSet: true, status: "running", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveStatusFilter(tt.statusSet, tt.status, tt.running, tt.stopped)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveStatusFilter(%v, %q, %v, %v) expected error, got %q",
						tt.statusSet, tt.status, tt.running, tt.stopped, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveStatusFilter(%v, %q, %v, %v) unexpected error: %v",
					tt.statusSet, tt.status, tt.running, tt.stopped, err)
			}
			if got != tt.want {
				t.Errorf("resolveStatusFilter(%v, %q, %v, %v) = %q, want %q",
					tt.statusSet, tt.status, tt.running, tt.stopped, got, tt.want)
			}
		})
	}
}

func TestResolveStatusFilterAcceptsEveryValidValue(t *testing.T) {
	for _, status := range validStatusFilters {
		got, err := resolveStatusFilter(true, status, false, false)
		if err != nil {
			t.Errorf("resolveStatusFilter rejected valid status %q: %v", status, err)
		}
		if got != status {
			t.Errorf("resolveStatusFilter(%q) = %q, want %q", status, got, status)
		}
	}
}

func TestFilterContainersByStatus(t *testing.T) {
	newContainers := func() []ContainerInfo {
		return []ContainerInfo{
			{Name: "coi-aaa-1", Status: "Running"},
			{Name: "coi-bbb-1", Status: "Stopped"},
			{Name: "coi-ccc-1", Status: "Running"},
			{Name: "coi-ddd-1", Status: "Frozen"},
			{Name: "coi-eee-1", Status: "Error"},
		}
	}

	tests := []struct {
		name   string
		status string
		want   []string
	}{
		{name: "running only", status: "running", want: []string{"coi-aaa-1", "coi-ccc-1"}},
		{name: "stopped only", status: "stopped", want: []string{"coi-bbb-1"}},
		{name: "frozen only", status: "frozen", want: []string{"coi-ddd-1"}},
		{name: "error only", status: "error", want: []string{"coi-eee-1"}},
		{name: "case-insensitive match", status: "RUNNING", want: []string{"coi-aaa-1", "coi-ccc-1"}},
		{name: "no matches", status: "starting", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterContainersByStatus(newContainers(), tt.status)
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
		if got := filterContainersByStatus(nil, "running"); len(got) != 0 {
			t.Errorf("expected empty result for nil input, got %+v", got)
		}
	})

	t.Run("original slice is not mutated", func(t *testing.T) {
		containers := newContainers()
		want := newContainers()
		filterContainersByStatus(containers, "running")
		for i := range want {
			if containers[i] != want[i] {
				t.Errorf("input[%d] was mutated: got %+v, want %+v", i, containers[i], want[i])
			}
		}
	})
}
