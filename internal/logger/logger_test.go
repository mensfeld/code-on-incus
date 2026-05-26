package logger

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func TestNew_CreatesFiles(t *testing.T) {
	dir := t.TempDir()
	l := New("test-container", dir)
	defer l.Close()

	l.Printf("info message %d", 1)
	l.Println("info line")
	l.Errorf("error message %d", 1)
	l.Errorln("error line")

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	outData, err := os.ReadFile(l.OutPath())
	if err != nil {
		t.Fatalf("read stdout log: %v", err)
	}
	if !strings.Contains(string(outData), "info message 1") {
		t.Errorf("stdout log missing expected content: %s", outData)
	}

	errData, err := os.ReadFile(l.ErrPath())
	if err != nil {
		t.Fatalf("read stderr log: %v", err)
	}
	if !strings.Contains(string(errData), "error message 1") {
		t.Errorf("stderr log missing expected content: %s", errData)
	}
}

func TestNew_FallbackOnBadDir(t *testing.T) {
	// Point homeDir at a file so MkdirAll fails.
	f, err := os.CreateTemp("", "not-a-dir")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	l := New("test-container", f.Name())
	if l == nil {
		t.Fatal("expected non-nil logger on fallback")
	}
	// Must not panic.
	l.Printf("should discard")
	l.Errorf("should discard")
	if err := l.Close(); err != nil {
		t.Fatalf("Close on discard logger: %v", err)
	}
}

func TestConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	l := New("concurrent", dir)
	defer l.Close()

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			l.Printf("info %d", n)
		}(i)
		go func(n int) {
			defer wg.Done()
			l.Errorf("error %d", n)
		}(i)
	}
	wg.Wait()
}

func TestClose_Idempotent(t *testing.T) {
	dir := t.TempDir()
	l := New("idempotent", dir)
	if err := l.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second close must not panic or return an error about already-closed file.
	_ = l.Close()
}

func TestNewDiscard(t *testing.T) {
	l := NewDiscard()
	if l == nil {
		t.Fatal("expected non-nil discard logger")
	}
	l.Printf("anything")
	l.Errorf("anything")
	if l.OutPath() != "" || l.ErrPath() != "" {
		t.Error("discard logger should have empty paths")
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close on discard: %v", err)
	}
}
