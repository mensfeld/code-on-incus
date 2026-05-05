package audit

import "testing"

func TestParsePSSnapshot(t *testing.T) {
	snap := `    1     0 systemd /sbin/init
   42     1 sh sh -c echo hi
  137    42 curl curl https://example.com -o /tmp/x
  malformed line
   99     1 short
`
	rows := ParsePSSnapshot(snap)
	if len(rows) != 4 {
		t.Fatalf("want 4 rows (3 full + 1 short), got %d", len(rows))
	}
	if rows[0].PID != 1 || rows[0].Comm != "systemd" {
		t.Errorf("row 0 wrong: %+v", rows[0])
	}
	if rows[1].PPID != 1 || rows[1].Comm != "sh" || rows[1].Args != "sh -c echo hi" {
		t.Errorf("row 1 wrong: %+v", rows[1])
	}
	if rows[2].PID != 137 || rows[2].PPID != 42 || rows[2].Comm != "curl" {
		t.Errorf("row 2 wrong: %+v", rows[2])
	}
	if rows[3].PID != 99 || rows[3].Args != "" {
		t.Errorf("row 3 (no args) wrong: %+v", rows[3])
	}
}

func TestDiffNewPIDs(t *testing.T) {
	prev := []ProcRow{
		{PID: 1, Comm: "systemd"},
		{PID: 42, Comm: "sh"},
	}
	cur := []ProcRow{
		{PID: 1, Comm: "systemd"},
		{PID: 42, Comm: "sh"},
		{PID: 137, Comm: "curl"},
		{PID: 138, Comm: "wget"},
	}
	newRows := DiffNewPIDs(prev, cur)
	if len(newRows) != 2 {
		t.Fatalf("want 2 new pids, got %d: %+v", len(newRows), newRows)
	}
	if newRows[0].PID != 137 || newRows[1].PID != 138 {
		t.Errorf("wrong new pids: %+v", newRows)
	}
}

func TestDiffNewPIDs_NoChange(t *testing.T) {
	rows := []ProcRow{{PID: 1}, {PID: 2}}
	if got := DiffNewPIDs(rows, rows); len(got) != 0 {
		t.Errorf("expected no diff, got %+v", got)
	}
}

func TestDiffNewPIDs_PIDReuse(t *testing.T) {
	// pid 42 disappears, then 42 reappears as a different command. We can't
	// detect reuse with pid-only diffing — the new row is hidden because the
	// pid is "seen". Document this behaviour.
	prev := []ProcRow{{PID: 42, Comm: "sh"}}
	cur := []ProcRow{{PID: 42, Comm: "evil"}}
	if got := DiffNewPIDs(prev, cur); len(got) != 0 {
		t.Errorf("known limitation: pid reuse goes undetected (got %+v)", got)
	}
}

func TestParsePSSnapshot_Empty(t *testing.T) {
	if rows := ParsePSSnapshot(""); len(rows) != 0 {
		t.Errorf("want 0 rows from empty input, got %d", len(rows))
	}
	if rows := ParsePSSnapshot("\n\n   \n"); len(rows) != 0 {
		t.Errorf("want 0 rows from whitespace, got %d", len(rows))
	}
}
