package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePullDestination(t *testing.T) {
	dir := t.TempDir()

	existingDir := filepath.Join(dir, "existing-dir")
	if err := os.Mkdir(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existingFile := filepath.Join(dir, "existing-file.txt")
	if err := os.WriteFile(existingFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirWithInnerDir := filepath.Join(dir, "outer")
	if err := os.MkdirAll(filepath.Join(dirWithInnerDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	dirWithInnerFile := filepath.Join(dir, "outer2")
	if err := os.Mkdir(dirWithInnerFile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirWithInnerFile, "data"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		localPath  string
		remotePath string
		recursive  bool
		want       string // expected final target ("" if wantErr)
		wantErr    string // substring the error must contain
	}{
		{
			name:       "nonexistent exact path",
			localPath:  filepath.Join(dir, "new.txt"),
			remotePath: "/tmp/example.output",
			want:       filepath.Join(dir, "new.txt"),
		},
		{
			name:       "existing directory places inside",
			localPath:  existingDir,
			remotePath: "/tmp/example.output",
			want:       filepath.Join(existingDir, "example.output"),
		},
		{
			name:       "existing directory with trailing slash places inside",
			localPath:  existingDir + "/",
			remotePath: "/tmp/example.output",
			want:       filepath.Join(existingDir, "example.output"),
		},
		{
			name:       "recursive into existing directory places inside",
			localPath:  existingDir,
			remotePath: "/root/.claude",
			recursive:  true,
			want:       filepath.Join(existingDir, ".claude"),
		},
		{
			name:       "remote trailing slash still derives basename",
			localPath:  existingDir,
			remotePath: "/root/.claude/",
			recursive:  true,
			want:       filepath.Join(existingDir, ".claude"),
		},
		{
			name:       "existing file target allowed for single file",
			localPath:  existingFile,
			remotePath: "/tmp/example.output",
			want:       existingFile,
		},
		{
			name:       "existing file target refused for recursive",
			localPath:  existingFile,
			remotePath: "/tmp/data",
			recursive:  true,
			wantErr:    "already exists",
		},
		{
			name:       "inner directory target refused",
			localPath:  dirWithInnerDir,
			remotePath: "/tmp/data",
			wantErr:    "already exists",
		},
		{
			name:       "inner file target allowed for single file",
			localPath:  dirWithInnerFile,
			remotePath: "/tmp/data",
			want:       filepath.Join(dirWithInnerFile, "data"),
		},
		{
			name:       "inner file target refused for recursive",
			localPath:  dirWithInnerFile,
			remotePath: "/tmp/data",
			recursive:  true,
			wantErr:    "already exists and is not a directory",
		},
		{
			name:       "trailing slash on nonexistent path fails fast",
			localPath:  filepath.Join(dir, "newdir") + "/",
			remotePath: "/tmp/example.output",
			wantErr:    "trailing slash requires an existing directory",
		},
		{
			name:       "trailing slash on existing file fails",
			localPath:  existingFile + "/",
			remotePath: "/tmp/example.output",
			wantErr:    "not a directory",
		},
		{
			name:       "remote root path refused",
			localPath:  filepath.Join(dir, "out"),
			remotePath: "/",
			wantErr:    "cannot derive a file name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePullDestination(tt.localPath, tt.remotePath, tt.recursive)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got target %q", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("target = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestResolvePullDestinationDotAndParent covers the incident shapes: "." and
// "../" destinations must resolve to "inside that directory", never to the
// directory itself.
func TestResolvePullDestinationDotAndParent(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)

	got, err := resolvePullDestination("../", "/tmp/example.output", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Join("..", "example.output") {
		t.Fatalf("target = %q, want %q", got, filepath.Join("..", "example.output"))
	}

	got, err = resolvePullDestination(".", "/tmp/example.output", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "example.output" {
		t.Fatalf("target = %q, want %q", got, "example.output")
	}
}
