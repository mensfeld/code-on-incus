"""
Enforcement tests for resource limits.

test_limits_apply.py proves the incus keys are *set* (`incus config show`
contains `limits.memory: 512MiB` etc.), but not that the kernel actually
*enforces* them. A regression that writes the right key to the wrong device,
with a wrong unit, or that the kernel silently ignores would pass every
config-string test. These tests run a workload inside the limited container and
observe the kernel-level effect:

- CPU count   → `nproc` inside reflects the pinned CPU set (behavioral).
- Memory      → the container cgroup's `memory.max` equals the limit in bytes.
                (A real OOM-on-overcommit is swap-controller-dependent and flaky
                on GitHub Actions runners, so we assert the enforced cgroup value,
                which is the kernel's actual hard limit — "max"/unlimited there is
                exactly the not-enforced regression.)
- Processes   → forking past the limit raises EAGAIN (behavioral).

All use `coi run --workspace <dir> -- <cmd>`, which creates the container with
the config's limits applied and runs the command under them.
"""

import subprocess
from pathlib import Path


def _write_limits_config(workspace_dir, limits_toml):
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text("[container]\npersistent = true\n\n" + limits_toml)


def test_cpu_count_limit_is_enforced(coi_binary, workspace_dir, cleanup_containers):
    """With [limits.cpu] count = "1", the container sees exactly 1 CPU.

    incus applies an integer limits.cpu as a cpuset, so sched_getaffinity (what
    `nproc` reads) is restricted — a value the kernel enforces, not just config.
    GitHub Actions runners have >1 CPU, so nproc==1 is a real restriction there.
    """
    _write_limits_config(workspace_dir, '[limits.cpu]\ncount = "1"\n')

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "bash",
            "-c",
            "echo NPROC=$(nproc)",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )
    combined = result.stdout + result.stderr
    assert "NPROC=1" in combined, (
        "limits.cpu count=1 must restrict the container to 1 CPU (nproc==1), not just "
        f"set the incus key. Got:\n{combined}"
    )


def test_memory_limit_is_enforced(coi_binary, workspace_dir, cleanup_containers):
    """With [limits.memory] max = "512MiB", the cgroup memory.max equals 512 MiB.

    memory.max is the kernel's hard limit; reading it from inside proves the
    container's cgroup is actually constrained. If the incus key were set but not
    enforced, memory.max would read "max" (unlimited) — the exact regression this
    guards. 512 MiB = 512*1024*1024 = 536870912 bytes.
    """
    _write_limits_config(workspace_dir, '[limits.memory]\nmax = "512MiB"\n')

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "bash",
            "-c",
            "echo MEMMAX=$(cat /sys/fs/cgroup/memory.max)",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )
    combined = result.stdout + result.stderr
    assert "MEMMAX=536870912" in combined, (
        "limits.memory max=512MiB must constrain the container cgroup "
        f"(memory.max == 536870912 bytes), not read 'max'/unlimited. Got:\n{combined}"
    )


def test_process_limit_is_enforced(coi_binary, workspace_dir, cleanup_containers):
    """With [limits.runtime] max_processes = 100, forking past the cap raises EAGAIN.

    limits.processes maps to the cgroup pids.max. A process that forks in a loop
    hits the ceiling and fork() fails with EAGAIN (BlockingIOError) — a kernel
    refusal, the true proof of enforcement. 100 is well above the container's
    systemd baseline so it boots, but low enough that the loop reaches it. The
    forked children just sleep and are reaped when the container is deleted.
    """
    _write_limits_config(workspace_dir, "[limits.runtime]\nmax_processes = 100\n")

    fork_probe = (
        "import os, time\n"
        "blocked = False\n"
        "for _ in range(1000):\n"
        "    try:\n"
        "        pid = os.fork()\n"
        "    except OSError:\n"  # EAGAIN (BlockingIOError) once pids.max is hit
        "        blocked = True\n"
        "        break\n"
        "    if pid == 0:\n"
        "        time.sleep(300)\n"
        "        os._exit(0)\n"
        "print('FORK_BLOCKED' if blocked else 'NO_LIMIT_HIT')\n"
    )

    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "python3", "-c", fork_probe],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )
    combined = result.stdout + result.stderr
    assert "FORK_BLOCKED" in combined, (
        "limits.runtime max_processes=100 must cap the container's pids.max so fork() "
        f"eventually fails with EAGAIN, not just set the incus key. Got:\n{combined}"
    )
