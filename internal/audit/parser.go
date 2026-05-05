package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ParseLine decodes a single JSON-line record from the in-container agent.
//
// Lines that don't start with '{' are returned as a synthetic type=audit
// event with Msg="agent.stderr" so noisy shell warnings don't break the
// stream. Empty / whitespace-only lines return (nil, nil).
func ParseLine(line string) (*Event, error) {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return nil, nil
	}
	if !strings.HasPrefix(strings.TrimLeft(line, " \t"), "{") {
		return &Event{Type: TypeAudit, Msg: "agent.stderr", Raw: line}, nil
	}

	var ev Event
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if ev.Type == "" {
		return nil, fmt.Errorf("parse: missing type field")
	}
	switch ev.Type {
	case TypeExec, TypeNet, TypeFile, TypeAudit:
		// known
	default:
		return nil, fmt.Errorf("parse: unknown type %q", ev.Type)
	}
	return &ev, nil
}

// StreamEvents reads JSON-lines from r, parses each, and emits decorated
// events on out. sessionID and container are stamped onto every event.
//
// The function returns when r reaches EOF or yields a read error other than
// io.EOF. Parse errors on individual lines are forwarded to onError (if non-
// nil) and skipped — they do not abort the stream.
func StreamEvents(
	r io.Reader,
	sessionID, container string,
	emit func(Event) error,
	onError func(error),
) error {
	sc := bufio.NewScanner(r)
	// Default token size is 64K. Auditd lines can exceed this for long
	// argv records; bump to 1MB.
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	for sc.Scan() {
		ev, err := ParseLine(sc.Text())
		if err != nil {
			if onError != nil {
				onError(err)
			}
			continue
		}
		if ev == nil {
			continue
		}
		if sessionID != "" {
			ev.SessionID = sessionID
		}
		if container != "" {
			ev.Container = container
		}
		if err := emit(*ev); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}
