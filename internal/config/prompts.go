package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// PromptNames returns the sorted list of registered [prompts] names, for help
// text and error messages.
func (c *Config) PromptNames() []string {
	names := make([]string, 0, len(c.Prompts))
	for name := range c.Prompts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResolvePrompt returns the final prompt text for a named [prompts] entry,
// reading the referenced file for a { file = "..." } entry (the path is already
// absolute after load-time resolution). It is the lookup behind
// `coi run --prompt-name <name>` (#701).
//
// A missing name is an error that lists the available names so a typo is easy to
// fix. An entry that resolves to empty (empty inline string or empty file) is an
// error too, so a scheduled run never launches an agent with no instructions.
func (c *Config) ResolvePrompt(name string) (string, error) {
	entry, ok := c.Prompts[name]
	if !ok {
		if len(c.Prompts) == 0 {
			return "", fmt.Errorf("no prompt named %q: no [prompts] are configured", name)
		}
		return "", fmt.Errorf("no prompt named %q; available prompts: %s",
			name, strings.Join(c.PromptNames(), ", "))
	}

	if entry.File != "" {
		data, err := os.ReadFile(entry.File)
		if err != nil {
			return "", fmt.Errorf("prompt %q: failed to read file %s: %w", name, entry.File, err)
		}
		text := string(data)
		if strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("prompt %q resolves to an empty file: %s", name, entry.File)
		}
		return text, nil
	}

	if strings.TrimSpace(entry.Text) == "" {
		return "", fmt.Errorf("prompt %q is empty", name)
	}
	return entry.Text, nil
}
