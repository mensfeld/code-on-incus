package cli

import (
	"bytes"
	"testing"
)

func TestTableBasicRender(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("NAME", "AGE")
	tbl.SetOutput(&buf)
	tbl.AddRow("alice", "30")
	tbl.AddRow("bob", "25")
	tbl.Render()

	got := buf.String()
	// Check header and rows are present
	if !bytes.Contains([]byte(got), []byte("NAME")) {
		t.Errorf("expected header NAME in output, got:\n%s", got)
	}
	if !bytes.Contains([]byte(got), []byte("alice")) {
		t.Errorf("expected row alice in output, got:\n%s", got)
	}
	if !bytes.Contains([]byte(got), []byte("bob")) {
		t.Errorf("expected row bob in output, got:\n%s", got)
	}
}

func TestTableLen(t *testing.T) {
	tbl := NewTable("A", "B")
	if tbl.Len() != 0 {
		t.Errorf("expected Len() == 0, got %d", tbl.Len())
	}
	tbl.AddRow("1", "2")
	tbl.AddRow("3", "4")
	if tbl.Len() != 2 {
		t.Errorf("expected Len() == 2, got %d", tbl.Len())
	}
}

func TestTableShortRowPadding(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("A", "B", "C")
	tbl.SetOutput(&buf)
	tbl.AddRow("only-one")
	tbl.Render()

	got := buf.String()
	// Should not panic and should contain the value
	if !bytes.Contains([]byte(got), []byte("only-one")) {
		t.Errorf("expected padded row in output, got:\n%s", got)
	}
}

func TestTableColumnAlignment(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("NAME", "VALUE")
	tbl.SetOutput(&buf)
	tbl.AddRow("short", "1")
	tbl.AddRow("a-much-longer-name", "2")
	tbl.Render()

	got := buf.String()
	// Both values should appear on their respective lines
	if !bytes.Contains([]byte(got), []byte("short")) {
		t.Errorf("expected short in output, got:\n%s", got)
	}
	if !bytes.Contains([]byte(got), []byte("a-much-longer-name")) {
		t.Errorf("expected a-much-longer-name in output, got:\n%s", got)
	}
}
