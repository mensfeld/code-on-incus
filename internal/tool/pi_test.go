package tool

import (
	"testing"
)

func TestPiTool_Basics(t *testing.T) {
	pi := NewPi()

	if pi.Name() != "pi" {
		t.Errorf("Name() = %q, want %q", pi.Name(), "pi")
	}
	if pi.Binary() != "pi" {
		t.Errorf("Binary() = %q, want %q", pi.Binary(), "pi")
	}
	if pi.ConfigDirName() != ".pi/agent" {
		t.Errorf("ConfigDirName() = %q, want %q", pi.ConfigDirName(), ".pi/agent")
	}
	if pi.SessionsDirName() != "sessions-pi" {
		t.Errorf("SessionsDirName() = %q, want %q", pi.SessionsDirName(), "sessions-pi")
	}
}

func TestPiTool_BuildCommand_NewSession(t *testing.T) {
	pi := NewPi()
	cmd := pi.BuildCommand("some-session-id", false, "")
	if len(cmd) != 1 || cmd[0] != "pi" {
		t.Errorf("BuildCommand(new) = %v, want [pi]", cmd)
	}
}

func TestPiTool_BuildCommand_Resume(t *testing.T) {
	pi := NewPi()
	cmd := pi.BuildCommand("", true, "")
	expected := []string{"pi", "--continue"}
	if len(cmd) != len(expected) {
		t.Fatalf("BuildCommand(resume) = %v, want %v", cmd, expected)
	}
	for i, v := range expected {
		if cmd[i] != v {
			t.Errorf("BuildCommand(resume)[%d] = %q, want %q", i, cmd[i], v)
		}
	}
}

func TestPiTool_BuildCommand_ResumeWithID(t *testing.T) {
	pi := NewPi()
	// Even when a resumeSessionID is provided, pi always uses --continue
	// because it manages its own session discovery in .pi-sessions/.
	cmd := pi.BuildCommand("", true, "some-id")
	expected := []string{"pi", "--continue"}
	if len(cmd) != len(expected) {
		t.Fatalf("BuildCommand(resume with ID) = %v, want %v", cmd, expected)
	}
	for i, v := range expected {
		if cmd[i] != v {
			t.Errorf("BuildCommand(resume with ID)[%d] = %q, want %q", i, cmd[i], v)
		}
	}
}

func TestPiTool_DiscoverSessionID(t *testing.T) {
	pi := NewPi()
	id := pi.DiscoverSessionID("/some/path")
	if id != "" {
		t.Errorf("DiscoverSessionID() = %q, want %q", id, "")
	}
}

func TestPiTool_GetSandboxSettings(t *testing.T) {
	pi := NewPi()
	settings := pi.GetSandboxSettings()

	// Without contextFilePath, settings should be empty
	if len(settings) != 0 {
		t.Errorf("GetSandboxSettings() = %v, want empty map", settings)
	}
}

func TestPiTool_EssentialConfigFiles(t *testing.T) {
	pi := NewPi()
	tcf, ok := pi.(ToolWithConfigDirFiles)
	if !ok {
		t.Fatal("PiTool does not implement ToolWithConfigDirFiles")
	}
	files := tcf.EssentialConfigFiles()
	expected := []string{"settings.json", "models.json", "auth.json", "AGENTS.md"}
	if len(files) != len(expected) {
		t.Fatalf("EssentialConfigFiles() = %v, want %v", files, expected)
	}
	for i, f := range files {
		if f != expected[i] {
			t.Errorf("EssentialConfigFiles()[%d] = %q, want %q", i, f, expected[i])
		}
	}
}

func TestPiTool_SandboxSettingsFileName(t *testing.T) {
	pi := NewPi()
	tcf, ok := pi.(ToolWithConfigDirFiles)
	if !ok {
		t.Fatal("PiTool does not implement ToolWithConfigDirFiles")
	}
	if tcf.SandboxSettingsFileName() != "settings.json" {
		t.Errorf("SandboxSettingsFileName() = %q, want %q", tcf.SandboxSettingsFileName(), "settings.json")
	}
}

func TestPiTool_StateConfigFileName(t *testing.T) {
	pi := NewPi()
	tcf, ok := pi.(ToolWithConfigDirFiles)
	if !ok {
		t.Fatal("PiTool does not implement ToolWithConfigDirFiles")
	}
	if tcf.StateConfigFileName() != "" {
		t.Errorf("StateConfigFileName() = %q, want %q", tcf.StateConfigFileName(), "")
	}
}

func TestPiTool_AlwaysSetupConfig(t *testing.T) {
	pi := NewPi()
	tcf, ok := pi.(ToolWithConfigDirFiles)
	if !ok {
		t.Fatal("PiTool does not implement ToolWithConfigDirFiles")
	}
	if !tcf.AlwaysSetupConfig() {
		t.Error("AlwaysSetupConfig() = false, want true")
	}
}

func TestPiTool_RegistryLookup(t *testing.T) {
	pi, err := Get("pi")
	if err != nil {
		t.Fatalf("Get(\"pi\") returned error: %v", err)
	}
	if pi.Name() != "pi" {
		t.Errorf("Name() = %q, want %q", pi.Name(), "pi")
	}
}

func TestPiTool_ImplementsPermissionMode(t *testing.T) {
	pi := NewPi()

	twpm, ok := pi.(ToolWithPermissionMode)
	if !ok {
		t.Fatal("PiTool should implement ToolWithPermissionMode")
	}

	// Verify method works without panic
	twpm.SetPermissionMode("interactive")
}

func TestPiTool_ImplementsContainerEnv(t *testing.T) {
	pi := NewPi()

	twce, ok := pi.(ToolWithContainerEnv)
	if !ok {
		t.Fatal("PiTool should implement ToolWithContainerEnv")
	}

	env := twce.GetContainerEnv("/workspace")
	if env["PI_CODING_AGENT_SESSION_DIR"] != "/workspace/.pi-sessions" {
		t.Errorf("PI_CODING_AGENT_SESSION_DIR = %q, want %q", env["PI_CODING_AGENT_SESSION_DIR"], "/workspace/.pi-sessions")
	}
}

func TestPiTool_GetContainerEnv_CustomWorkspace(t *testing.T) {
	pi := &PiTool{}
	env := pi.GetContainerEnv("/home/user/project")
	if env["PI_CODING_AGENT_SESSION_DIR"] != "/home/user/project/.pi-sessions" {
		t.Errorf("PI_CODING_AGENT_SESSION_DIR = %q, want %q", env["PI_CODING_AGENT_SESSION_DIR"], "/home/user/project/.pi-sessions")
	}
}

func TestPiTool_GetSandboxSettings_WithAutoContext(t *testing.T) {
	pi := &PiTool{contextFilePath: "/home/code/SANDBOX_CONTEXT.md"}
	settings := pi.GetSandboxSettings()

	// Context injection is handled via PreLaunch (symlinks APPEND_SYSTEM.md), not settings.json
	if len(settings) != 0 {
		t.Errorf("GetSandboxSettings() = %v, want empty map", settings)
	}
}

func TestPiTool_BuildCommand_WithContext(t *testing.T) {
	pi := &PiTool{contextFilePath: "/home/code/SANDBOX_CONTEXT.md"}
	cmd := pi.BuildCommand("some-session-id", false, "")
	// BuildCommand should be clean — no shell setup, that's in PreLaunch
	expected := []string{"pi"}
	if len(cmd) != len(expected) {
		t.Fatalf("BuildCommand(with context) = %v, want %v", cmd, expected)
	}
	for i, v := range expected {
		if cmd[i] != v {
			t.Errorf("BuildCommand(with context)[%d] = %q, want %q", i, cmd[i], v)
		}
	}
}

func TestPiTool_PreLaunch_WithContext(t *testing.T) {
	pi := &PiTool{contextFilePath: "/home/code/SANDBOX_CONTEXT.md"}
	cmds := pi.PreLaunch()
	if len(cmds) != 2 {
		t.Fatalf("PreLaunch() returned %d commands, want 2", len(cmds))
	}
	// First command: mkdir -p
	expectedMkdir := []string{"mkdir", "-p", "/home/code/.pi/agent"}
	if len(cmds[0]) != len(expectedMkdir) {
		t.Fatalf("PreLaunch()[0] = %v, want %v", cmds[0], expectedMkdir)
	}
	for i, v := range expectedMkdir {
		if cmds[0][i] != v {
			t.Errorf("PreLaunch()[0][%d] = %q, want %q", i, cmds[0][i], v)
		}
	}
	// Second command: ln -sf
	expectedLn := []string{"ln", "-sf", "/home/code/SANDBOX_CONTEXT.md", "/home/code/.pi/agent/APPEND_SYSTEM.md"}
	if len(cmds[1]) != len(expectedLn) {
		t.Fatalf("PreLaunch()[1] = %v, want %v", cmds[1], expectedLn)
	}
	for i, v := range expectedLn {
		if cmds[1][i] != v {
			t.Errorf("PreLaunch()[1][%d] = %q, want %q", i, cmds[1][i], v)
		}
	}
}

func TestPiTool_PreLaunch_WithoutContext(t *testing.T) {
	pi := &PiTool{}
	cmds := pi.PreLaunch()
	if cmds != nil {
		t.Errorf("PreLaunch() = %v, want nil", cmds)
	}
}

func TestPiTool_PreLaunch_SpecialChars(t *testing.T) {
	// Paths with spaces, quotes, etc. are passed as argv elements — no shell interpretation
	pi := &PiTool{contextFilePath: "/home/code/path with spaces/SANDBOX_CONTEXT.md"}
	cmds := pi.PreLaunch()
	if len(cmds) != 2 {
		t.Fatalf("PreLaunch() returned %d commands, want 2", len(cmds))
	}
	// The path should appear verbatim, not quoted
	if cmds[1][2] != "/home/code/path with spaces/SANDBOX_CONTEXT.md" {
		t.Errorf("PreLaunch()[1][2] = %q, want path with spaces verbatim", cmds[1][2])
	}
}

func TestPiTool_SetAutoContextPath_RejectsRelative(t *testing.T) {
	pi := &PiTool{}
	// Verify interface implementation
	var _ ToolWithAutoContextPath = pi

	pi.SetAutoContextPath("relative/path/SANDBOX_CONTEXT.md")
	if pi.contextFilePath != "" {
		t.Errorf("SetAutoContextPath accepted relative path, contextFilePath = %q", pi.contextFilePath)
	}
}

func TestPiTool_SetAutoContextPath_AcceptsAbsolute(t *testing.T) {
	pi := &PiTool{}
	pi.SetAutoContextPath("/home/code/SANDBOX_CONTEXT.md")
	if pi.contextFilePath != "/home/code/SANDBOX_CONTEXT.md" {
		t.Errorf("SetAutoContextPath did not store path, contextFilePath = %q", pi.contextFilePath)
	}
}

func TestPiTool_ImplementsPreLaunch(t *testing.T) {
	pi := NewPi()
	_, ok := pi.(ToolWithPreLaunch)
	if !ok {
		t.Fatal("PiTool should implement ToolWithPreLaunch")
	}
}

func TestPiTool_DoesNotImplementAutoContextFile(t *testing.T) {
	pi := NewPi()
	_, ok := pi.(ToolWithAutoContextFile)
	if ok {
		t.Error("PiTool should NOT implement ToolWithAutoContextFile")
	}
}

func TestListSupported_IncludesPi(t *testing.T) {
	supported := ListSupported()
	found := false
	for _, name := range supported {
		if name == "pi" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListSupported() = %v, does not include 'pi'", supported)
	}
}
