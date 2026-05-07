// Package audit implements threat-event audit streaming for coi sandbox sessions.
//
// The host-side Collector launches an in-container shell agent (agent/agent.sh)
// via incus exec, reads the agent's JSON-line stdout, augments each event with
// the session ID + container name, and re-emits the JSON line on stdout.
//
// Event sources inside the container are auditd (when present), syslog/auth.log
// fallback, periodic ss snapshots for network events, and periodic ps snapshots
// diffed for new pids. No daemon is installed; the agent terminates with the
// incus exec invocation.
package audit

import (
	"encoding/json"
)

// EventType identifies the kind of audit event.
//
// Stable wire values; consumers may filter by these.
const (
	TypeExec      = "exec"
	TypeNet       = "net"
	TypeFile      = "file"
	TypeAudit     = "audit"
	TypeHeartbeat = "heartbeat"
)

// Event is the on-the-wire shape of a single JSON-line audit record.
//
// Fields are intentionally minimal; per-type fields are sparse-populated and
// omitempty so the JSONL stays compact. Unknown fields from future agent
// versions are preserved via Extra.
type Event struct {
	TS        string `json:"ts"`
	SessionID string `json:"sessionId,omitempty"`
	Container string `json:"container,omitempty"`
	Type      string `json:"type"`

	// type=exec
	PID  int    `json:"pid,omitempty"`
	PPID int    `json:"ppid,omitempty"`
	Comm string `json:"comm,omitempty"`
	Args string `json:"args,omitempty"`

	// type=net
	Proto string `json:"proto,omitempty"`
	State string `json:"state,omitempty"`
	Local string `json:"local,omitempty"`
	Peer  string `json:"peer,omitempty"`

	// type=file
	Path string `json:"path,omitempty"`
	Op   string `json:"op,omitempty"`

	// type=audit
	Msg string `json:"msg,omitempty"`
	Raw string `json:"raw,omitempty"`

	// type=heartbeat
	Seq     uint64 `json:"seq,omitempty"`
	Sources string `json:"sources,omitempty"`

	// Forward-compatibility: any unrecognised key from the agent is preserved.
	Extra map[string]json.RawMessage `json:"-"`
}
