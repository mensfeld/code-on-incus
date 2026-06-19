package network

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/logger"
)

// Regression test for issue #372: network diagnostics (the background IP-refresh
// goroutine in particular) must go to the session log files, not stderr — which
// in a coi shell is the user's tmux terminal.
func TestNetworkLogging_RoutesToSessionLoggerNotStderr(t *testing.T) {
	// Capture the standard logger's output (its default sink is os.Stderr).
	var stdlog bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&stdlog)
	t.Cleanup(func() { log.SetOutput(orig) })

	// Default (no session logger): falls back to the standard logger.
	SetLogger(nil)
	logWarnf("fallback warning %d", 1)
	logInfof("fallback info %d", 2)
	if !strings.Contains(stdlog.String(), "fallback warning 1") {
		t.Errorf("expected fallback to standard logger, got: %q", stdlog.String())
	}

	// With a session logger set, output must go to the session files and NOT to
	// the standard logger / stderr.
	tmp := t.TempDir()
	sl := logger.New("netlog-test", tmp)
	SetLogger(sl)
	t.Cleanup(func() { SetLogger(nil) })

	stdlog.Reset()
	logInfof("routed info %d", 3)
	logWarnf("routed warning %d", 4)
	_ = sl.Close()

	if stdlog.Len() != 0 {
		t.Errorf("network output leaked to stderr/standard logger: %q", stdlog.String())
	}

	logsDir := filepath.Join(tmp, ".coi", "logs")
	out, _ := os.ReadFile(filepath.Join(logsDir, "netlog-test.stdout.log"))
	if !strings.Contains(string(out), "routed info 3") {
		t.Errorf("info message not routed to session stdout log; got: %q", string(out))
	}
	errf, _ := os.ReadFile(filepath.Join(logsDir, "netlog-test.stderr.log"))
	if !strings.Contains(string(errf), "routed warning 4") {
		t.Errorf("warning message not routed to session stderr log; got: %q", string(errf))
	}
}
