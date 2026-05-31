// Package schema provides access to the COI profile JSON Schema.
// External tools (e.g. a Rails web UI) can consume the schema via
// `coi schema profile` and validate profile config.toml data before
// writing it to disk — no need to duplicate field-level validation.
package schema

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed profile.schema.json
var profileSchemaBytes []byte

// GetProfileSchema returns the raw bytes of the COI profile JSON Schema
// (JSON Schema 2020-12).
func GetProfileSchema() []byte {
	return profileSchemaBytes
}

// ProfileSchemaID returns the $id declared in the schema document.
func ProfileSchemaID() (string, error) {
	var doc struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(profileSchemaBytes, &doc); err != nil {
		return "", fmt.Errorf("failed to parse profile schema: %w", err)
	}
	return doc.ID, nil
}
