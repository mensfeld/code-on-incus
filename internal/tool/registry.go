package tool

import (
	"fmt"
	"sort"
	"strings"
)

// registry maps tool names to their factory functions
var registry = map[string]func() Tool{
	"claude": NewClaude,
}

// Get returns a tool by name
func Get(name string) (Tool, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s (supported: %s)", name, strings.Join(ListSupported(), ", "))
	}
	return factory(), nil
}

// GetDefault returns the default tool (Claude)
func GetDefault() Tool {
	return NewClaude()
}

// ListSupported returns a sorted list of supported tool names
func ListSupported() []string {
	tools := make([]string, 0, len(registry))
	for name := range registry {
		tools = append(tools, name)
	}
	sort.Strings(tools)
	return tools
}
