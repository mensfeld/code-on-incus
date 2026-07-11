package session

import (
	"errors"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// fakeExec implements container.ContainerExecution with a canned
// ExecArgsCapture response; the other methods are unused by ResolveCodeUID.
type fakeExec struct {
	out string
	err error
}

func (f *fakeExec) Exec(args ...string) error { return nil }
func (f *fakeExec) ExecArgs(commandArgs []string, opts container.ExecCommandOptions) error {
	return nil
}

func (f *fakeExec) ExecArgsCapture(commandArgs []string, opts container.ExecCommandOptions) (string, error) {
	return f.out, f.err
}

func (f *fakeExec) ExecCommand(command string, opts container.ExecCommandOptions) (string, error) {
	return "", nil
}
func (f *fakeExec) ExecHostCommand(command string, capture bool) (string, error) { return "", nil }

func TestResolveCodeUID(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		err     error
		want    int
		wantErr bool
	}{
		{name: "default code uid", out: "1000\n", want: 1000},
		{name: "remapped uid", out: "501\n", want: 501},
		{name: "surrounding whitespace", out: "  1000  \n", want: 1000},
		// `id` exits non-zero for a missing user: images without a code
		// user run sessions (and tmux) as root
		{name: "no code user falls back to root", err: &container.ExitError{ExitCode: 1}, want: 0},
		// connectivity failures must NOT silently become root — that is
		// the exact misdirection the resolver exists to prevent (#588)
		{name: "connectivity error propagates", err: errors.New("incus unreachable"), wantErr: true},
		{name: "garbage output is an error", out: "not-a-uid\n", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveCodeUID(&fakeExec{out: tt.out, err: tt.err}, "code")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveCodeUID(out=%q, err=%v) expected error, got uid %d", tt.out, tt.err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveCodeUID(out=%q, err=%v) unexpected error: %v", tt.out, tt.err, err)
			}
			if got != tt.want {
				t.Errorf("ResolveCodeUID(out=%q) = %d, want %d", tt.out, got, tt.want)
			}
		})
	}
}
