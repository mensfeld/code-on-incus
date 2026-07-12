package session

import (
	"errors"
	"testing"
	"time"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// fakeProber scripts successive probe answers; Running answers come from a
// function of the call index so tests can flip state mid-flight.
type fakeProber struct {
	outs     []string
	errs     []error
	execs    int
	running  func(call int) (bool, error)
	runCalls int
}

func (f *fakeProber) ExecCommand(_ string, _ container.ExecCommandOptions) (string, error) {
	i := f.execs
	f.execs++
	var out string
	var err error
	if i < len(f.outs) {
		out = f.outs[i]
	}
	if i < len(f.errs) {
		err = f.errs[i]
	}
	return out, err
}

func (f *fakeProber) Running() (bool, error) {
	i := f.runCalls
	f.runCalls++
	if f.running == nil {
		return true, nil
	}
	return f.running(i)
}

func busDownErr() error {
	return &container.ExitError{ExitCode: 1, Stderr: "Failed to connect to bus: Host is down"}
}

func noSystemdErr() error {
	return &container.ExitError{ExitCode: 127, Stderr: "bash: line 1: systemctl: command not found"}
}

func TestGuestShutdownInProgress_Stopping(t *testing.T) {
	f := &fakeProber{outs: []string{"stopping"}}
	if !guestShutdownInProgress(f) {
		t.Error("'stopping' must classify as shutdown in progress")
	}
	if f.execs != 1 {
		t.Errorf("one probe should suffice, got %d", f.execs)
	}
}

func TestGuestShutdownInProgress_HealthyStatesAreUserExit(t *testing.T) {
	for _, state := range []string{"running", "degraded", "maintenance", "initializing", "starting"} {
		f := &fakeProber{outs: []string{state}}
		if guestShutdownInProgress(f) {
			t.Errorf("%q must classify as a normal exit", state)
		}
		if f.execs != 1 {
			t.Errorf("%q: one probe should suffice, got %d", state, f.execs)
		}
	}
}

func TestGuestShutdownInProgress_BusDownIsShutdown(t *testing.T) {
	// systemctl ran but systemd's bus is gone while the container still
	// reports Running: the shutdown tail.
	f := &fakeProber{outs: []string{""}, errs: []error{busDownErr()}}
	if !guestShutdownInProgress(f) {
		t.Error("an answered-but-bus-down probe must classify as shutdown")
	}
}

func TestGuestShutdownInProgress_NoSystemdFastExit(t *testing.T) {
	// An image that can never answer must not burn the retry budget on
	// every session exit.
	f := &fakeProber{outs: []string{""}, errs: []error{noSystemdErr()}}
	start := time.Now()
	if guestShutdownInProgress(f) {
		t.Error("missing systemctl must classify as a normal exit")
	}
	if f.execs != 1 {
		t.Errorf("no-systemd must be decided on the first probe, got %d probes", f.execs)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("no-systemd classification took %v, must not sleep", elapsed)
	}
}

func TestGuestShutdownInProgress_AmbiguousThenStopped(t *testing.T) {
	// "unknown"/"offline" (late-poweroff tail) is ambiguous; the container
	// stopping during the retry window resolves it as a shutdown.
	f := &fakeProber{
		outs:    []string{"unknown", "offline"},
		running: func(call int) (bool, error) { return call < 1, nil },
	}
	if !guestShutdownInProgress(f) {
		t.Error("ambiguous states with the container then stopping must classify as shutdown")
	}
}

func TestGuestShutdownInProgress_TransportErrorOnLiveContainer(t *testing.T) {
	// Exec yields nothing, container stays verifiably running: user exit
	// (after the bounded retries) — a broken exec must not delete containers.
	f := &fakeProber{errs: []error{errors.New("x"), errors.New("x"), errors.New("x"), errors.New("x"), errors.New("x"), errors.New("x")}}
	if guestShutdownInProgress(f) {
		t.Error("a persistently unanswerable probe on a running container must classify as a normal exit")
	}
}

func TestGuestShutdownInProgress_RunningErrorDoesNotCountAsStopped(t *testing.T) {
	// Incus daemon errors during the ambiguous branch must NOT be read as
	// "container stopped" (the swallowed-error chain from the review).
	f := &fakeProber{
		outs:    []string{"unknown", "unknown", "unknown", "unknown", "unknown", "unknown"},
		running: func(int) (bool, error) { return false, errors.New("incus daemon restarting") },
	}
	if guestShutdownInProgress(f) {
		t.Error("a Running() error must not be treated as evidence of a shutdown")
	}
}

func TestContainerRunning_ErrorIsUnknownNotStopped(t *testing.T) {
	f := &fakeProber{running: func(int) (bool, error) { return false, errors.New("boom") }}
	if _, ok := containerRunning(f); ok {
		t.Error("persistent Running() errors must report state as unknown")
	}

	// A transient error followed by a clean answer resolves.
	f2 := &fakeProber{running: func(call int) (bool, error) {
		if call == 0 {
			return false, errors.New("blip")
		}
		return true, nil
	}}
	running, ok := containerRunning(f2)
	if !ok || !running {
		t.Errorf("expected (running=true, ok=true) after a transient blip, got (%v, %v)", running, ok)
	}
}

func TestWaitForStopped(t *testing.T) {
	// Stops on the second poll.
	f := &fakeProber{running: func(call int) (bool, error) { return call < 1, nil }}
	stopped, interrupted := waitForStopped(f, 5*time.Second)
	if !stopped || interrupted {
		t.Errorf("expected (stopped=true, interrupted=false), got (%v, %v)", stopped, interrupted)
	}

	// Never stops: times out, and daemon errors are not "stopped".
	f2 := &fakeProber{running: func(int) (bool, error) { return false, errors.New("err") }}
	stopped, _ = waitForStopped(f2, time.Second)
	if stopped {
		t.Error("Running() errors must not satisfy the stopped condition")
	}
}
