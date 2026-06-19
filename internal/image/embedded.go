package image

import (
	_ "embed"
)

// build.sh is tracked here and embedded directly (no cp/generated copy);
// profiles/default/build.sh is a symlink to it so it still lives in the default
// profile. go:embed cannot use ".." so the real file must live in this package.
//
//go:embed build.sh
var embeddedCoiBuildScript []byte

//go:embed embedded/dummy
var embeddedDummy []byte
