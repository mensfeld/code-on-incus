package tool

import (
	"testing"
)

func TestOmpTool_Basics(t *testing.T) {
	omp := NewOmp()

	if omp.Name() != "omp" {
		t.Errorf("Name() = %q, want %q", omp.Name(), "omp")
	}
	if omp.Binary() != "omp" {
		t.Errorf("Binary() = %q, want %q", omp.Binary(), "omp")
	}
	if omp.ConfigDirName() != ".omp" {
		t.Errorf("ConfigDirName() = %q, want %q", omp.ConfigDirName(), ".omp")
	}
	if omp.SessionsDirName() != "sessions-omp" {
		t.Errorf("SessionsDirName() = %q, want %q", omp.SessionsDirName(), "sessions-omp")
	}
}

func TestOmpTool_BuildCommand_NewSession(t *testing.T) {
	omp := NewOmp()
	cmd := omp.BuildCommand("some-session-id", false, "")
	if len(cmd) != 1 || cmd[0] != "omp" {
		t.Errorf("BuildCommand(new) = %v, want [omp]", cmd)
	}
}

func TestOmpTool_BuildCommand_Resume(t *testing.T) {
	omp := NewOmp()
	cmd := omp.BuildCommand("", true, "")
	expected := []string{"omp", "--continue"}
	if len(cmd) != len(expected) {
		t.Fatalf("BuildCommand(resume) = %v, want %v", cmd, expected)
	}
	for i, v := range expected {
		if cmd[i] != v {
			t.Errorf("BuildCommand(resume)[%d] = %q, want %q", i, cmd[i], v)
		}
	}
}

func TestOmpTool_DiscoverSessionID(t *testing.T) {
	omp := NewOmp()
	id := omp.DiscoverSessionID("/some/path")
	if id != "" {
		t.Errorf("DiscoverSessionID() = %q, want %q", id, "")
	}
}

func TestOmpTool_GetSandboxSettings(t *testing.T) {
	omp := NewOmp()
	settings := omp.GetSandboxSettings()
	if len(settings) != 0 {
		t.Errorf("GetSandboxSettings() = %v, want empty map", settings)
	}
}

func TestOmpTool_EssentialConfigFiles(t *testing.T) {
	omp := NewOmp()
	tcf, ok := omp.(ToolWithConfigDirFiles)
	if !ok {
		t.Fatal("OmpTool does not implement ToolWithConfigDirFiles")
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

func TestOmpTool_SandboxSettingsFileName(t *testing.T) {
	omp := NewOmp()
	tcf, ok := omp.(ToolWithConfigDirFiles)
	if !ok {
		t.Fatal("OmpTool does not implement ToolWithConfigDirFiles")
	}
	if tcf.SandboxSettingsFileName() != "settings.json" {
		t.Errorf("SandboxSettingsFileName() = %q, want %q", tcf.SandboxSettingsFileName(), "settings.json")
	}
}

func TestOmpTool_AlwaysSetupConfig(t *testing.T) {
	omp := NewOmp()
	tcf, ok := omp.(ToolWithConfigDirFiles)
	if !ok {
		t.Fatal("OmpTool does not implement ToolWithConfigDirFiles")
	}
	if !tcf.AlwaysSetupConfig() {
		t.Error("AlwaysSetupConfig() = false, want true")
	}
}

func TestOmpTool_RegistryLookup(t *testing.T) {
	omp, err := Get("omp")
	if err != nil {
		t.Fatalf("Get(\"omp\") returned error: %v", err)
	}
	if omp.Name() != "omp" {
		t.Errorf("Name() = %q, want %q", omp.Name(), "omp")
	}
}

func TestOmpTool_PreLaunch_WithContext(t *testing.T) {
	omp := &OmpTool{contextFilePath: "/home/code/SANDBOX_CONTEXT.md"}
	cmds := omp.PreLaunch()
	if len(cmds) != 2 {
		t.Fatalf("PreLaunch() returned %d commands, want 2", len(cmds))
	}
	expectedMkdir := []string{"mkdir", "-p", "/home/code/.omp"}
	if len(cmds[0]) != len(expectedMkdir) {
		t.Fatalf("PreLaunch()[0] = %v, want %v", cmds[0], expectedMkdir)
	}
	expectedLn := []string{"ln", "-sf", "/home/code/SANDBOX_CONTEXT.md", "/home/code/.omp/APPEND_SYSTEM.md"}
	if len(cmds[1]) != len(expectedLn) {
		t.Fatalf("PreLaunch()[1] = %v, want %v", cmds[1], expectedLn)
	}
}

func TestListSupported_IncludesOmp(t *testing.T) {
	supported := ListSupported()
	found := false
	for _, name := range supported {
		if name == "omp" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListSupported() = %v, does not include 'omp'", supported)
	}
}
