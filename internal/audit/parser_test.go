package audit

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParseLine_Empty(t *testing.T) {
	for _, in := range []string{"", "   ", "\t\n", "\r\n"} {
		ev, err := ParseLine(in)
		if err != nil {
			t.Errorf("input %q: unexpected err %v", in, err)
		}
		if ev != nil {
			t.Errorf("input %q: expected nil event, got %+v", in, ev)
		}
	}
}

func TestParseLine_NonJSONStderr(t *testing.T) {
	ev, err := ParseLine("tail: /var/log/syslog: No such file")
	if err != nil {
		t.Fatalf("non-json: unexpected err %v", err)
	}
	if ev == nil || ev.Type != TypeAudit || ev.Msg != "agent.stderr" {
		t.Errorf("expected agent.stderr fallback, got %+v", ev)
	}
	if !strings.Contains(ev.Raw, "No such file") {
		t.Errorf("Raw not preserved: %q", ev.Raw)
	}
}

func TestParseLine_Exec(t *testing.T) {
	in := `{"ts":"2026-05-05T12:00:00Z","type":"exec","pid":42,"ppid":1,"comm":"curl","args":"curl https://example.com"}`
	ev, err := ParseLine(in)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev.Type != TypeExec || ev.PID != 42 || ev.PPID != 1 || ev.Comm != "curl" {
		t.Errorf("bad event: %+v", ev)
	}
	if !strings.Contains(ev.Args, "example.com") {
		t.Errorf("Args missing: %q", ev.Args)
	}
}

func TestParseLine_Net(t *testing.T) {
	in := `{"ts":"2026-05-05T12:00:00Z","type":"net","proto":"tcp","state":"ESTAB","local":"10.0.0.2:54321","peer":"169.254.169.254:80","pid":99,"comm":"python3"}`
	ev, err := ParseLine(in)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev.Type != TypeNet || ev.Proto != "tcp" || ev.Peer != "169.254.169.254:80" || ev.PID != 99 {
		t.Errorf("bad net event: %+v", ev)
	}
}

func TestParseLine_File(t *testing.T) {
	in := `{"ts":"2026-05-05T12:00:00Z","type":"file","path":"/etc/shadow","op":"NORMAL"}`
	ev, err := ParseLine(in)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev.Type != TypeFile || ev.Path != "/etc/shadow" || ev.Op != "NORMAL" {
		t.Errorf("bad file event: %+v", ev)
	}
}

func TestParseLine_UnknownType(t *testing.T) {
	in := `{"ts":"x","type":"galaxy"}`
	if _, err := ParseLine(in); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestParseLine_MissingType(t *testing.T) {
	in := `{"ts":"x"}`
	if _, err := ParseLine(in); err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestParseLine_BadJSON(t *testing.T) {
	in := `{"ts":"x","type":`
	if _, err := ParseLine(in); err == nil {
		t.Fatal("expected error for malformed json")
	}
}

func TestStreamEvents_DecoratesAndFiltersErrors(t *testing.T) {
	in := strings.Join([]string{
		`{"ts":"t","type":"exec","pid":1,"comm":"sh"}`,
		`tail: oops`, // stderr — promoted to type=audit
		`{"ts":"t","type":"net","proto":"tcp"}`,
		`{"ts":"t","type":"galaxy"}`, // unknown type — onError + skip
		`{"ts":"t","type":"file","path":"/etc/passwd"}`,
		``,
	}, "\n")

	var emitted []Event
	var errs []error
	err := StreamEvents(strings.NewReader(in), "sess1", "coi-x", func(ev Event) error {
		emitted = append(emitted, ev)
		return nil
	}, func(err error) { errs = append(errs, err) })
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(emitted) != 4 {
		t.Fatalf("want 4 events (exec, audit-stderr, net, file), got %d: %+v", len(emitted), emitted)
	}
	for _, ev := range emitted {
		if ev.SessionID != "sess1" || ev.Container != "coi-x" {
			t.Errorf("decoration missing: %+v", ev)
		}
	}
	if len(errs) != 1 {
		t.Errorf("want 1 parse error (galaxy), got %d", len(errs))
	}
}

func TestStreamEvents_EmitErrorAborts(t *testing.T) {
	in := `{"ts":"t","type":"exec","pid":1}` + "\n" + `{"ts":"t","type":"net"}`
	target := errors.New("downstream gone")
	count := 0
	err := StreamEvents(strings.NewReader(in), "", "", func(ev Event) error {
		count++
		return target
	}, nil)
	if !errors.Is(err, target) {
		t.Fatalf("want target err, got %v", err)
	}
	if count != 1 {
		t.Errorf("expected emit called once before abort, got %d", count)
	}
}

func TestStreamEvents_LongLine(t *testing.T) {
	// Auditd records can be long. Make sure we don't hit the default 64K limit.
	big := bytes.Repeat([]byte("A"), 200_000)
	in := `{"ts":"t","type":"audit","msg":"x","raw":"` + string(big) + `"}`
	count := 0
	err := StreamEvents(strings.NewReader(in), "", "", func(ev Event) error {
		count++
		if len(ev.Raw) != 200_000 {
			t.Errorf("Raw truncated: got %d bytes", len(ev.Raw))
		}
		return nil
	}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1, got %d", count)
	}
}
