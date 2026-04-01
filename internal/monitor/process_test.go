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
