"""
Test that the container's hostname is present in /etc/hosts.

Incus assigns the hostname at boot; /etc/hosts is baked into the image at build
time with a different name. Without adding the runtime hostname, sudo prints
"unable to resolve host <name>" on every invocation. The fix lives in build.sh
(the coi-fix-hostname.service oneshot), and the CI image cache key includes
internal/image/**, so the built image carries the fix — this test runs for real.
"""

import subprocess


def test_hostname_in_hosts(coi_binary, cleanup_containers, workspace_dir):
    """
    Verify the container hostname resolves via /etc/hosts.

    The container command's output arrives on stdout; coi's own progress/cleanup
    logs ("Launching container ...", "Cleaning up container ...") go to stderr, so
    stderr must NOT be parsed as command output — doing so previously made this
    test read a log line as the "hostname".
    """
    # Step 1: read the Incus-assigned hostname from the command's stdout only.
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "hostname"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"coi run hostname failed: {result.stderr}"

    stdout_lines = [line.strip() for line in result.stdout.splitlines() if line.strip()]
    assert stdout_lines, (
        f"coi run hostname produced no stdout.\nstdout: {result.stdout!r}\nstderr: {result.stderr!r}"
    )
    hostname = stdout_lines[-1]
    # The container hostname is its Incus instance name (coi-<hash>-<slot>). This
    # guard also fails loudly if extraction ever regresses to a coi log line
    # (e.g. "Cleaning up container ...", which does not start with "coi-").
    assert hostname.startswith("coi-"), (
        f"expected a coi-* container hostname, got {hostname!r}\nstdout: {result.stdout!r}"
    )

    # Step 2: confirm it resolves via /etc/hosts inside the container. grep's
    # matched line is on stdout; require both a zero exit (a match) and the
    # hostname in stdout (not stderr, whose cleanup log also contains the name).
    result2 = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "grep", hostname, "/etc/hosts"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result2.returncode == 0 and hostname in result2.stdout, (
        f"Hostname '{hostname}' not found in /etc/hosts (grep exit {result2.returncode}).\n"
        f"grep stdout: {result2.stdout!r}\n"
        "The coi-fix-hostname.service (build.sh) may not have run correctly."
    )
