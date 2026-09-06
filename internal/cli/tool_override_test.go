package cli

import (
	"testing"

	"github.com/mensfeld/code-on-incus/internal/config"
)

// resolveConfiguredTool applies the --tool override on top of [tool] name, wins
// over config, and validates the name (#708).
func TestResolveConfiguredTool_Override(t *testing.T) {
	t.Cleanup(func() { toolOverride = "" })

	t.Run("no override uses configured default", func(t *testing.T) {
		toolOverride = ""
		a := &App{cfg: &config.Config{Tool: config.ToolConfig{Name: "opencode"}}}
		got, err := a.resolveConfiguredTool()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name() != "opencode" {
			t.Errorf("got %q, want opencode", got.Name())
		}
	})

	t.Run("empty config defaults to claude", func(t *testing.T) {
		toolOverride = ""
		a := &App{cfg: &config.Config{}}
		got, err := a.resolveConfiguredTool()
		if err != nil || got.Name() != "claude" {
			t.Fatalf("got (%v, %v), want (claude, nil)", got, err)
		}
	})

	t.Run("override wins over configured name", func(t *testing.T) {
		toolOverride = "codex"
		a := &App{cfg: &config.Config{Tool: config.ToolConfig{Name: "claude"}}}
		got, err := a.resolveConfiguredTool()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name() != "codex" {
			t.Errorf("override ignored: got %q, want codex", got.Name())
		}
	})

	t.Run("unknown override errors", func(t *testing.T) {
		toolOverride = "bogus"
		a := &App{cfg: &config.Config{Tool: config.ToolConfig{Name: "claude"}}}
		if _, err := a.resolveConfiguredTool(); err == nil {
			t.Fatal("expected error for an unknown --tool value")
		}
	})
}
