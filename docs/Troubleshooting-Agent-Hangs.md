# Troubleshooting: Agent Hangs Completely

One of the most common causes of a coding agent freezing mid-task — with no error message, no progress, just silence — is a full `/tmp` filesystem.

## Why /tmp fills up

AI coding agents are heavy users of temporary storage. A single session can generate gigabytes in `/tmp` through:

- **npm / yarn / pnpm** — unpacking packages, caching tarballs, writing lock files
- **Cargo / Rust builds** — incremental compilation artefacts, linker inputs
- **Test runners** — coverage reports, snapshot diffs, JUnit XML output
- **TypeScript / Babel transpilation** — intermediate `.js` files, source maps
- **Docker builds** — layer staging, build context tarballs
- **Large file operations** — sort, grep, awk on big datasets write to `/tmp` by default

Inside a COI container `/tmp` is a **tmpfs** — a RAM-backed filesystem with a hard size cap. When it fills up the kernel returns `ENOSPC` ("no space left on device") to every process that tries to write. Most tools are not written to handle this gracefully: they either freeze waiting for a write that will never succeed, silently corrupt output, or crash in ways that look like unrelated errors.

## Why Linux does not auto-clean it

Ubuntu's built-in `systemd-tmpfiles-clean.timer` runs **once a day** and only removes files older than **10 days** by default. Within a single session (typically hours, not days), nothing ever ages out. There is also no back-pressure mechanism — the kernel does not evict files when space is low the way it pages out memory. It just blocks writes.

## How COI addresses this

COI applies two layers of protection:

### 1. Hard size cap (since v0.7.0)

At container startup COI mounts `/tmp` as a size-limited tmpfs device:

```toml
# ~/.config/coi/config.toml or .coi.toml in your project
[limits.disk]
tmpfs_size = "4GiB"   # default — increase if your builds need more
```

This prevents a single session from consuming all available RAM regardless of how much the agent writes.

### 2. Periodic auto-cleanup (since v0.7.0)

The COI base image ships with:

- **`/etc/tmpfiles.d/coi-tmp-cleanup.conf`** — marks files in `/tmp` for deletion after they have not been accessed for **1 hour**
- **`/etc/systemd/system/systemd-tmpfiles-clean.timer.d/coi-interval.conf`** — overrides the cleanup timer to run every **15 minutes** instead of daily

Together these ensure that abandoned temp files (e.g. from a cancelled build or a previous tool invocation) are reclaimed automatically, so `/tmp` self-heals between heavy operations.

## Symptoms of a full /tmp

| Symptom | What is actually happening |
|---|---|
| Agent produces no output for 5–30 minutes | A write to `/tmp` is blocking waiting for space |
| `npm install` hangs after "Resolving packages" | npm is writing package tarballs to a cache in `/tmp` |
| `cargo build` hangs at a specific crate | The linker is writing a staging file |
| `pytest` starts but never prints a result | Coverage or JUnit output writer is blocked |
| Agent suddenly dies with a confusing error | Tool wrote a partial file and crashed when flush failed |

## What to do if an agent is already hanging

1. **Check /tmp usage** from outside the container:
   ```bash
   incus exec <container-name> -- df -h /tmp
   ```

2. **Find the large files:**
   ```bash
   incus exec <container-name> -- du -sh /tmp/* 2>/dev/null | sort -rh | head -20
   ```

3. **Free space without killing the session:**
   ```bash
   incus exec <container-name> -- find /tmp -maxdepth 1 -type d -name 'npm-*' -exec rm -rf {} +
   incus exec <container-name> -- find /tmp -maxdepth 1 -size +100M -exec rm {} +
   ```

4. **Increase the cap permanently** in your project config:
   ```toml
   # .coi.toml
   [limits.disk]
   tmpfs_size = "8GiB"
   ```
   This takes effect the next time the container starts.

## FAQ

**Why not just make /tmp unlimited?**

Tmpfs is backed by RAM and swap. An unlimited `/tmp` means a runaway build can exhaust all system memory, hang the host, or trigger the OOM killer on unrelated processes.

**Why not bind-mount a real disk directory to /tmp?**

A real disk `/tmp` removes the size risk but makes all temporary writes slower (especially for tools that use `/tmp` as a high-frequency scratch space) and means temp files survive container restarts, accumulating over time.

**Can I point a specific tool away from /tmp?**

Yes. Most tools respect `TMPDIR`:

```bash
export TMPDIR=/workspace/.tmp
mkdir -p "$TMPDIR"
```

Setting `TMPDIR` to a path inside `/workspace` (which is bind-mounted from your host) moves temp writes off the size-limited tmpfs entirely. The trade-off is that temp files will persist after the session unless you clean them up.
