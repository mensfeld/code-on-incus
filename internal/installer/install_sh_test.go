package installer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// installShPath returns the absolute path to install.sh at the repo root.
func installShPath(t *testing.T) string {
	t.Helper()
	// Walk up from this test file to repo root
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Join(wd, "..", "..")
	path := filepath.Join(root, "install.sh")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("install.sh not found at %s: %v", path, err)
	}
	return path
}

// runBashSnippet runs a bash snippet that sources the install.sh functions and
// returns stdout, stderr, and the exit code. The snippet runs with
// NONINTERACTIVE=1 by default unless overridden via env.
func runBashSnippet(t *testing.T, snippet string, env ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command("bash", "-c", snippet)
	cmd.Env = append(os.Environ(), env...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run snippet: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// When stdin is not a terminal (piped input), install.sh should detect non-interactive
// mode automatically via the `! [ -t 0 ]` check in the NONINTERACTIVE detection block.
func TestInstallSh_DetectsNonInteractiveWhenPiped(t *testing.T) {
	script := installShPath(t)

	// Source install.sh functions and check the NONINTERACTIVE variable.
	// We pipe through bash (simulating curl|bash) so stdin is not a TTY.
	snippet := `
		# Source just the variable/function definitions (stop before main runs)
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		echo "NONINTERACTIVE=$NONINTERACTIVE"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	if !strings.Contains(stdout, "NONINTERACTIVE=1") {
		t.Errorf("expected NONINTERACTIVE=1 when piped, got: %s", strings.TrimSpace(stdout))
	}
}

// When NONINTERACTIVE=1 is set explicitly, prompt_continue should abort with a
// clear message instead of trying to read from the terminal.
func TestInstallSh_PromptContinueAbortsNonInteractive(t *testing.T) {
	script := installShPath(t)

	snippet := `
		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		prompt_continue "Continue anyway?"
		echo "SHOULD_NOT_REACH"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1")
	if exitCode == 0 {
		t.Errorf("expected non-zero exit code from prompt_continue in non-interactive mode")
	}
	if strings.Contains(stdout, "SHOULD_NOT_REACH") {
		t.Errorf("prompt_continue should have exited, but execution continued")
	}
	if !strings.Contains(stdout, "Non-interactive mode") {
		t.Errorf("expected non-interactive warning message, got: %s", stdout)
	}
}

// When NONINTERACTIVE=1 is set, prompt_choice should return the default value
// without attempting to read from the terminal.
func TestInstallSh_PromptChoiceUsesDefaultNonInteractive(t *testing.T) {
	script := installShPath(t)

	snippet := `
		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		prompt_choice "Pick one: " "1"
		echo "REPLY=$REPLY"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	if !strings.Contains(stdout, "REPLY=1") {
		t.Errorf("expected REPLY=1 (default), got: %s", strings.TrimSpace(stdout))
	}
}

// When CI=true is set, install.sh should detect non-interactive mode even
// without piped stdin or explicit NONINTERACTIVE flag.
func TestInstallSh_DetectsCI(t *testing.T) {
	script := installShPath(t)

	snippet := `
		export CI=true
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		echo "NONINTERACTIVE=$NONINTERACTIVE"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "CI=true")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	if !strings.Contains(stdout, "NONINTERACTIVE=1") {
		t.Errorf("expected NONINTERACTIVE=1 with CI=true, got: %s", strings.TrimSpace(stdout))
	}
}

// In non-interactive mode, check_firewalld should complete without hanging
// (it should skip the interactive prompts and return cleanly).
func TestInstallSh_FirewalldAutoSetup_NonInteractive(t *testing.T) {
	script := installShPath(t)

	snippet := `
		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		check_firewalld
		echo "COMPLETED"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1")
	if exitCode != 0 {
		t.Fatalf("check_firewalld exited with %d in non-interactive mode; stdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "COMPLETED") {
		t.Errorf("check_firewalld did not complete in non-interactive mode; stdout: %s", stdout)
	}
	// Should not contain any prompt text
	if strings.Contains(stdout, "[y/N]") {
		t.Errorf("check_firewalld tried to prompt in non-interactive mode; stdout: %s", stdout)
	}
	t.Logf("check_firewalld non-interactive output:\n%s", stdout)
}

// prompt_choice should use a non-default value ("2") when that is passed as default.
func TestInstallSh_PromptChoiceCustomDefault(t *testing.T) {
	script := installShPath(t)

	snippet := `
		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		prompt_choice "Pick one: " "2"
		echo "REPLY=$REPLY"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	if !strings.Contains(stdout, "REPLY=2") {
		t.Errorf("expected REPLY=2, got: %s", strings.TrimSpace(stdout))
	}
}

// When incus is not installed, ensure_incus_initialized should be a silent no-op.
func TestInstallSh_EnsureIncusInitialized_SkipsWhenIncusMissing(t *testing.T) {
	script := installShPath(t)

	// Use a PATH that definitely does not contain incus
	snippet := `
		export NONINTERACTIVE=1
		export PATH=/usr/bin:/bin
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		ensure_incus_initialized
		echo "EXIT=$?"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1", "PATH=/usr/bin:/bin")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "EXIT=0") {
		t.Errorf("expected clean exit, got: %s", strings.TrimSpace(stdout))
	}
	// Should produce no output (silent skip)
	if strings.Contains(stdout, "Incus") {
		t.Errorf("expected no Incus messages when incus is missing, got: %s", stdout)
	}
}

// When incus reports networks exist, ensure_incus_initialized should be a silent no-op
// (Incus is already initialized).
func TestInstallSh_EnsureIncusInitialized_SkipsWhenAlreadyInitialized(t *testing.T) {
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping")
	}

	script := installShPath(t)

	// Stub incus: when called with "network list", return a fake network line.
	// This simulates an already-initialized Incus without needing a real one.
	snippet := `
		tmpdir=$(mktemp -d)
		trap "rm -rf $tmpdir" EXIT

		cat > "$tmpdir/incus" <<'STUB'
#!/bin/bash
if [[ "$*" == *"network list"* ]]; then
	echo "incusbr0,bridge,,"
	exit 0
fi
exit 0
STUB
		chmod +x "$tmpdir/incus"
		export PATH="$tmpdir:$PATH"

		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		ensure_incus_initialized
		echo "COMPLETED"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "COMPLETED") {
		t.Errorf("function did not complete; stdout: %s", stdout)
	}
	// Should NOT attempt initialization
	if strings.Contains(stdout, "has not been initialized") {
		t.Errorf("should skip when networks exist, got: %s", stdout)
	}
}

// When incus reports no networks, ensure_incus_initialized should run
// `sudo incus admin init --auto` and print a styled success message.
func TestInstallSh_EnsureIncusInitialized_RunsInitWhenNoNetworks(t *testing.T) {
	script := installShPath(t)

	// Stub both incus and sudo:
	// - incus network list returns empty (no networks → not initialized)
	// - sudo incus admin init --auto succeeds
	snippet := `
		tmpdir=$(mktemp -d)
		trap "rm -rf $tmpdir" EXIT

		cat > "$tmpdir/incus" <<'STUB'
#!/bin/bash
if [[ "$*" == *"network list"* ]]; then
	# Empty output = no networks
	exit 0
fi
exit 0
STUB
		chmod +x "$tmpdir/incus"

		export COI_TEST_MARKER="$tmpdir/init_called"
		cat > "$tmpdir/sudo" <<'STUB'
#!/bin/bash
if [[ "$*" == *"incus admin init --auto"* ]]; then
	touch "$COI_TEST_MARKER"
	exit 0
fi
# Pass through other sudo calls
exec /usr/bin/sudo "$@"
STUB
		chmod +x "$tmpdir/sudo"
		export PATH="$tmpdir:$PATH"

		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		ensure_incus_initialized
		# Check the marker file
		if [ -f "$COI_TEST_MARKER" ]; then
			echo "INIT_WAS_CALLED"
		fi
		echo "COMPLETED"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "COMPLETED") {
		t.Errorf("function did not complete; stdout: %s", stdout)
	}
	// Should print the initialization message
	if !strings.Contains(stdout, "has not been initialized") {
		t.Errorf("expected 'has not been initialized' message, got: %s", stdout)
	}
	if !strings.Contains(stdout, "Incus initialized") {
		t.Errorf("expected success message, got: %s", stdout)
	}
	// Verify sudo was called with the right command
	if !strings.Contains(stdout, "INIT_WAS_CALLED") {
		t.Errorf("expected sudo incus admin init --auto to be called; stdout: %s", stdout)
	}
}

// When `sudo incus admin init --auto` fails, ensure_incus_initialized should
// print a warning with the error output and return non-zero.
func TestInstallSh_EnsureIncusInitialized_HandlesInitFailure(t *testing.T) {
	script := installShPath(t)

	snippet := `
		tmpdir=$(mktemp -d)
		trap "rm -rf $tmpdir" EXIT

		cat > "$tmpdir/incus" <<'STUB'
#!/bin/bash
if [[ "$*" == *"network list"* ]]; then
	exit 0
fi
exit 0
STUB
		chmod +x "$tmpdir/incus"

		cat > "$tmpdir/sudo" <<'STUB'
#!/bin/bash
if [[ "$*" == *"incus admin init --auto"* ]]; then
	echo "Error: something went wrong"
	exit 1
fi
exec /usr/bin/sudo "$@"
STUB
		chmod +x "$tmpdir/sudo"
		export PATH="$tmpdir:$PATH"

		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		ensure_incus_initialized
		echo "SHOULD_NOT_REACH"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1")
	if exitCode == 0 {
		t.Errorf("expected non-zero exit from init failure, got 0; stdout: %s", stdout)
	}
	if strings.Contains(stdout, "SHOULD_NOT_REACH") {
		t.Errorf("function should have returned non-zero; stdout: %s", stdout)
	}
	if !strings.Contains(stdout, "initialization failed") {
		t.Errorf("expected failure warning, got: %s", stdout)
	}
	if !strings.Contains(stdout, "something went wrong") {
		t.Errorf("expected error output to be shown, got: %s", stdout)
	}
}

// When `incus network list` itself fails (daemon down, permission denied),
// ensure_incus_initialized should warn and return non-zero instead of
// incorrectly triggering init.
func TestInstallSh_EnsureIncusInitialized_WarnsOnQueryFailure(t *testing.T) {
	script := installShPath(t)

	snippet := `
		tmpdir=$(mktemp -d)
		trap "rm -rf $tmpdir" EXIT

		cat > "$tmpdir/incus" <<'STUB'
#!/bin/bash
if [[ "$*" == *"network list"* ]]; then
	echo "Error: not authorized" >&2
	exit 1
fi
exit 0
STUB
		chmod +x "$tmpdir/incus"
		export PATH="$tmpdir:$PATH"

		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		ensure_incus_initialized
		echo "SHOULD_NOT_REACH"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1")
	if exitCode == 0 {
		t.Errorf("expected non-zero exit on query failure, got 0; stdout: %s", stdout)
	}
	if strings.Contains(stdout, "SHOULD_NOT_REACH") {
		t.Errorf("function should have returned non-zero; stdout: %s", stdout)
	}
	// Should warn about inability to query, NOT attempt init
	if !strings.Contains(stdout, "Unable to determine") {
		t.Errorf("expected query failure warning, got: %s", stdout)
	}
	if strings.Contains(stdout, "has not been initialized") {
		t.Errorf("should NOT attempt init when query fails; stdout: %s", stdout)
	}
}

// Verify that `incus storage create` output is captured (not printed to terminal)
// on success.
func TestInstallSh_SetupZfsStorage_SuppressesOutputOnSuccess(t *testing.T) {
	script := installShPath(t)

	// Stub incus and sudo to simulate successful storage + profile setup.
	// The key test: sudo incus storage create prints a hint that should NOT
	// appear in output.
	snippet := `
		tmpdir=$(mktemp -d)
		trap "rm -rf $tmpdir" EXIT

		cat > "$tmpdir/incus" <<'STUB'
#!/bin/bash
if [[ "$*" == *"storage list"* ]]; then
	# No zfs-pool exists yet
	echo ""
	exit 0
fi
if [[ "$*" == *"profile device set"* ]]; then
	exit 0
fi
exit 0
STUB
		chmod +x "$tmpdir/incus"

		cat > "$tmpdir/sudo" <<'STUB'
#!/bin/bash
if [[ "$*" == *"storage create"* ]]; then
	echo "If this is your first time running Incus, you should also run: incus admin init"
	exit 0
fi
exec /usr/bin/sudo "$@"
STUB
		chmod +x "$tmpdir/sudo"

		cat > "$tmpdir/zfs" <<'STUB'
#!/bin/bash
exit 0
STUB
		chmod +x "$tmpdir/zfs"

		export PATH="$tmpdir:$PATH"
		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		setup_zfs_storage
		echo "COMPLETED"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "COMPLETED") {
		t.Errorf("function did not complete; stdout: %s", stdout)
	}
	// The raw Incus hint should NOT appear in output
	if strings.Contains(stdout, "first time running Incus") {
		t.Errorf("raw Incus hint should be suppressed on success, got: %s", stdout)
	}
	// Success messages should appear
	if !strings.Contains(stdout, "ZFS storage pool created") {
		t.Errorf("expected success message, got: %s", stdout)
	}
}

// When ufw is not installed, check_ufw should be a silent no-op.
func TestInstallSh_CheckUfw_SkipsWhenUfwAbsent(t *testing.T) {
	script := installShPath(t)

	snippet := `
		tmpdir=$(mktemp -d)
		trap "rm -rf $tmpdir" EXIT

		# PATH without ufw
		export PATH=/usr/bin:/bin
		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		check_ufw
		echo "SKIP_FIREWALLD=${SKIP_FIREWALLD:-0}"
		echo "COMPLETED"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1", "PATH=/usr/bin:/bin")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "COMPLETED") {
		t.Errorf("function did not complete; stdout: %s", stdout)
	}
	if !strings.Contains(stdout, "SKIP_FIREWALLD=0") {
		t.Errorf("SKIP_FIREWALLD should be 0 when ufw absent, got: %s", stdout)
	}
}

// When ufw is installed but inactive, check_ufw should print info and not set SKIP_FIREWALLD.
func TestInstallSh_CheckUfw_SkipsWhenUfwInactive(t *testing.T) {
	script := installShPath(t)

	snippet := `
		tmpdir=$(mktemp -d)
		trap "rm -rf $tmpdir" EXIT

		cat > "$tmpdir/ufw" <<'STUB'
#!/bin/bash
echo "Status: inactive"
exit 0
STUB
		chmod +x "$tmpdir/ufw"

		# Stub systemctl to report ufw as inactive
		cat > "$tmpdir/systemctl" <<'STUB'
#!/bin/bash
if [[ "$1" == "is-active" && "$*" == *"ufw"* ]]; then
	exit 1
fi
exec /usr/bin/systemctl "$@"
STUB
		chmod +x "$tmpdir/systemctl"
		export PATH="$tmpdir:$PATH"

		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		check_ufw
		echo "SKIP_FIREWALLD=${SKIP_FIREWALLD:-0}"
		echo "COMPLETED"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "COMPLETED") {
		t.Errorf("function did not complete; stdout: %s", stdout)
	}
	if !strings.Contains(stdout, "SKIP_FIREWALLD=0") {
		t.Errorf("SKIP_FIREWALLD should be 0 when ufw inactive, got: %s", stdout)
	}
	if !strings.Contains(stdout, "inactive") {
		t.Errorf("expected info about ufw being inactive, got: %s", stdout)
	}
}

// In non-interactive mode with active ufw, check_ufw should exit non-zero.
func TestInstallSh_CheckUfw_NonInteractiveExitsOnActiveUfw(t *testing.T) {
	script := installShPath(t)

	snippet := `
		tmpdir=$(mktemp -d)
		trap "rm -rf $tmpdir" EXIT

		cat > "$tmpdir/ufw" <<'STUB'
#!/bin/bash
echo "Status: active"
exit 0
STUB
		chmod +x "$tmpdir/ufw"

		# Stub systemctl to report ufw as active
		cat > "$tmpdir/systemctl" <<'STUB'
#!/bin/bash
if [[ "$1" == "is-active" && "$*" == *"ufw"* ]]; then
	exit 0
fi
exec /usr/bin/systemctl "$@"
STUB
		chmod +x "$tmpdir/systemctl"
		export PATH="$tmpdir:$PATH"

		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")
		check_ufw
		echo "SHOULD_NOT_REACH"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=1")
	if exitCode == 0 {
		t.Errorf("expected non-zero exit from check_ufw with active ufw in non-interactive mode")
	}
	if strings.Contains(stdout, "SHOULD_NOT_REACH") {
		t.Errorf("check_ufw should have exited, but execution continued")
	}
	if !strings.Contains(stdout, "ufw is active") {
		t.Errorf("expected ufw active warning, got: %s", stdout)
	}
}

// When the user picks option 2 (skip firewalld), SKIP_FIREWALLD should be set to 1.
// We test the real check_ufw by running in NONINTERACTIVE=1 mode and overriding
// prompt_choice to return "2" instead of exiting.
func TestInstallSh_CheckUfw_Option2SkipsFirewalld(t *testing.T) {
	script := installShPath(t)

	snippet := `
		tmpdir=$(mktemp -d)
		trap "rm -rf $tmpdir" EXIT

		cat > "$tmpdir/ufw" <<'STUB'
#!/bin/bash
echo "Status: active"
exit 0
STUB
		chmod +x "$tmpdir/ufw"

		# Stub systemctl to report ufw as active
		cat > "$tmpdir/systemctl" <<'STUB'
#!/bin/bash
if [[ "$1" == "is-active" && "$*" == *"ufw"* ]]; then
	exit 0
fi
if [[ "$1" == "disable" ]]; then
	exit 0
fi
exec /usr/bin/systemctl "$@"
STUB
		chmod +x "$tmpdir/systemctl"

		cat > "$tmpdir/sudo" <<'STUB'
#!/bin/bash
if [[ "$*" == *"ufw disable"* ]]; then
	exit 0
fi
exec /usr/bin/sudo "$@"
STUB
		chmod +x "$tmpdir/sudo"
		export PATH="$tmpdir:$PATH"

		export NONINTERACTIVE=1
		source <(sed '/^main "\$@"/d; /^trap error_handler ERR/d' "` + script + `")

		# Override NONINTERACTIVE after sourcing so check_ufw reaches prompt_choice
		NONINTERACTIVE=0

		# Override prompt_choice to simulate user picking option 2
		prompt_choice() {
			REPLY="2"
		}

		check_ufw
		echo "SKIP_FIREWALLD=${SKIP_FIREWALLD:-0}"
	`
	stdout, _, exitCode := runBashSnippet(t, snippet, "NONINTERACTIVE=0")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "SKIP_FIREWALLD=1") {
		t.Errorf("expected SKIP_FIREWALLD=1 after option 2, got: %s", stdout)
	}
}
