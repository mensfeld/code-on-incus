package session

import (
	"fmt"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/tool"
)

// fakeDirProbe is a minimal containerCommandRunner: it records the command and
// returns success/failure to stand in for `test -d`'s exit status.
type fakeDirProbe struct {
	present bool
	gotCmd  string
}

func (f *fakeDirProbe) ExecCommand(cmd string, _ container.ExecCommandOptions) (string, error) {
	f.gotCmd = cmd
	if f.present {
		return "", nil
	}
	return "", fmt.Errorf("exit status 1") // `test -d` on a missing dir
}

// toolConfigDirPresent probes the tool's config dir via `test -d` and maps the
// exit status to presence (#708 follow-up: gates reuse-seeding of a new tool).
func TestToolConfigDirPresent(t *testing.T) {
	c, err := tool.Get("claude")
	if err != nil {
		t.Fatalf("tool.Get: %v", err)
	}
	tcf, ok := c.(tool.ToolWithConfigDirFiles)
	if !ok {
		t.Fatal("claude should implement ToolWithConfigDirFiles")
	}

	t.Run("present", func(t *testing.T) {
		fp := &fakeDirProbe{present: true}
		if !toolConfigDirPresent(fp, "/home/code", tcf) {
			t.Error("want present=true when test -d succeeds")
		}
		if fp.gotCmd != "test -d /home/code/.claude" {
			t.Errorf("probe command = %q, want `test -d /home/code/.claude`", fp.gotCmd)
		}
	})

	t.Run("absent", func(t *testing.T) {
		fa := &fakeDirProbe{present: false}
		if toolConfigDirPresent(fa, "/home/code", tcf) {
			t.Error("want present=false when test -d fails")
		}
	})
}
