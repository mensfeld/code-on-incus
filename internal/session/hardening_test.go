package session

import (
	"testing"
)

// hardeningPolicyFrom passes the raw flags through; the docker-vs-hardening
// precedence is resolved by HardeningPolicy.DockerEnabled, not here. So the
// mapping is a straight copy, and DockerEnabled applies "reduce wins".
func TestHardeningPolicyFrom(t *testing.T) {
	tests := []struct {
		docker, reduce  bool
		wantDockerAfter bool // DockerEnabled() result
	}{
		{true, false, true},
		{false, false, false},
		{true, true, false},
		{false, true, false},
	}
	for _, tt := range tests {
		opts := &SetupOptions{DockerSupport: tt.docker, ReduceKernelSurface: tt.reduce}
		got := hardeningPolicyFrom(opts)
		if got.Docker != tt.docker || got.ReduceKernelSurface != tt.reduce {
			t.Errorf("docker=%v reduce=%v: got %+v, want raw flags copied", tt.docker, tt.reduce, got)
		}
		if got.DockerEnabled() != tt.wantDockerAfter {
			t.Errorf("docker=%v reduce=%v: DockerEnabled()=%v, want %v", tt.docker, tt.reduce, got.DockerEnabled(), tt.wantDockerAfter)
		}
	}
}
