package tool

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestRenderContextFileJSON(t *testing.T) {
	info := ContextInfo{
		ContainerName:      "coi-abc",
		ToolName:           "claude",
		WorkspacePath:      "/workspace",
		HomeDir:            "/home/code",
		Persistent:         true,
		NetworkMode:        "restricted",
		AllowedPorts:       []int{443},
		DNSServers:         []string{"192.168.1.2"},
		AllowedDomains:     []string{"github.com"},
		SSHAgentForwarded:  true,
		GHCLIAuthenticated: true,
		ForwardedEnvVars:   []string{"GH_TOKEN"},
		ProtectedPaths:     []string{"/home/code/.ssh"},
		Timezone:           "Europe/Warsaw",
		ExtraMounts:        []MountInfo{{ContainerPath: "/data"}, {ContainerPath: "/config"}},
		PublishedPorts: []PortInfo{
			{Name: "web", HostPort: 8080, ContainerPort: 80, Listen: "127.0.0.1", EnvVar: "COI_PORT_WEB"},
			{HostPort: 23410, ContainerPort: 23410, Pool: true},
		},
		CPULimit:    "2",
		MemoryLimit: "2GiB",
		MaxDuration: "2h",
		// OSName / Architecture deliberately unset — must default.
	}

	out, err := RenderContextFileJSON(info)
	if err != nil {
		t.Fatalf("RenderContextFileJSON returned error: %v", err)
	}

	var got SandboxContextJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}

	if got.SchemaVersion != sandboxContextSchemaVersion {
		t.Errorf("schema_version = %d, want %d", got.SchemaVersion, sandboxContextSchemaVersion)
	}
	if got.ContainerName != "coi-abc" || got.ToolName != "claude" {
		t.Errorf("container/tool = %q/%q", got.ContainerName, got.ToolName)
	}
	if got.OS != "Ubuntu (container)" {
		t.Errorf("os = %q, want defaulted 'Ubuntu (container)'", got.OS)
	}
	if got.Architecture != runtime.GOARCH {
		t.Errorf("architecture = %q, want %q", got.Architecture, runtime.GOARCH)
	}
	if !got.Persistent || !got.SSHAgentForwarded || !got.GHCLIAuthenticated {
		t.Errorf("bool fields not carried through: %+v", got)
	}
	if got.Network.Mode != "restricted" {
		t.Errorf("network.mode = %q, want restricted", got.Network.Mode)
	}
	if len(got.Network.AllowedPorts) != 1 || got.Network.AllowedPorts[0] != 443 {
		t.Errorf("network.allowed_ports = %v, want [443]", got.Network.AllowedPorts)
	}
	if len(got.Network.DNSServers) != 1 || got.Network.DNSServers[0] != "192.168.1.2" {
		t.Errorf("network.dns_servers = %v", got.Network.DNSServers)
	}
	if len(got.ExtraMounts) != 2 || got.ExtraMounts[0] != "/data" {
		t.Errorf("extra_mounts = %v, want [/data /config]", got.ExtraMounts)
	}
	if len(got.PublishedPorts) != 2 {
		t.Fatalf("published_ports = %v, want 2", got.PublishedPorts)
	}
	if got.PublishedPorts[0].Name != "web" || got.PublishedPorts[0].HostPort != 8080 || got.PublishedPorts[0].EnvVar != "COI_PORT_WEB" {
		t.Errorf("published_ports[0] mapped wrong: %+v", got.PublishedPorts[0])
	}
	if !got.PublishedPorts[1].Pool {
		t.Errorf("published_ports[1].pool should be true")
	}
	if got.Limits.CPU != "2" || got.Limits.Memory != "2GiB" || got.Limits.MaxDuration != "2h" {
		t.Errorf("limits mapped wrong: %+v", got.Limits)
	}
	if got.Timezone != "Europe/Warsaw" {
		t.Errorf("timezone = %q", got.Timezone)
	}
}

// A zero-ish ContextInfo must still produce valid JSON, apply the OS/arch
// defaults, and emit list fields as [] (never null) so consumers don't have to
// special-case a missing array.
func TestRenderContextFileJSON_NilSlicesBecomeEmptyArrays(t *testing.T) {
	out, err := RenderContextFileJSON(ContextInfo{WorkspacePath: "/workspace", HomeDir: "/home/code"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Every list field must render as [] (not null). These explicit checks are
	// the real contract; a blanket strings.Contains(out, "null") would be
	// brittle (it would also match a value that happens to contain "null").
	for _, key := range []string{
		`"protected_paths": []`,
		`"forwarded_env_vars": []`,
		`"extra_mounts": []`,
		`"published_ports": []`,
		`"allowed_ports": []`,
		`"dns_servers": []`,
		`"allowed_domains": []`,
	} {
		if !strings.Contains(out, key) {
			t.Errorf("expected %s in output (arrays must be [] not null):\n%s", key, out)
		}
	}
	// No field should serialize to a JSON null value ("key": null). Checked on
	// the ": null" token so a string value containing the word "null" can't
	// trip it.
	if strings.Contains(out, ": null") {
		t.Errorf("output should contain no null field values:\n%s", out)
	}

	var got SandboxContextJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if got.OS != "Ubuntu (container)" || got.Architecture != runtime.GOARCH {
		t.Errorf("defaults not applied: os=%q arch=%q", got.OS, got.Architecture)
	}
}

// The JSON egress fields must mirror the .md: a control is reported only in the
// mode that actually enforces it, so a consumer never reads an inert config
// value as an installed cap (#705 review #1).
func TestRenderContextFileJSON_EgressGatedByMode(t *testing.T) {
	base := ContextInfo{
		WorkspacePath:  "/workspace",
		HomeDir:        "/home/code",
		AllowedPorts:   []int{443},
		DNSServers:     []string{"192.168.1.2"},
		AllowedDomains: []string{"github.com"},
	}

	render := func(t *testing.T, mode string) SandboxNetworkJSON {
		t.Helper()
		info := base
		info.NetworkMode = mode
		out, err := RenderContextFileJSON(info)
		if err != nil {
			t.Fatalf("render(%s): %v", mode, err)
		}
		var got SandboxContextJSON
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("render(%s) invalid JSON: %v", mode, err)
		}
		return got.Network
	}

	// open: nothing is enforced even though all three are configured.
	if n := render(t, "open"); len(n.AllowedPorts) != 0 || len(n.DNSServers) != 0 || len(n.AllowedDomains) != 0 {
		t.Errorf("open mode should report no enforced egress, got %+v", n)
	}
	// restricted: ports + DNS enforced, domains are not (allowlist-only).
	if n := render(t, "restricted"); len(n.AllowedPorts) != 1 || len(n.DNSServers) != 1 || len(n.AllowedDomains) != 0 {
		t.Errorf("restricted mode gating wrong, got %+v", n)
	}
	// allowlist: ports + domains enforced, DNS pinning is not (allowlist blocks all DNS).
	if n := render(t, "allowlist"); len(n.AllowedPorts) != 1 || len(n.AllowedDomains) != 1 || len(n.DNSServers) != 0 {
		t.Errorf("allowlist mode gating wrong, got %+v", n)
	}
}
