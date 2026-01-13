package container

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	CodeUID      = 1000
	CodeUser     = "code"
	IncusGroup   = "incus-admin"
	IncusProject = "default"
)

// IncusExec executes an Incus command via sg wrapper for group permissions
func IncusExec(args ...string) error {
	cmdArgs := buildIncusCommand(args...)
	cmd := exec.Command("sg", cmdArgs...)
	cmd.Stdout = os.Stderr // Send stdout to stderr so it's visible
	cmd.Stderr = os.Stderr // Show errors instead of silencing them
	return cmd.Run()
}

// IncusExecInteractive executes an Incus command with stdin/stdout/stderr attached
func IncusExecInteractive(args ...string) error {
	cmdArgs := buildIncusCommand(args...)
	cmd := exec.Command("sg", cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// IncusExecQuiet executes an Incus command silently (suppress stdout/stderr)
func IncusExecQuiet(args ...string) error {
	cmdArgs := buildIncusCommand(args...)
	cmd := exec.Command("sg", cmdArgs...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// IncusOutput executes an Incus command and returns the output (trimmed)
func IncusOutput(args ...string) (string, error) {
	cmdArgs := buildIncusCommand(args...)
	cmd := exec.Command("sg", cmdArgs...)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil

	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if err != nil {
		// Extract exit code if available
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, &ExitError{
				ExitCode: exitErr.ExitCode(),
				Err:      err,
			}
		}
		return output, err
	}

	return output, nil
}

// IncusOutputRaw executes an Incus command and returns the output (not trimmed)
func IncusOutputRaw(args ...string) (string, error) {
	cmdArgs := buildIncusCommand(args...)
	cmd := exec.Command("sg", cmdArgs...)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil

	err := cmd.Run()
	output := stdout.String()

	if err != nil {
		// Extract exit code if available
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, &ExitError{
				ExitCode: exitErr.ExitCode(),
				Err:      err,
			}
		}
		return output, err
	}

	return output, nil
}

// IncusOutputWithArgs executes incus with raw args (no additional wrapping)
func IncusOutputWithArgs(args ...string) (string, error) {
	// Build command with project flag
	incusArgs := append([]string{"--project", IncusProject}, args...)

	// Build properly quoted command
	quotedArgs := make([]string, len(incusArgs))
	for i, arg := range incusArgs {
		quotedArgs[i] = shellQuote(arg)
	}

	incusCmd := "incus " + strings.Join(quotedArgs, " ")
	sgArgs := []string{IncusGroup, "-c", incusCmd}

	cmd := exec.Command("sg", sgArgs...)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil

	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if err != nil {
		// Extract exit code if available
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, &ExitError{
				ExitCode: exitErr.ExitCode(),
				Err:      err,
			}
		}
		return output, err
	}

	return output, nil
}

// IncusFilePush pushes a file into a container
func IncusFilePush(source, destination string) error {
	cmdArgs := buildIncusCommand("file", "push", source, destination)
	cmd := exec.Command("sg", cmdArgs...)
	return cmd.Run()
}

// ContainerExecOptions holds options for executing commands in containers
type ContainerExecOptions struct {
	Sandbox       bool
	RunAsRoot     bool
	CaptureOutput bool
	Env           map[string]string
	Cwd           string
	Timeout       *time.Duration
}

// ContainerExec executes a command inside a container with proper environment
func ContainerExec(containerName, command string, opts ContainerExecOptions) (string, error) {
	// Build command parts
	args := []string{"exec", containerName}

	// Set working directory
	if opts.Cwd == "" {
		opts.Cwd = "/workspace"
	}
	args = append(args, "--cwd", opts.Cwd)

	// User context: run as code user by default
	var envFlags []string
	if !opts.RunAsRoot {
		args = append(args, "--user", fmt.Sprintf("%d", CodeUID))
		args = append(args, "--group", fmt.Sprintf("%d", CodeUID))
		envFlags = append(envFlags, "--env", "HOME=/home/code")
	}

	// Sandbox mode
	if opts.Sandbox {
		envFlags = append(envFlags, "--env", "IS_SANDBOX=1")
	}

	// Additional environment variables
	for k, v := range opts.Env {
		envFlags = append(envFlags, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	// Build full incus command
	incusArgs := append([]string{"--project", IncusProject}, args...)
	incusArgs = append(incusArgs, envFlags...)
	incusArgs = append(incusArgs, "--", "bash", "-c", command)

	// Build sg command
	incusCmd := "incus " + strings.Join(incusArgs, " ")
	sgArgs := []string{IncusGroup, "-c", incusCmd}

	// Add timeout wrapper if specified
	var cmd *exec.Cmd
	if opts.Timeout != nil {
		timeoutCmd := fmt.Sprintf("timeout %d %s", int(opts.Timeout.Seconds()), incusCmd)
		cmd = exec.Command("sg", IncusGroup, "-c", timeoutCmd)
	} else {
		cmd = exec.Command("sg", sgArgs...)
	}

	if opts.CaptureOutput {
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		err := cmd.Run()
		return strings.TrimSpace(stdout.String()), err
	}

	return "", cmd.Run()
}

// LaunchContainer launches an ephemeral container
func LaunchContainer(imageAlias, containerName string) error {
	args := []string{"launch", imageAlias, containerName, "--ephemeral"}
	return IncusExec(args...)
}

// LaunchContainerPersistent launches a non-ephemeral container
func LaunchContainerPersistent(imageAlias, containerName string) error {
	args := []string{"launch", imageAlias, containerName}
	return IncusExec(args...)
}

// StopContainer stops a container
func StopContainer(containerName string) error {
	return IncusExec("stop", containerName, "--force")
}

// DeleteContainer deletes a container forcefully
func DeleteContainer(containerName string) error {
	return IncusExecQuiet("delete", containerName, "--force")
}

// ContainerRunning checks if a container is running
func ContainerRunning(containerName string) (bool, error) {
	output, err := IncusOutput("list", containerName, "--format=json")
	if err != nil {
		return false, err
	}

	var containers []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		return false, err
	}

	for _, c := range containers {
		if c.Name == containerName && c.Status == "Running" {
			return true, nil
		}
	}

	return false, nil
}

// PublishContainer publishes a stopped container as an image
func PublishContainer(containerName, aliasName, description string) (string, error) {
	// Stop container if running (ignore error if already stopped)
	running, _ := ContainerRunning(containerName)
	if running {
		if err := StopContainer(containerName); err != nil {
			return "", err
		}
	}

	// Build publish command
	args := []string{"publish", containerName, "--alias", aliasName}
	if description != "" {
		args = append(args, fmt.Sprintf("description=%s", description))
	}

	// Execute and capture output
	output, err := IncusOutput(args...)
	if err != nil {
		return "", err
	}

	// Extract fingerprint from output
	re := regexp.MustCompile(`fingerprint:\s*([a-f0-9]+)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not extract fingerprint from output")
	}

	fingerprint := matches[1]

	// Cleanup container after successful publish
	if err := DeleteContainer(containerName); err != nil {
		return fingerprint, err // Return fingerprint even if cleanup fails
	}

	return fingerprint, nil
}

// DeleteImage deletes an image by alias
func DeleteImage(aliasName string) error {
	return IncusExecQuiet("image", "delete", aliasName)
}

// ImageExists checks if an image with the given alias exists
func ImageExists(aliasName string) (bool, error) {
	output, err := IncusOutput("image", "list", "--format=json")
	if err != nil {
		return false, err
	}

	var images []struct {
		Aliases []struct {
			Name string `json:"name"`
		} `json:"aliases"`
	}

	if err := json.Unmarshal([]byte(output), &images); err != nil {
		return false, err
	}

	for _, img := range images {
		for _, alias := range img.Aliases {
			if alias.Name == aliasName {
				return true, nil
			}
		}
	}

	return false, nil
}

// ListImagesByPrefix lists images by alias prefix
func ListImagesByPrefix(prefix string) ([]string, error) {
	output, err := IncusOutput("image", "list", "--format=json")
	if err != nil {
		return nil, err
	}

	var images []struct {
		Aliases []struct {
			Name string `json:"name"`
		} `json:"aliases"`
	}

	if err := json.Unmarshal([]byte(output), &images); err != nil {
		return nil, err
	}

	var matching []string
	for _, img := range images {
		for _, alias := range img.Aliases {
			if strings.HasPrefix(alias.Name, prefix) {
				matching = append(matching, alias.Name)
			}
		}
	}

	return matching, nil
}

// ListContainers lists all containers matching a name pattern
func ListContainers(pattern string) ([]string, error) {
	output, err := IncusOutput("list", "--format=json")
	if err != nil {
		return nil, err
	}

	var containers []struct {
		Name string `json:"name"`
	}

	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		return nil, err
	}

	// Compile pattern as regex
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	var matching []string
	for _, c := range containers {
		if re.MatchString(c.Name) {
			matching = append(matching, c.Name)
		}
	}

	return matching, nil
}

// buildIncusCommand builds the full incus command with project flag
func buildIncusCommand(args ...string) []string {
	incusArgs := append([]string{"--project", IncusProject}, args...)

	// Properly quote arguments for shell execution
	quotedArgs := make([]string, len(incusArgs))
	for i, arg := range incusArgs {
		quotedArgs[i] = shellQuote(arg)
	}

	incusCmd := "incus " + strings.Join(quotedArgs, " ")
	return []string{IncusGroup, "-c", incusCmd}
}

// shellQuote quotes a string for safe use in a shell command
func shellQuote(s string) string {
	// If string contains no special characters, don't quote
	if regexp.MustCompile(`^[a-zA-Z0-9@%+=:,./_-]+$`).MatchString(s) {
		return s
	}

	// Otherwise, single-quote and escape any single quotes
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
