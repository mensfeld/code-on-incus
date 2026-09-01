package cli

import (
	"fmt"
	"os"

	"github.com/mensfeld/code-on-incus/internal/alias"
)

// confirmYN prints prompt and reads a line from stdin, returning true only when
// the user answers "y" or "Y". This is the shared semantics of coi's inline
// "[y/N]" confirmation prompts (destructive commands like clean/kill/persist).
func confirmYN(prompt string) bool {
	fmt.Print(prompt)
	var response string
	_, _ = fmt.Scanln(&response)
	return response == "y" || response == "Y"
}

// validateTextOrJSON returns a usage error (exit code 2) when format is neither
// "text" nor "json", using the message shared by the format-taking commands.
func validateTextOrJSON(format string) error {
	if format != "text" && format != "json" {
		return &ExitCodeError{Code: 2, Message: fmt.Sprintf("invalid format '%s': must be 'text' or 'json'", format)}
	}
	return nil
}

// resolveNameOrAlias resolves a running container's alias to its container name,
// returns the input unchanged when it is already a container name, and returns
// the resolver error when it is neither a resolvable alias nor a container name.
func resolveNameOrAlias(nameOrAlias string) (string, error) {
	if resolved, err := alias.ResolveAliasForRunning(nameOrAlias); err == nil {
		return resolved, nil
	} else if !alias.IsContainerName(nameOrAlias) {
		return "", err
	}
	return nameOrAlias, nil
}

// stderrLogFn is the shared line logger for the session helpers that take a
// func(string): it writes msg to stderr with a trailing newline.
func stderrLogFn(msg string) {
	fmt.Fprintf(os.Stderr, "%s\n", msg)
}
