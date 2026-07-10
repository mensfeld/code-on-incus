# Credential Catalog & Generic `[[credentials]]` Seeding

**Status:** Implemented — shipped in v0.10.0 (PR #573)
**Related:** [#549](https://github.com/mensfeld/code-on-incus/issues/549)

## Problem

Host-credential seeding into containers (config files, auth tokens, SSH keys)
only works for the three tools registered in `internal/tool/registry.go`
(Claude, OpenCode, Pi), via the `ToolWithConfigDirFiles` interface and
`setupCLIConfig`. There's no way for a profile — or a third-party integration
like Ollama — to say "also seed this host path into every new container"
without writing Go and rebuilding `coi`.

The motivating case: a custom profile installs `ollama` alongside Claude Code
so `ollama launch claude --model <cloud-model>` can run against Ollama Cloud
inside a container. That needs the container to have the same signed-in
identity as the host — an SSH keypair at `~/.ollama/id_ed25519` — copied in
and chowned to the container's `code` user (bind-mounting doesn't work here;
the container-side UID isn't guaranteed to match the host's).

Note: Ollama itself is not a launchable `Tool` in coi's sense — it has no
`BuildCommand`, it's not what gets invoked. It's a credential-bearing
*provider* that needs to be present alongside whichever real tool (Claude, in
this case) is running. So this isn't "register a 4th `Tool`" — it's "seed
extra credential material for something that never appears in the tool
registry at all, regardless of which tool is selected."

## Non-goals

- Not making the full `Tool` interface declarative. `BuildCommand()`,
  `DiscoverSessionID()`, and `GetSandboxSettings()` stay as Go code — they
  involve real per-tool logic (CLI flag shapes, state-file scanning,
  conditional settings construction) that doesn't reduce to data.
- Not adding a "pull credentials back out" step on session save. Credentials
  are one-way, host-to-container identity material, not session state —
  unlike a tool's own config dir, which already gets pulled back via
  `saveSessionData`.
- Not collapsing `setupCLIConfig`/`injectCredentials` into a single generic
  engine with the new feature (considered as "Approach B", rejected — see
  Alternatives Considered).

## Design

### 1. Catalog (data)

A new embedded catalog file, `internal/tool/credentials/catalog.toml`, defines
named credential bundles:

```toml
[claude]
config_dir = ".claude"
files = [".credentials.json", "config.yml", "settings.json", "CLAUDE.md"]
state_file = ".claude.json"
sandbox_settings_file = "settings.json"
always_setup = false
auto_context_file = ".claude/CLAUDE.md"

[opencode]
config_dir = ".config/opencode"
files = ["opencode.json", "tui.json"]
sandbox_settings_file = "opencode.json"
always_setup = true

[pi]
config_dir = ".pi/agent"
files = ["settings.json", "models.json", "auth.json", "AGENTS.md"]
sandbox_settings_file = "settings.json"
always_setup = true

[ollama]
config_dir = ".ollama"
files = ["id_ed25519"]
mode = "0600"
```

The `claude`, `opencode`, and `pi` entries are exact data mirrors of today's
hardcoded Go values in `ClaudeTool`/`OpencodeTool`/`PiTool` — this is a pure
data-extraction refactor with no behavior change for existing tools. `ollama`
is the first curated third-party bundle.

```go
package credentials

type Bundle struct {
    Name                string
    ConfigDir           string   `toml:"config_dir"`
    Files               []string `toml:"files"`
    StateFile           string   `toml:"state_file"`
    SandboxSettingsFile string   `toml:"sandbox_settings_file"`
    AlwaysSetup         bool     `toml:"always_setup"`
    AutoContextFile     string   `toml:"auto_context_file"`
    Mode                string   `toml:"mode"` // optional chmod mode, e.g. "0600"
}

func Lookup(name string) (Bundle, bool)
```

Parsed once (embedded via `go:embed`) into a `map[string]Bundle` at package
load.

### 2. Builtin tool wiring

`ClaudeTool.ConfigDirName()`, `.EssentialConfigFiles()`,
`.SandboxSettingsFileName()`, `.StateConfigFileName()`, `.AlwaysSetupConfig()`,
and `.AutoContextFile()` (and the OpenCode/Pi equivalents) become lookups into
the catalog:

```go
func (c *ClaudeTool) EssentialConfigFiles() []string {
    return mustLookup("claude").Files
}
```

`BuildCommand`, `DiscoverSessionID`, and `GetSandboxSettings` are untouched —
still real Go per tool.

### 3. Profile config: `[[credentials]]`

Parallel to the existing `[[mounts]]`:

```toml
[[credentials]]
bundle = "ollama"

[[credentials]]
host = "~/.aws/credentials"
container = "~/.aws/credentials"
mode = "0600"
```

New struct in `internal/config/config.go`:

```go
type CredentialEntry struct {
    Bundle    string `toml:"bundle"`
    Host      string `toml:"host"`
    Container string `toml:"container"`
    Mode      string `toml:"mode"`
    Untrusted bool   `toml:"-"`
    SourcePath string `toml:"-"`
}
```

`ProfileConfig.Credentials []CredentialEntry` `toml:"credentials"`, merged
into `Config.Credentials.Default` in `ApplyProfile` the same way mounts are
today.

`Validate()` enforces:
- Exactly one of `Bundle`, or `Host`+`Container`, is set (mutually exclusive).
- If `Mode` is set, it must parse as a valid octal file-mode string.

### 4. Parsing

New `internal/cli/credential_parser.go`, mirroring `mount_parser.go`:

- `ParseCredentialConfig(cfg *config.Config) (*session.CredentialConfig, error)`
- Bundle references resolve via `credentials.Lookup`. An unknown bundle name
  is a **hard config error** at parse time (fail fast — it's a typo, not
  something to silently skip).
- A resolved bundle expands into one `session.CredentialEntry` per file in
  `Files`, each with the bundle's `config_dir` joined against the filename
  for both host and container paths (e.g. `~/.claude/settings.json`). If
  `StateFile` is set, it expands into one additional entry rooted directly
  at the home directory, not under `config_dir` — it's a sibling of the
  config dir (e.g. `~/.claude.json` sits next to `~/.claude/`, not inside
  it), matching `StateConfigFileName()`'s existing semantics.
- Ad-hoc entries expand `~`, and validate the container path is absolute —
  same rules as mounts.

### 5. Trust gating

Reuses the same "does this host path escape the workspace" predicate that
`internal/session/trust.go` already applies to mounts, but **only for ad-hoc
entries** sourced from untrusted project-scope config. Catalog-referenced
entries are never gated — they carry the same trust level as the existing
builtin tool credential seeding, since the actual host path is fixed by
coi's own shipped catalog, not chosen by the untrusted config.

Rationale (see discussion below): the risk `coi trust` guards against is an
untrusted project config choosing an arbitrary host path to read and
exfiltrate during the session. A bundle reference can only pick a name from
a coi-vetted, bounded list — it can't point at an arbitrary path — so it
doesn't reopen that hole. An ad-hoc entry can point at anything, so it gets
gated exactly like an ad-hoc mount.

Copy-vs-mount and trust, worked through explicitly during design: credential
entries are copied and chowned into the container, not bind-mounted. On the
threat `coi trust` actually targets — live exfiltration during the session —
copy and mount are equivalent (once the bytes are in the container, it
doesn't matter how they got there). Copy is *safer* than mount on host
write-back (a copy can't corrupt the host original). Copy is *worse* on
residue/revocability for **persistent** profiles — a copied credential sits
in that container's storage indefinitely, with no "unmount to revoke."
This isn't a new risk class though: `setupCLIConfig` already does exactly
this for Claude/OpenCode/Pi credentials on persistent containers today.
`[[credentials]]` extends an already-accepted pattern rather than
introducing a new one, so it doesn't change the trust-gating design above —
worth a docs callout, not a different rule.

### 6. Apply at session setup

New `setupCredentials(mgr container.ContainerManager, homeDir string, entries []session.CredentialEntry, logger func(string)) error`
in `internal/session/setup_config.go`, run as a new step alongside the
existing CLI-tool-config setup (`setupCLIConfig`). Per entry:

1. `mkdir -p` the container destination's parent dir (bundles like `ollama`
   whose config dir doesn't otherwise get created).
2. `os.Stat` the host file; skip with a log line if missing — same tolerant
   behavior as `EssentialConfigFiles` copying (e.g. user hasn't signed into
   ollama yet).
3. `mgr.PushFile(host, dest)`.
4. `mgr.Chown(dest, container.CodeUID, container.CodeUID)`.
5. If `Mode` is set, chmod the file (new small addition — either a
   `mgr.Chmod` helper mirroring the existing `Chown`, or an
   `ExecArgs(["chmod", mode, dest])` call).

Also re-run on session resume (idempotent — picks up a rotated key). No
pull-back-out step (see Non-goals).

### 7. Testing

- Catalog tests asserting `claude`/`opencode`/`pi` entries exactly match
  today's hardcoded Go values — regression guard for the extraction refactor.
- `CredentialEntry.Validate()` tests (mutual exclusivity, mode format).
- Parser tests: bundle resolution, unknown-bundle error, ad-hoc path
  expansion.
- Trust-gating tests: ad-hoc + untrusted + outside-workspace → dropped;
  bundle entry ungated regardless of trust source.
- `setupCredentials` test against the existing fake `ContainerManager`
  (push+chown+chmod call sequence, missing-file skip).
- Existing `ClaudeTool`/`OpenCode`/`Pi` tests pass unchanged — proves the
  catalog refactor is behavior-preserving.

## Alternatives Considered

**Shape-only consistency, no named catalog.** Make `[[credentials]]`'s field
names match `[[mounts]]` for a consistent mental model, without a shared
lookup-by-name catalog. Rejected: doesn't let a profile say
`bundle = "ollama"` and reuse a vetted definition; every profile would have
to spell out full host paths, and there'd be no natural home for builtin
tools' own definitions to live as data.

**Catalog-only, no ad-hoc entries.** Require every credential source, builtin
or third-party, to be a named catalog entry, contributed via a (data-only)
PR to coi. Rejected: contradicts the self-service motivation in #549 — the
user would have had to land an upstream PR before using their own ollama
setup.

**Full unification (Approach B).** Collapse `setupCLIConfig`/
`injectCredentials` and the new `[[credentials]]` feature into a single
generic seeding engine, with builtin tools reduced to catalog entries the
selected `Tool` references — no special-casing at all. More literally
delivers "make custom tools and builtin more similar" (same code path, not
just same data shape), but is a much bigger refactor touching working,
tool-specific logic (sandbox-settings merging, state-file handling) for a
payoff that's mostly aesthetic right now. Deferred as a possible followup
once the catalog's proven out.

**Gate all `[[credentials]]` entries uniformly, like mounts.** Simpler single
rule, but would require `coi trust` even for
`credentials = ["gh-cli"]`-style catalog references from project-scope
config, despite the host path being no more attacker-influenced than the
builtin tool credentials already seeded ungated today. Rejected in favor of
gating ad-hoc entries only.
