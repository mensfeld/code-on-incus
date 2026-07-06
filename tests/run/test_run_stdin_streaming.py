"""
Two 0.10 `coi run` claims that had no end-to-end proof:

1. Piped/redirected stdin is connected, so `cat data | coi run -- ./process.sh`
   works (the container command reads the host's piped stdin).
2. Output streams live — it appears as produced, not buffered until the command
   completes.

Both are exercised against a real container, so they are integration tests
(they live in tests/run, already a CI group). `workspace_dir`/`cleanup_containers`
handle isolation + teardown.
"""

import select
import subprocess
import tempfile
import time


def test_run_reads_piped_stdin(coi_binary, cleanup_containers, workspace_dir):
    """`echo <data> | coi run -- cat` must echo the piped data back — proving the
    host's stdin is connected to the in-container command (not /dev/null)."""
    payload = "sentinel-STDIN-9f3a\nsecond-line-2b7c\n"
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "cat"],
        input=payload,
        capture_output=True,
        text=True,
        timeout=180,
    )
    assert result.returncode == 0, (
        f"coi run -- cat should succeed with piped stdin. stderr:\n{result.stderr}"
    )
    # `cat` writes stdin straight to stdout; coi's own diagnostics go to stderr,
    # so the payload must appear on stdout intact.
    assert "sentinel-STDIN-9f3a" in result.stdout and "second-line-2b7c" in result.stdout, (
        f"piped stdin was not delivered to the container command.\n"
        f"stdout:\n{result.stdout}\nstderr:\n{result.stderr}"
    )


def test_run_output_streams_before_completion(coi_binary, cleanup_containers, workspace_dir):
    """`coi run` must stream stdout live. The command prints one line, sleeps,
    then prints another; if output were buffered until exit, both lines would
    arrive together at the end. We assert the FIRST line is observed well before
    the process exits (by at least most of the sleep gap), which is impossible
    with buffer-until-completion semantics."""
    gap = 8  # seconds between the two prints
    overall_deadline = 180  # hard cap: never let a stalled read hang the CI lane
    # Capture stderr to a temp file (not DEVNULL) so a failure has coi's launch
    # diagnostics; not a PIPE, to avoid a full-pipe-buffer deadlock if unread.
    with tempfile.TemporaryFile(mode="w+") as errf:
        proc = subprocess.Popen(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "bash",
                "-c",
                f"echo FIRST_LINE_MARKER; sleep {gap}; echo SECOND_LINE_MARKER",
            ],
            stdout=subprocess.PIPE,
            stderr=errf,
            text=True,
        )
        t_first = None
        try:
            # Bounded read: select() with a shrinking deadline so a coi that
            # stalls before emitting the first line fails the test in finite time
            # (a bare `for line in proc.stdout` would block until the job timeout).
            deadline = time.monotonic() + overall_deadline
            while t_first is None and time.monotonic() < deadline:
                ready, _, _ = select.select([proc.stdout], [], [], deadline - time.monotonic())
                if not ready:
                    break
                line = proc.stdout.readline()
                if line == "":  # EOF
                    break
                if "FIRST_LINE_MARKER" in line:
                    t_first = time.monotonic()
            if t_first is None:
                errf.seek(0)
                raise AssertionError(
                    f"never saw the first stdout line within {overall_deadline}s.\n"
                    f"coi stderr:\n{errf.read()}"
                )
            rc = proc.wait(timeout=deadline - time.monotonic())
            t_exit = time.monotonic()
        finally:
            if proc.poll() is None:
                proc.kill()
                proc.wait()
            proc.stdout.close()

        errf.seek(0)
        stderr_text = errf.read()

    assert rc == 0, f"the streamed command should exit 0, got {rc}.\ncoi stderr:\n{stderr_text}"
    # If output were buffered until the command finished, the first line and the
    # process exit would be near-simultaneous (delta ~0). Live streaming means the
    # first line arrives, THEN the sleep+second-line+teardown elapse before exit.
    delta = t_exit - t_first
    assert delta >= gap * 0.5, (
        f"first stdout line arrived only {delta:.1f}s before exit (sleep gap was "
        f"{gap}s) — output appears buffered until completion, not streamed live"
    )
