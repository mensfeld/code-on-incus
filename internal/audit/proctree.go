package audit

import (
	"strconv"
	"strings"
)

// ProcRow is one entry from a `ps -eo pid,ppid,comm,args` snapshot.
type ProcRow struct {
	PID  int
	PPID int
	Comm string
	Args string
}

// ParsePSSnapshot parses the output of `ps -eo pid=,ppid=,comm=,args=` into
// rows. Lines that don't have at least three whitespace-separated fields are
// skipped silently — the stream is best-effort.
func ParsePSSnapshot(snapshot string) []ProcRow {
	var rows []ProcRow
	for _, line := range strings.Split(snapshot, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		row := ProcRow{PID: pid, PPID: ppid, Comm: fields[2]}
		if len(fields) > 3 {
			row.Args = strings.Join(fields[3:], " ")
		}
		rows = append(rows, row)
	}
	return rows
}

// DiffNewPIDs returns rows from cur whose PID is not present in prev.
//
// This is the basic "new process" detector that ps_loop in agent.sh
// implements in shell; the Go version here is what the host-side fallback
// uses when running the parser against fixture files in tests.
func DiffNewPIDs(prev, cur []ProcRow) []ProcRow {
	seen := make(map[int]struct{}, len(prev))
	for _, r := range prev {
		seen[r.PID] = struct{}{}
	}
	var out []ProcRow
	for _, r := range cur {
		if _, ok := seen[r.PID]; ok {
			continue
		}
		out = append(out, r)
	}
	return out
}
