package config

import (
	_ "embed"
)

//go:embed embedded/default_config.toml
var EmbeddedDefaultConfig []byte
