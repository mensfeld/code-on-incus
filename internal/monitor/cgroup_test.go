package monitor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitPIDFromCgroupProcs_SingleProc(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cgroup.procs"), []byte("1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pid, err := initPIDFromCgroupProcs(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 1234 {
		t.Errorf("want 1234 got %d", pid)
	}
}

func TestInitPIDFromCgroupProcs_ReturnsMinimum(t *testing.T) {
	// Simulate a systemd container: PID 1 is in init.scope, other processes
	// are in sub-cgroups with higher PIDs.
	dir := t.TempDir()

	// Root cgroup.procs is empty (systemd moves all procs to sub-cgroups).
	if err := os.WriteFile(filepath.Join(dir, "cgroup.procs"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	initScope := filepath.Join(dir, "init.scope")
	if err := os.MkdirAll(initScope, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(initScope, "cgroup.procs"), []byte("500\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	svcSlice := filepath.Join(dir, "system.slice", "ssh.service")
	if err := os.MkdirAll(svcSlice, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcSlice, "cgroup.procs"), []byte("800\n900\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pid, err := initPIDFromCgroupProcs(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 500 {
		t.Errorf("want 500 (minimum) got %d", pid)
	}
}

func TestInitPIDFromCgroupProcs_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cgroup.procs"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := initPIDFromCgroupProcs(dir)
	if err == nil {
		t.Error("expected error for empty cgroup.procs, got nil")
	}
}

func TestInitPIDFromCgroupProcs_NonexistentPath(t *testing.T) {
	_, err := initPIDFromCgroupProcs("/nonexistent/cgroup/path")
	if err == nil {
		t.Error("expected error for nonexistent path, got nil")
	}
}

func TestParseInitPIDFromIncusInfo_Valid(t *testing.T) {
	output := `Name: mycontainer
Status: Running
Type: container
Architecture: x86_64
PID: 42317
Created: 2026/06/01 12:00 UTC`
	pid, err := parseInitPIDFromIncusInfo(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 42317 {
		t.Errorf("want 42317 got %d", pid)
	}
}

func TestParseInitPIDFromIncusInfo_CaseInsensitive(t *testing.T) {
	output := "Pid: 99\n"
	pid, err := parseInitPIDFromIncusInfo(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 99 {
		t.Errorf("want 99 got %d", pid)
	}
}

func TestParseInitPIDFromIncusInfo_Missing(t *testing.T) {
	output := "Name: mycontainer\nStatus: Stopped\n"
	_, err := parseInitPIDFromIncusInfo(output)
	if err == nil {
		t.Error("expected error when PID line absent, got nil")
	}
}
