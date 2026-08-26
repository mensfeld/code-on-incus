<p align="center">
  <img src="misc/logo.png" alt="Code on Incus Logo" width="350">
</p>

# code-on-incus (`coi`)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/mensfeld/code-on-incus)](https://golang.org/)
[![Latest Release](https://img.shields.io/github/v/release/mensfeld/code-on-incus)](https://github.com/mensfeld/code-on-incus/releases)
[![Join the chat at https://slack.karafka.io](https://raw.githubusercontent.com/karafka/misc/master/slack.svg)](https://slack.karafka.io)

**Give every AI coding agent its own machine - with active defense.**

`coi` runs your AI coding tool (Claude Code, Codex, opencode, pi) inside its own full Linux machine: root access, systemd, Docker, install anything. The agent works like it would on a real server - but it can't touch your host, can't see your credentials, and if it does something dangerous, `coi` pauses or kills the container on its own.

One command drops you into a coding session. Your project is mounted, file permissions just work, and your SSH keys, tokens, and environment variables never enter the container unless you explicitly say so.

Built by developers, for developers who run AI agents and want to know what those agents are doing. Not a product, not a startup - a tool that does the job.

<p align="center">
  <a href="https://www.youtube.com/watch?v=t78-JUnTK5Q">
    <img src="https://img.youtube.com/vi/t78-JUnTK5Q/maxresdefault.jpg" alt="BetterStack video about Code on Incus" width="600">
  </a>
  <br>
  <em>Watch the BetterStack video about Code on Incus</em>
</p>

![Demo](misc/demo.gif)

## Get started in three commands

```bash
# 1. Install
curl -fsSL https://raw.githubusercontent.com/mensfeld/code-on-incus/master/install.sh | bash

# 2. Build the base image (first time only, ~5-10 min)
coi build

# 3. Start coding - from any project directory
cd your-project
coi shell
```

That's it. Your agent is now running in an isolated container with your project at `/workspace`, correct file ownership (no more `chown`), Docker and `gh` available inside, every workspace change saved back to the host - and **no** access to your host SSH keys, env vars, or credentials.

> Requires Linux with [Incus](https://linuxcontainers.org/incus/docs/main/installing/) (macOS works too, via Colima/Lima - see [macOS Setup](https://github.com/mensfeld/code-on-incus/wiki/macOS-Setup-Guide)).

## Who it's for

- You run AI coding agents and want them to have **full machine access** - root, Docker, package managers, services - without risking your host.
- You want to **know when an agent does something suspicious**, not find out after the fact.
- You run **multiple agents in parallel** and need them isolated from each other.
- You want **persistent dev environments** that survive restarts, not throwaway containers that lose your setup every time.
- You care about your **credentials never ending up** inside an agent-controlled environment.

## What makes it different

- **A real machine, not a locked box.** Incus *system* containers run a full OS with systemd and native Docker inside. Agents install packages, run services, use cron - exactly like a server, with none of Docker's permission hell (files come out correctly owned).

- **Your credentials stay home.** SSH keys, `.env` files, Git tokens, and host environment variables are **never** exposed unless you explicitly mount them. Need to give an agent a secret? Forward a host socket or mint a short-lived token per session - the secret itself never enters the container.

- **Active defense, not just a wall.** Kernel-level monitoring catches reverse shells, C2 connections, data exfiltration, DNS tunneling, and credential scanning in real time - and **auto-pauses on HIGH, auto-kills on CRITICAL**. No babysitting.

- **Parallel agents, fully isolated.** Run several sessions on the same project at once; each slot gets its own home directory, so nothing leaks between them.

- **Your work always survives.** Containers can be ephemeral (deleted on exit) or persistent (kept with installed packages) - either way, **workspace files and session history are always saved**. Resume any session later with full conversation history and credentials restored.

### `coi` vs. the alternatives

| Capability | **code-on-incus** | Docker Sandbox | Bare Metal |
|------------|-------------------|----------------|------------|
| Credential isolation | Default (never exposed) | Partial | None |
| Real-time threat detection | Kernel-level (nftables) | No | No |
| Reverse-shell / exfil response | Auto-kill / auto-pause | No | No |
| Network isolation | nftables (3 modes) | Basic | No |
| Supply-chain protection | Git hooks / IDE configs read-only | No | No |
| Audit logging | JSONL forensics | No | No |
| Runs on Linux natively | Yes | microVM only on macOS/Windows | - |

## Profiles: your setups, one flag

Profiles are the feature you'll reach for every day. A profile is a **reusable, named container setup** - image, tool, resource limits, mounts, network mode, build scripts, and AI-agent instructions bundled into one template you can apply with a single flag.

```bash
coi shell --profile rust-dev        # spin up your Rust environment, ready to go
coi profile create rust-dev         # scaffold a new profile, then edit its config.toml
coi profile list                    # see what you've got
```

Profiles support **inheritance** (`inherits = "parent"`), ship AI-agent context files, and can carry their own build scripts - so "my hardened Python box with these limits and these tools" becomes one word.

**The killer preset: `hardened`.** Opening a repo you don't trust? One flag gives you `coi`'s strongest lockdown - restricted network (no exfil path), workspace secret masking, an ephemeral container, **no SSH-agent forwarding**, and live threat monitoring with auto-pause/kill:

```bash
coi shell --profile hardened        # inspect untrusted code safely
coi profile info hardened           # see exactly what it locks down
```

It overrides a weaker global config (a global `mode = "open"` still becomes restricted) and needs zero setup. See the [Profiles wiki page](https://github.com/mensfeld/code-on-incus/wiki/Profiles) for the full reference and schema.

## Supported AI tools

**Claude Code** (default) · **Codex CLI** · **opencode** · **pi** - pick one in config or a profile:

```toml
# ~/.coi/config.toml or ./.coi/config.toml
[tool]
name = "claude"              # or "codex", "opencode", "pi"
permission_mode = "bypass"   # run autonomously ("bypass") or ask first ("interactive")
```

_Aider and Cursor are on the way._ See the [Supported Tools wiki page](https://github.com/mensfeld/code-on-incus/wiki/Supported-Tools) for per-tool auth and configuration.

## Everyday commands

```bash
coi shell                 # interactive AI session (Claude Code by default)
coi run -- npm test       # run any command in the sandbox (streams output, propagates exit code)
coi top                   # per-container CPU/memory/IO, resolved to workspace + alias
coi monitor               # real-time security dashboard
coi list --all            # active containers + saved sessions
coi attach                # attach to a running session
coi audit                 # stream the JSONL threat-event log (pipe into a SIEM or jq)
coi shutdown / coi kill   # stop or force-kill containers
coi clean                 # remove stopped containers and orphaned resources
```

Drop a `.coi/config.toml` in any repo to auto-configure `coi` for that project - teams share one image, network mode, and limits. Run `coi <command> --help` for any command.

## Documentation

The README is the pitch; the wiki is the manual. Everything below lives there in full:

- **[Configuration](https://github.com/mensfeld/code-on-incus/wiki/Configuration)** - the complete config reference, precedence, and per-repo setup
- **[Profiles](https://github.com/mensfeld/code-on-incus/wiki/Profiles)** - reusable setups, inheritance, and the JSON schema
- **[Network Isolation](https://github.com/mensfeld/code-on-incus/wiki/Network-Isolation)** - restricted/allowlist/open modes, DNS pinning, egress and per-host port controls
- **[Security Monitoring](https://github.com/mensfeld/code-on-incus/wiki/Security-Monitoring)** & **[Audit Log](https://github.com/mensfeld/code-on-incus/wiki/Audit-Log)** - threat detection, automated response, and the event format
- **[Security Best Practices](https://github.com/mensfeld/code-on-incus/wiki/Security-Best-Practices)** - protected paths, the trust model, hardening
- **[Container Lifecycle & Sessions](https://github.com/mensfeld/code-on-incus/wiki/Container-Lifecycle-and-Sessions)** - ephemeral vs. persistent, resume, aliases
- **[Resource & Time Limits](https://github.com/mensfeld/code-on-incus/wiki/Resource-and-Time-Limits)** · **[Snapshot Management](https://github.com/mensfeld/code-on-incus/wiki/Snapshot-Management)** · **[Image Management](https://github.com/mensfeld/code-on-incus/wiki/Image-Management)**
- **[File Transfer](https://github.com/mensfeld/code-on-incus/wiki/File-Transfer)** · **[Tmux Automation](https://github.com/mensfeld/code-on-incus/wiki/Tmux-Automation)** · **[Container Operations](https://github.com/mensfeld/code-on-incus/wiki/Container-Operations)**
- **[System Health Check](https://github.com/mensfeld/code-on-incus/wiki/System-Health-Check)** - `coi health` diagnoses your setup end-to-end
- **[Troubleshooting](https://github.com/mensfeld/code-on-incus/wiki/Troubleshooting)** · **[FAQ](https://github.com/mensfeld/code-on-incus/wiki/FAQ)** · **[Migration Guide](https://github.com/mensfeld/code-on-incus/wiki/Migration-Guide)**

## Why Incus, not Docker?

Incus (a modern LXD fork) gives you **system containers** - lightweight VMs with a real init system - instead of Docker's application containers. That means one clean isolation layer running a full OS with native Docker inside, correct file ownership on the host by default, and no Docker Desktop, no vendor lock-in, no opaque VM nesting. It's Linux-native and fully open source. (More in the [FAQ](https://github.com/mensfeld/code-on-incus/wiki/FAQ).)

## Getting help

- **Slack**: [Join the COI community](https://slack.karafka.io) - ask questions, report issues, share feedback
- **GitHub Issues**: [Open an issue](https://github.com/mensfeld/code-on-incus/issues) for bugs and feature requests
- **Wiki**: [Browse the documentation](https://github.com/mensfeld/code-on-incus/wiki)
