package tool

import (
	"path/filepath"
)

// OmpTool implements Tool for Oh My Pi (https://github.com/can1357/oh-my-pi)
type OmpTool struct {
	permissionMode  string
	contextFilePath string
}

// NewOmp creates a new omp tool instance
func NewOmp() Tool { return &OmpTool{} }

func (o *OmpTool) Name() string { return "omp" }

func (o *OmpTool) Binary() string { return "omp" }

// ConfigDirName returns the config directory for omp (~/.omp).
func (o *OmpTool) ConfigDirName() string { return mustBundle("omp").ConfigDir }

func (o *OmpTool) SessionsDirName() string { return "sessions-omp" }

// BuildCommand builds the omp launch command.
func (o *OmpTool) BuildCommand(sessionID string, resume bool, resumeSessionID string) []string {
	cmd := []string{"omp"}
	if resume {
		cmd = append(cmd, "--continue")
	}
	return cmd
}

// PreLaunch implements ToolWithPreLaunch.
func (o *OmpTool) PreLaunch() [][]string {
	if o.contextFilePath == "" {
		return nil
	}
	homeDir := filepath.Dir(o.contextFilePath)
	ompDir := filepath.Join(homeDir, ".omp")
	linkTarget := filepath.Join(ompDir, "APPEND_SYSTEM.md")

	return [][]string{
		{"mkdir", "-p", ompDir},
		{"ln", "-sf", o.contextFilePath, linkTarget},
	}
}

func (o *OmpTool) DiscoverSessionID(stateDir string) string { return "" }

func (o *OmpTool) GetSandboxSettings() map[string]interface{} {
	return map[string]interface{}{}
}

func (o *OmpTool) SetPermissionMode(mode string) {
	o.permissionMode = mode
}

func (o *OmpTool) SetAutoContextPath(path string) {
	if !filepath.IsAbs(path) {
		return
	}
	o.contextFilePath = path
}

func (o *OmpTool) EssentialConfigFiles() []string {
	return mustBundle("omp").Files
}

func (o *OmpTool) SandboxSettingsFileName() string { return mustBundle("omp").SandboxSettingsFile }

func (o *OmpTool) StateConfigFileName() string { return mustBundle("omp").StateFile }

func (o *OmpTool) AlwaysSetupConfig() bool { return mustBundle("omp").AlwaysSetup }

func (o *OmpTool) GetContainerEnv(workspacePath string) map[string]string {
	return map[string]string{
		"OMP_SESSION_DIR": filepath.Join(workspacePath, ".omp-sessions"),
	}
}
