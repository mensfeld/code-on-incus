package cli

import "fmt"

// ExitCodeError is returned by commands that need to exit with a specific
// non-zero code (e.g., health checks, container exec, coi run). Returning
// this through cobra instead of calling os.Exit() directly ensures that
// deferred cleanup (container deletion, firewall teardown) runs before the
// process exits.
type ExitCodeError struct {
	Code int
}

func (e *ExitCodeError) Error() string {
	return fmt.Sprintf("exit code %d", e.Code)
}
