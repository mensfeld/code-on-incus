package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/config"
)

func TestDetectBootScript_Absent(t *testing.T) {
	found, _, err := detectBootScript(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("want found=false in empty workspace")
	}
}

func TestDetectBootScript_Executable(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, bootScriptName)
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	found, executable, err := detectBootScript(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("want found=true")
	}
	if !executable {
		t.Error("want executable=true for a 0755 script (direct exec, shebang respected)")
	}
}

func TestDetectBootScript_NonExecutable(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, bootScriptName)
	if err := os.WriteFile(script, []byte("echo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	found, executable, err := detectBootScript(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("want found=true")
	}
	if executable {
		t.Error("want executable=false for a 0644 script (run via bash)")
	}
}

func TestDetectBootScript_DirectoryIsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, bootScriptName), 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, err := detectBootScript(dir)
	if err == nil {
		t.Fatal("want error when coi-boot.sh is a directory")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("error should mention directory, got: %v", err)
	}
}

func TestRemovedFlagHint_ImageAndPersistent(t *testing.T) {
	for _, flag := range []string{"--image", "--persistent"} {
		err := removedFlagHint(nil, errors.New("unknown flag: "+flag))
		if err == nil {
			t.Fatalf("%s: want non-nil error", flag)
		}
		msg := err.Error()
		if !strings.Contains(msg, "flag was removed") || !strings.Contains(msg, "[container]") {
			t.Errorf("%s: want migration hint pointing at [container] config, got: %s", flag, msg)
		}
	}
}

func TestRemovedFlagHint_OtherFlagsUntouched(t *testing.T) {
	orig := errors.New("unknown flag: --bogus")
	err := removedFlagHint(nil, orig)
	if err != orig {
		t.Errorf("unrelated flag errors must pass through unchanged, got: %v", err)
	}
	if removedFlagHint(nil, nil) != nil {
		t.Error("nil error must stay nil")
	}
}

func TestResolveImageName_ConfigDriven(t *testing.T) {
	cfg := config.GetDefaultConfig()
	if got := ResolveImageName(cfg); got == "" {
		t.Error("want built-in default image when config sets none")
	}

	cfg.Container.Image = "my-custom"
	if got := ResolveImageName(cfg); got != "my-custom" {
		t.Errorf("want config image to win, got %q", got)
	}
}
