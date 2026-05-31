// Package schema provides access to the COI profile JSON Schema.
// External tools (e.g. a Rails web UI) can consume the schema via
// `coi schema profile` and validate profile config.toml data before
// writing it to disk — no need to duplicate field-level validation.
//
// Source layout: the root schema (profile.schema.json) holds only the
// top-level property declarations.  Each sub-type lives in its own file
// under defs/ (e.g. defs/NetworkConfig.json).  GetProfileSchema bundles
// all defs into a single self-contained document so consumers do not need
// a multi-file resolver.
package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"embed"
)

//go:embed profile.schema.json
var rootSchemaBytes []byte

//go:embed defs/*.json
var defsFS embed.FS

// GetProfileSchema returns a self-contained JSON Schema 2020-12 document
// describing the full COI profile config.toml structure.  All sub-type
// definitions from defs/ are bundled inline so the output can be used
// by any validator without additional file resolution.
func GetProfileSchema() ([]byte, error) {
	return bundle()
}

// ProfileSchemaID returns the $id declared in the root schema document.
func ProfileSchemaID() (string, error) {
	var doc struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(rootSchemaBytes, &doc); err != nil {
		return "", fmt.Errorf("failed to parse profile schema: %w", err)
	}
	return doc.ID, nil
}

// bundle merges defs/*.json into the root schema's $defs and returns the
// resulting self-contained document.
func bundle() ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(rootSchemaBytes, &root); err != nil {
		return nil, fmt.Errorf("failed to parse root schema: %w", err)
	}

	entries, err := defsFS.ReadDir("defs")
	if err != nil {
		return nil, fmt.Errorf("failed to read defs directory: %w", err)
	}

	defs := make(map[string]any, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := defsFS.ReadFile("defs/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read def file %s: %w", entry.Name(), err)
		}
		var def any
		if err := json.Unmarshal(data, &def); err != nil {
			return nil, fmt.Errorf("failed to parse def file %s: %w", entry.Name(), err)
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		defs[key] = def
	}

	root["$defs"] = defs
	return json.MarshalIndent(root, "", "  ")
}
