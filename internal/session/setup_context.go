package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/tool"
)

// injectContextFile creates ~/SANDBOX_CONTEXT.md inside the container.
// If customPath is provided, it reads the file from the host and uses its content.
// Otherwise, it renders the default embedded template with dynamic environment info.
func injectContextFile(mgr *container.Manager, info tool.ContextInfo, customPath, homeDir string, logger func(string)) error {
	destPath := filepath.Join(homeDir, "SANDBOX_CONTEXT.md")

	var content string
	if customPath != "" {
		data, err := os.ReadFile(customPath)
		if err != nil {
			return fmt.Errorf("failed to read custom context file %s: %w", customPath, err)
		}
		content = string(data)
		logger(fmt.Sprintf("Using custom context file: %s", customPath))
	} else {
		content = tool.RenderContextFileContent(info)
		logger("Injecting default sandbox context file")
	}

	// Create the file
	if err := mgr.CreateFile(destPath, content); err != nil {
		return fmt.Errorf("failed to create context file %s: %w", destPath, err)
	}

	// Fix ownership if running as non-root user
	if homeDir != "/root" {
		if err := mgr.Chown(destPath, container.CodeUID, container.CodeUID); err != nil {
			return fmt.Errorf("failed to set context file ownership: %w", err)
		}
	}

	logger(fmt.Sprintf("Context file injected at %s", destPath))
	return nil
}

// resolveContextContent returns the sandbox context content string.
// If customPath is provided, it reads the file from the host; otherwise it
// renders the default embedded template. This is used both for ~/SANDBOX_CONTEXT.md
// and for auto-context injection into tool-native files.
func resolveContextContent(info tool.ContextInfo, customPath string, logger func(string)) string {
	if customPath != "" {
		data, err := os.ReadFile(customPath)
		if err != nil {
			logger(fmt.Sprintf("Warning: Failed to read custom context file %s: %v", customPath, err))
			return ""
		}
		return string(data)
	}
	return tool.RenderContextFileContent(info)
}

// injectAutoContextFile writes sandbox context into the tool's native auto-load
// file (e.g., ~/.claude/CLAUDE.md). If the file already exists (e.g., copied from
// host), the sandbox context is appended with a separator. Otherwise, the file is
// created with just the sandbox context.
func injectAutoContextFile(mgr *container.Manager, acf tool.ToolWithAutoContextFile, contextContent, homeDir string, logger func(string)) error {
	relPath := acf.AutoContextFile()
	destPath := filepath.Join(homeDir, relPath)
	destDir := filepath.Dir(destPath)

	// Ensure parent directory exists
	mkdirCmd := fmt.Sprintf("mkdir -p %s", destDir)
	if _, err := mgr.ExecCommand(mkdirCmd, container.ExecCommandOptions{Capture: true}); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", relPath, err)
	}

	separator := "\n\n# COI Sandbox Context\n\n"

	// Check if the file already exists in the container (e.g., host's CLAUDE.md was copied)
	checkCmd := fmt.Sprintf("test -f %s && echo exists || echo missing", destPath)
	checkResult, err := mgr.ExecCommand(checkCmd, container.ExecCommandOptions{Capture: true})

	if err == nil && strings.TrimSpace(checkResult) == "exists" {
		// File exists — append sandbox context with separator by writing to a temp
		// file and using cat >> inside the container
		logger(fmt.Sprintf("Appending sandbox context to existing %s", relPath))
		appendContent := separator + contextContent
		tmpFile, tmpErr := os.CreateTemp("", "coi-autocontext-*")
		if tmpErr != nil {
			return fmt.Errorf("failed to create temp file for append: %w", tmpErr)
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)
		if _, tmpErr = tmpFile.WriteString(appendContent); tmpErr != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to write temp file for append: %w", tmpErr)
		}
		tmpFile.Close()
		// Push temp file to a staging path in the container, then cat >> to target
		stagingPath := destPath + ".coi-append"
		if pushErr := mgr.PushFile(tmpPath, stagingPath); pushErr != nil {
			return fmt.Errorf("failed to push append content to %s: %w", relPath, pushErr)
		}
		catCmd := fmt.Sprintf("cat %s >> %s && rm -f %s", stagingPath, destPath, stagingPath)
		if _, catErr := mgr.ExecCommand(catCmd, container.ExecCommandOptions{Capture: true}); catErr != nil {
			return fmt.Errorf("failed to append to %s: %w", relPath, catErr)
		}
	} else {
		// File doesn't exist — create it with sandbox context
		logger(fmt.Sprintf("Creating %s with sandbox context", relPath))
		if err := mgr.CreateFile(destPath, contextContent); err != nil {
			return fmt.Errorf("failed to create %s: %w", relPath, err)
		}
	}

	// Fix ownership if running as non-root user
	if homeDir != "/root" {
		if err := mgr.Chown(destPath, container.CodeUID, container.CodeUID); err != nil {
			return fmt.Errorf("failed to set %s ownership: %w", relPath, err)
		}
	}

	logger(fmt.Sprintf("Auto-context injected at %s", destPath))
	return nil
}
