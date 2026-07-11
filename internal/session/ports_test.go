package session

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func withPortProbe(t *testing.T, probe func(listen string, port int) bool) {
	t.Helper()
	orig := hostPortFree
	hostPortFree = probe
	t.Cleanup(func() { hostPortFree = orig })
}

func allFree(string, int) bool { return true }

func TestAllocateHostPort(t *testing.T) {
	ws := "/some/workspace"
	// Deterministic
	if AllocateHostPort(ws, 1, 0) != AllocateHostPort(ws, 1, 0) {
		t.Error("allocation must be deterministic")
	}
	// Slot stride: slot 2 is exactly one block above slot 1
	if AllocateHostPort(ws, 2, 0)-AllocateHostPort(ws, 1, 0) != slotStride {
		t.Error("slots must occupy adjacent blocks")
	}
	// Index within block
	if AllocateHostPort(ws, 1, 3)-AllocateHostPort(ws, 1, 0) != 3 {
		t.Error("indices must be consecutive within a block")
	}
	// Range bounds
	for slot := 1; slot <= 10; slot++ {
		p := AllocateHostPort(ws, slot, slotStride-1)
		if p < autoPortBase || p >= autoPortBase+neighborhoods*neighborhoodSize {
			t.Errorf("port %d out of range for slot %d", p, slot)
		}
	}
	// Different workspaces land in different neighborhoods (for these two)
	if AllocateHostPort("/ws/a", 1, 0) == AllocateHostPort("/ws/b", 1, 0) {
		t.Log("hash collision between test workspaces — permissible but unexpected")
	}
}

func TestPortEnvVar(t *testing.T) {
	if got := portEnvVar("web"); got != "COI_PORT_WEB" {
		t.Errorf("portEnvVar(web) = %q", got)
	}
	if got := portEnvVar("my-api.v2"); got != "COI_PORT_MY_API_V2" {
		t.Errorf("portEnvVar(my-api.v2) = %q", got)
	}
}

func TestResolvePortsPoolIsIdentityMapped(t *testing.T) {
	withPortProbe(t, allFree)
	resolved, err := ResolvePorts(&PortConfig{Pool: 3}, "/ws", 1)
	if err != nil {
		t.Fatalf("ResolvePorts: %v", err)
	}
	if len(resolved) != 3 {
		t.Fatalf("want 3 pool ports, got %d", len(resolved))
	}
	for i, p := range resolved {
		if !p.Pool {
			t.Errorf("port %d not marked pool", i)
		}
		if p.HostPort != p.ContainerPort {
			t.Errorf("pool port %d not identity-mapped: host %d container %d", i, p.HostPort, p.ContainerPort)
		}
		if want := AllocateHostPort("/ws", 1, i); p.HostPort != want {
			t.Errorf("pool port %d = %d, want deterministic %d", i, p.HostPort, want)
		}
	}
}

func TestResolvePortsPinnedTakenIsHardError(t *testing.T) {
	withPortProbe(t, func(_ string, port int) bool { return port != 5432 })
	_, err := ResolvePorts(&PortConfig{Ports: []PortEntry{
		{Name: "postgres", ContainerPort: 5432, HostPort: 5432},
	}}, "/ws", 1)
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("pinned taken port must abort, got: %v", err)
	}
}

func TestResolvePortsAutoSkipsBusyForward(t *testing.T) {
	first := AllocateHostPort("/ws", 1, 0)
	withPortProbe(t, func(_ string, port int) bool { return port != first })
	resolved, err := ResolvePorts(&PortConfig{Pool: 1}, "/ws", 1)
	if err != nil {
		t.Fatalf("ResolvePorts: %v", err)
	}
	if resolved[0].HostPort != first+1 {
		t.Errorf("busy deterministic port should skip forward: got %d, want %d", resolved[0].HostPort, first+1)
	}
}

func TestResolvePortsBlockExhaustion(t *testing.T) {
	withPortProbe(t, func(string, int) bool { return false })
	_, err := ResolvePorts(&PortConfig{Pool: 1}, "/ws", 1)
	if err == nil || !strings.Contains(err.Error(), "no free port left") {
		t.Fatalf("exhausted block must error, got: %v", err)
	}
}

func TestResolvePortsCapacity(t *testing.T) {
	withPortProbe(t, allFree)
	_, err := ResolvePorts(&PortConfig{Pool: 9, Ports: []PortEntry{
		{Name: "a", ContainerPort: 3000},
		{Name: "b", ContainerPort: 4000},
	}}, "/ws", 1)
	if err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("over-capacity must error, got: %v", err)
	}
	// Pinned entries don't count against the auto budget
	_, err = ResolvePorts(&PortConfig{Pool: 9, Ports: []PortEntry{
		{Name: "a", ContainerPort: 3000},
		{Name: "b", ContainerPort: 4000, HostPort: 24000},
	}}, "/ws", 1)
	if err != nil {
		t.Fatalf("pinned entry must not consume auto budget: %v", err)
	}
}

// fakePublisher records device adds; failNames simulates bind races.
type fakePublisher struct {
	added     []string
	removed   []string
	failNames map[string]bool
}

func (f *fakePublisher) AddHostPortDevice(name, listenAddr string, hostPort, containerPort int) error {
	if f.failNames[name] {
		return errors.New("Error: listen tcp: bind: address already in use")
	}
	f.added = append(f.added, fmt.Sprintf("%s=%s:%d->%d", name, listenAddr, hostPort, containerPort))
	return nil
}
func (f *fakePublisher) RemoveDevice(name string) error {
	f.removed = append(f.removed, name)
	return nil
}

func TestPublishResolvedPorts(t *testing.T) {
	withPortProbe(t, allFree)
	resolved, err := ResolvePorts(&PortConfig{
		Pool:  2,
		Ports: []PortEntry{{Name: "web", ContainerPort: 3000}},
	}, "/ws", 1)
	if err != nil {
		t.Fatalf("ResolvePorts: %v", err)
	}

	pub := &fakePublisher{}
	published, env := PublishResolvedPorts(pub, resolved, func(string) {})

	if len(published) != 3 {
		t.Fatalf("want 3 published, got %d", len(published))
	}
	p0, p1 := AllocateHostPort("/ws", 1, 0), AllocateHostPort("/ws", 1, 1)
	if want := fmt.Sprintf("%d %d", p0, p1); env["COI_PORTS"] != want {
		t.Errorf("COI_PORTS = %q, want %q", env["COI_PORTS"], want)
	}
	if env["COI_PORT_WEB"] == "" {
		t.Error("COI_PORT_WEB missing")
	}
	// devices removed-then-added (stale-device tolerance)
	if len(pub.removed) != 3 {
		t.Errorf("expected 3 stale-device removals, got %d", len(pub.removed))
	}

	// A bind race drops only the affected entry and keeps env truthful
	pub2 := &fakePublisher{failNames: map[string]bool{"coi-port-web": true}}
	published2, env2 := PublishResolvedPorts(pub2, resolved, func(string) {})
	if len(published2) != 2 {
		t.Fatalf("want 2 published after one failure, got %d", len(published2))
	}
	if _, ok := env2["COI_PORT_WEB"]; ok {
		t.Error("failed entry must not appear in env")
	}
	if env2["COI_PORTS"] == "" {
		t.Error("pool env must survive an unrelated entry failure")
	}
}
