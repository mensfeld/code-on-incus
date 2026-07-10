// Package credentials holds the embedded catalog of named credential
// bundles: which host config directory and files to copy into a container
// for a given tool or third-party provider. Builtin AI tools (claude,
// opencode, pi) source their ToolWithConfigDirFiles metadata from here;
// profile [[credentials]] entries can reference any bundle by name.
package credentials

import (
	_ "embed"
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"
)

//go:embed catalog.toml
var catalogTOML string

// Bundle describes a named, coi-known set of host credential files: which
// directory they live in on both host and container (relative to the
// respective home directory), which files to copy, and how to treat them
// once copied.
type Bundle struct {
	ConfigDir           string   `toml:"config_dir"`
	Files               []string `toml:"files"`
	StateFile           string   `toml:"state_file"`
	SandboxSettingsFile string   `toml:"sandbox_settings_file"`
	AlwaysSetup         bool     `toml:"always_setup"`
	AutoContextFile     string   `toml:"auto_context_file"`
	Mode                string   `toml:"mode"`
}

var catalog map[string]Bundle

func init() {
	if _, err := toml.Decode(catalogTOML, &catalog); err != nil {
		panic(fmt.Sprintf("credentials: embedded catalog.toml is invalid: %v", err))
	}
}

// Lookup returns the named bundle and whether it exists in the catalog.
func Lookup(name string) (Bundle, bool) {
	b, ok := catalog[name]
	return b, ok
}

// Names returns the sorted list of known bundle names, for error messages.
func Names() []string {
	names := make([]string, 0, len(catalog))
	for n := range catalog {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
