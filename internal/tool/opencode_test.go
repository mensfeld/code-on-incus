package tool

import (
	"testing"
)

func TestOpencodeTool_Basics(t *testing.T) {
	oc := NewOpencode()

	if oc.Name() != "opencode" {
		t.Errorf("Name() = %q, want %q", oc.Name(), "opencode")
	}
	if oc.Binary() != "opencode" {
		t.Errorf("Binary() = %q, want %q", oc.Binary(), "opencode")
	}
	if oc.ConfigDirName() != "" {
		t.Errorf("ConfigDirName() = %q, want %q", oc.ConfigDirName(), "")
	}
	if oc.SessionsDirName() != "sessions-opencode" {
		t.Errorf("SessionsDirName() = %q, want %q", oc.SessionsDirName(), "sessions-opencode")
	}
}

func TestOpencodeTool_BuildCommand_NewSession(t *testing.T) {
	oc := NewOpencode()
	cmd := oc.BuildCommand("some-session-id", false, "")
	if len(cmd) != 1 || cmd[0] != "opencode" {
		t.Errorf("BuildCommand(new) = %v, want [opencode]", cmd)
	}
}

func TestOpencodeTool_BuildCommand_Resume(t *testing.T) {
	oc := NewOpencode()
	// opencode auto-continues from workspace .opencode/ SQLite, no flag needed
	cmd := oc.BuildCommand("", true, "")
	if len(cmd) != 1 || cmd[0] != "opencode" {
		t.Errorf("BuildCommand(resume) = %v, want [opencode]", cmd)
	}
}

func TestOpencodeTool_BuildCommand_ResumeWithID(t *testing.T) {
	oc := NewOpencode()
	// opencode auto-continues from workspace .opencode/ SQLite, no flag needed
	cmd := oc.BuildCommand("", true, "some-id")
	if len(cmd) != 1 || cmd[0] != "opencode" {
		t.Errorf("BuildCommand(resume with ID) = %v, want [opencode]", cmd)
	}
}

func TestOpencodeTool_DiscoverSessionID(t *testing.T) {
	oc := NewOpencode()
	id := oc.DiscoverSessionID("/some/path")
	if id != "" {
		t.Errorf("DiscoverSessionID() = %q, want %q", id, "")
	}
}

func TestOpencodeTool_GetSandboxSettings(t *testing.T) {
	oc := NewOpencode()
	settings := oc.GetSandboxSettings()

	perm, ok := settings["permission"]
	if !ok {
		t.Fatal("GetSandboxSettings() missing 'permission' key")
	}
	permMap, ok := perm.(map[string]interface{})
	if !ok {
		t.Fatalf("'permission' value is %T, want map[string]interface{}", perm)
	}
	val, ok := permMap["*"]
	if !ok {
		t.Fatal("permission map missing '*' key")
	}
	if val != "allow" {
		t.Errorf("permission['*'] = %q, want %q", val, "allow")
	}
}

func TestOpencodeTool_HomeConfigFileName(t *testing.T) {
	oc := NewOpencode()
	// Type-assert to ToolWithHomeConfigFile
	twh, ok := oc.(ToolWithHomeConfigFile)
	if !ok {
		t.Fatal("OpencodeTool does not implement ToolWithHomeConfigFile")
	}
	if twh.HomeConfigFileName() != ".opencode.json" {
		t.Errorf("HomeConfigFileName() = %q, want %q", twh.HomeConfigFileName(), ".opencode.json")
	}
}

func TestOpencodeTool_RegistryLookup(t *testing.T) {
	oc, err := Get("opencode")
	if err != nil {
		t.Fatalf("Get(\"opencode\") returned error: %v", err)
	}
	if oc.Name() != "opencode" {
		t.Errorf("Name() = %q, want %q", oc.Name(), "opencode")
	}
}

func TestListSupported_IncludesOpencode(t *testing.T) {
	supported := ListSupported()
	found := false
	for _, name := range supported {
		if name == "opencode" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListSupported() = %v, does not include 'opencode'", supported)
	}
}
