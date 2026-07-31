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
func injectContextFile(mgr container.ContainerManager, info tool.ContextInfo, customPath, homeDir string, logger func(string)) error {
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

// Markers delimiting the coi-managed sandbox context block inside the tool's
// auto-load file. The block is rewritten (not appended) every session, so the
// file never accumulates duplicate copies (#674). Anything outside the markers
// (e.g. a CLAUDE.md copied from the host) is preserved.
const (
	autoCtxBeginMarker = "# BEGIN COI Sandbox Context (managed by coi — do not edit this block)"
	autoCtxEndMarker   = "# END COI Sandbox Context"
)

// stripManagedAutoContext removes a previously coi-written managed block (from
// autoCtxBeginMarker through autoCtxEndMarker, inclusive) from s, so a fresh
// block can replace it instead of stacking on top of it. Content outside the
// block is returned untouched. If the begin marker is present but the end marker
// is missing (a truncated/hand-mangled block), everything from the begin marker
// on is dropped rather than left half-parsed.
func stripManagedAutoContext(s string) string {
	begin := strings.Index(s, autoCtxBeginMarker)
	if begin == -1 {
		return s
	}
	rest := s[begin+len(autoCtxBeginMarker):]
	endRel := strings.Index(rest, autoCtxEndMarker)
	if endRel == -1 {
		return s[:begin]
	}
	end := begin + len(autoCtxBeginMarker) + endRel + len(autoCtxEndMarker)
	return s[:begin] + s[end:]
}

// injectAutoContextFile writes sandbox context into the tool's native auto-load
// file (e.g., ~/.claude/CLAUDE.md) as a single coi-managed block delimited by
// autoCtxBeginMarker/autoCtxEndMarker. If the file already exists (e.g. copied
// from host, or left over from a previous session on a persistent container), any
// prior managed block is stripped and a fresh one is written in its place, so the
// file does not grow a new copy every session (#674). Non-managed content in the
// file is preserved.
func injectAutoContextFile(mgr container.ContainerManager, acf tool.ToolWithAutoContextFile, contextContent, homeDir string, logger func(string)) error {
	relPath := acf.AutoContextFile()
	destPath := filepath.Join(homeDir, relPath)
	destDir := filepath.Dir(destPath)

	// Ensure parent directory exists
	mkdirCmd := fmt.Sprintf("mkdir -p %s", destDir)
	if _, err := mgr.ExecCommand(mkdirCmd, container.ExecCommandOptions{Capture: true}); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", relPath, err)
	}

	managedBlock := autoCtxBeginMarker + "\n" + contextContent + "\n" + autoCtxEndMarker + "\n"

	// Check if the file already exists in the container (e.g., host's CLAUDE.md was
	// copied, or the container is persistent and a prior session already wrote one).
	checkCmd := fmt.Sprintf("test -f %s && echo exists || echo missing", destPath)
	checkResult, err := mgr.ExecCommand(checkCmd, container.ExecCommandOptions{Capture: true})

	var newContent string
	if err == nil && strings.TrimSpace(checkResult) == "exists" {
		// Read the current file, drop any prior managed block, and re-attach a fresh
		// one. This keeps exactly one always-current copy while preserving any
		// user/host content, instead of appending a new copy every session (#674).
		existing, readErr := mgr.ExecCommand(fmt.Sprintf("cat %s", destPath), container.ExecCommandOptions{Capture: true})
		if readErr != nil {
			return fmt.Errorf("failed to read %s: %w", relPath, readErr)
		}
		preserved := strings.TrimRight(stripManagedAutoContext(existing), "\n")
		if preserved == "" {
			logger(fmt.Sprintf("Refreshing sandbox context in %s", relPath))
			newContent = managedBlock
		} else {
			logger(fmt.Sprintf("Refreshing sandbox context in %s (preserving existing content)", relPath))
			newContent = preserved + "\n\n" + managedBlock
		}
	} else {
		logger(fmt.Sprintf("Creating %s with sandbox context", relPath))
		newContent = managedBlock
	}

	// Overwrite the file with the reconciled content (single managed block).
	if err := mgr.CreateFile(destPath, newContent); err != nil {
		return fmt.Errorf("failed to write %s: %w", relPath, err)
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
