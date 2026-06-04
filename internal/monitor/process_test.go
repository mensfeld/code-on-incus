package monitor

import "testing"

func TestCheckEnvAccess(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected bool
	}{
		// Existing patterns - exact env commands
		{"env command", "env", true},
		{"env with args", "env VAR=val command", true},
		{"printenv", "printenv", true},
		{"printenv with arg", "printenv HOME", true},
		{"set command", "set", true},
		{"export command", "export FOO=bar", true},

		// Existing patterns - grep with secret keywords
		{"grep api key", "grep -r API_KEY /workspace", true},
		{"grep password", "grep -i password .env", true},
		{"grep secret", "grep -r secret /workspace", true},
		{"grep token", "grep token config.yml", true},

		// Existing patterns - /proc/*/environ
		{"cat proc environ", "cat /proc/1/environ", true},
		{"cat proc self environ", "cat /proc/self/environ", true},

		// New patterns - expanded secret keywords
		{"grep credential", "grep credential config.yaml", true},
		{"grep auth", "grep auth_token .env", true},
		{"awk with secret keyword", "awk '/password/' .env", true},
		{"sed with token keyword", "sed -n '/token/p' config.yml", true},

		// New patterns - language-specific environ access
		{"python os.environ", "python3 -c 'import os; print(os.environ)'", true},
		{"python os.getenv", "python -c 'import os; os.getenv(\"SECRET\")'", true},
		{"node process.env", "node -e 'console.log(process.env)'", true},
		{"ruby ENV", "ruby -e 'puts ENV[\"SECRET\"]'", true},
		{"awk ENVIRON", "awk 'BEGIN{for(k in ENVIRON) print k, ENVIRON[k]}'", true},

		// New patterns - binary tools on /proc environ
		{"strings proc environ", "strings /proc/1/environ", true},
		{"xxd proc environ", "xxd /proc/1/environ", true},
		{"hexdump proc environ", "hexdump /proc/self/environ", true},
		{"xargs proc environ", "xargs -0 -a /proc/1/environ", true},
		{"xargs proc path", "xargs -0 < /proc/1/environ", true},

		// Negatives - should NOT match
		{"normal grep", "grep -r TODO /workspace", false},
		{"normal python", "python3 script.py", false},
		{"normal node", "node server.js", false},
		{"normal awk", "awk '{print $1}' file.txt", false},
		{"cat normal file", "cat /etc/hosts", false},
		{"sleep command", "sleep 30", false},
		{"ls command", "ls -la", false},
		{"environment in path", "/opt/environment/bin/run", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkEnvAccess(tt.command)
			if got != tt.expected {
				t.Errorf("checkEnvAccess(%q) = %v, want %v", tt.command, got, tt.expected)
			}
		})
	}
}

func TestParseStatusFields(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantPPID int
		wantUID  int
	}{
		{
			name: "typical process",
			input: "Name:\tbash\nPid:\t42\nPPid:\t1\nUid:\t1000\t1000\t1000\t1000\n",
			wantName: "bash", wantPPID: 1, wantUID: 1000,
		},
		{
			name: "root process",
			input: "Name:\tinit\nPPid:\t0\nUid:\t0\t0\t0\t0\n",
			wantName: "init", wantPPID: 0, wantUID: 0,
		},
		{
			name: "missing fields",
			input: "Name:\tsleep\n",
			wantName: "sleep", wantPPID: 0, wantUID: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotPPID, gotUID := parseStatusFields(tt.input)
			if gotName != tt.wantName {
				t.Errorf("name: got %q want %q", gotName, tt.wantName)
			}
			if gotPPID != tt.wantPPID {
				t.Errorf("ppid: got %d want %d", gotPPID, tt.wantPPID)
			}
			if gotUID != tt.wantUID {
				t.Errorf("uid: got %d want %d", gotUID, tt.wantUID)
			}
		})
	}
}

func TestParseCmdline(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "simple command",
			input: []byte("bash\x00"),
			want:  "bash",
		},
		{
			name:  "command with args",
			input: []byte("python3\x00-c\x00import os\x00"),
			want:  "python3 -c import os",
		},
		{
			name:  "no trailing null",
			input: []byte("sleep\x0030"),
			want:  "sleep 30",
		},
		{
			name:  "empty",
			input: []byte("\x00"),
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCmdline(tt.input)
			if got != tt.want {
				t.Errorf("parseCmdline(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseStatusFields_TabAndSpaceDelimited(t *testing.T) {
	// /proc/<pid>/status uses a tab between key and value, but be robust.
	input := "Name:   nginx\nPPid:   7\nUid:    33  33  33  33\n"
	name, ppid, uid := parseStatusFields(input)
	if name != "nginx" {
		t.Errorf("name: got %q want %q", name, "nginx")
	}
	if ppid != 7 {
		t.Errorf("ppid: got %d want 7", ppid)
	}
	if uid != 33 {
		t.Errorf("uid: got %d want 33", uid)
	}
}

func TestParseCmdline_KernelThread(t *testing.T) {
	// Kernel threads have empty cmdline; fall back to Name from status.
	// parseCmdline returns "" for empty input; caller uses Name as fallback.
	got := parseCmdline([]byte{})
	if got != "" {
		t.Errorf("expected empty string for kernel thread, got %q", got)
	}
}

