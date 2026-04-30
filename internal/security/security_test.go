package security

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

// ─── Command whitelist ────────────────────────────────────────────────────────

func TestBuildSafeCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		binary  string
		args    []string
		wantErr bool
	}{
		{"claude allowed", "claude", []string{"--print", "--session-id", "sess_abc"}, false},
		{"opencode allowed", "opencode", []string{"run"}, false},
		{"opencode with args", "opencode", []string{"--model", "claude-sonnet-4-6"}, false},
		{"rm rejected", "rm", nil, true},
		{"bash rejected", "bash", nil, true},
		{"sh rejected", "sh", nil, true},
		{"python rejected", "python", nil, true},
		{"empty binary", "", nil, true},
		{"docker rejected", "docker", nil, true},
		{"curl rejected", "curl", nil, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, err := BuildSafeCommand(tt.binary, tt.args...)
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, cmd)
			} else {
				require.NoError(t, err)
				require.NotNil(t, cmd)
				// Binary name should match (exec.Command resolves to absolute path from PATH)
				require.Equal(t, tt.binary, filepath.Base(cmd.Path))
			}
		})
	}
}

func TestValidateCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmd     string
		wantErr bool
	}{
		{"claude valid", "claude", false},
		{"opencode valid", "opencode", false},
		{"empty rejected", "", true},
		{"rm rejected", "rm", true},
		{"curl rejected", "curl", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCommand(tt.cmd)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ─── Dangerous characters ─────────────────────────────────────────────────────

func TestContainsDangerousChars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"plain text", "hello world", true},
		{"valid session id", "sess_abc123", false},
		{"semicolon", "a;b", true},
		{"pipe", "a|b", true},
		{"ampersand", "a&b", true},
		{"backtick command sub", "`id`", true},
		{"dollar parens", "$(id)", true},
		{"backslash", "a\\b", true},
		{"newline", "a\nb", true},
		{"carriage return", "a\rb", true},
		{"parentheses", "(echo)", true},
		{"braces", "{echo}", true},
		{"brackets", "[echo]", true},
		{"angle brackets", "<file", true},
		{"greater than", ">", true},
		{"exclamation", "!", true},
		{"hash comment", "#comment", true},
		{"tilde", "~", true},
		{"star", "*", true},
		{"question mark", "?", true},
		{"space", "hello world", true},
		{"tab", "hello\tworld", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ContainsDangerousChars(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

// ─── ValidateWorkDir ──────────────────────────────────────────────────────────

func TestValidateWorkDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		dir     string
		wantErr bool
	}{
		{"empty rejected", "", true},
		{"relative rejected", "relative/path", true},
		{"root forbidden", "/", true},
		{"usr forbidden", "/usr", true},
		{"usr sub forbidden", "/usr/local/bin", true},
		{"etc forbidden", "/etc", true},
		{"etc sub forbidden", "/etc/nginx/conf.d", true},
		{"bin forbidden", "/bin", true},
		{"sbin forbidden", "/sbin", true},
		{"boot forbidden", "/boot", true},
		{"dev forbidden", "/dev", true},
		{"proc forbidden", "/proc", true},
		{"sys forbidden", "/sys", true},
		{"lib forbidden", "/lib", true},
		{"lib64 forbidden", "/lib64", true},
		{"root home forbidden", "/root", true},
		{"home forbidden", "/home", true},
		{"run forbidden", "/run", true},
		{"srv forbidden", "/srv", true},
		{"System forbidden (macOS)", "/System", true},
		{"tmp dir allowed", tmpDir, false},
		{"non-existent clean path allowed", filepath.Join(tmpDir, "nonexistent", "project"), false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateWorkDir(tt.dir)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ─── Path safety ─────────────────────────────────────────────────────────────

func TestSafePathJoin(t *testing.T) {
	t.Parallel()

	// Use an existing system directory for real path resolution tests.
	// Note: filepath.EvalSymlinks on non-existent paths returns an error,
	// so we use /tmp which always exists.
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		base    string
		user    string
		wantErr bool
	}{
		{
			name:    "simple relative path",
			base:    tmpDir,
			user:    "project1/file.txt",
			wantErr: false,
		},
		{
			name:    "dot path",
			base:    tmpDir,
			user:    "./file.txt",
			wantErr: false,
		},
		{
			name:    "double dot traversal rejected",
			base:    tmpDir,
			user:    "../etc/passwd",
			wantErr: true,
		},
		{
			name:    "absolute path rejected",
			base:    tmpDir,
			user:    "/etc/passwd",
			wantErr: true,
		},
		{
			name:    "absolute with traversal rejected",
			base:    tmpDir,
			user:    "/var/hotplex/../../../etc/passwd",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// SafePathJoin calls filepath.EvalSymlinks which requires the path to exist.
			// Create the file so EvalSymlinks succeeds for non-error cases.
			if !tt.wantErr {
				path := filepath.Join(tt.base, tt.user)
				dir := filepath.Dir(path)
				if dir != tt.base {
					_ = os.MkdirAll(dir, 0755)
				}
				_ = os.WriteFile(path, []byte("test"), 0644)
			}
			_, err := SafePathJoin(tt.base, tt.user)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateBaseDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		base    string
		wantErr bool
	}{
		{"var hotplex projects allowed", "/var/hotplex/projects", false},
		{"tmp hotplex allowed", config.TempBaseDir(), false},
		{"home rejected", "/home/user", true},
		{"root rejected", "/", true},
		{"empty rejected", "", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateBaseDir(tt.base)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ─── Tool validation ──────────────────────────────────────────────────────────

func TestValidateTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tools   []string
		wantErr bool
	}{
		{"empty list allowed", []string{}, false},
		{"single valid tool", []string{"Read"}, false},
		{"multiple valid tools", []string{"Read", "Edit", "Bash"}, false},
		{"all allowed tools", []string{"Read", "Edit", "Write", "Bash", "Grep", "Glob", "Agent", "WebFetch", "NotebookEdit", "TodoWrite"}, false},
		{"unknown tool rejected", []string{"Exec"}, true},
		{"mixed valid and invalid", []string{"Read", "Exec", "Bash"}, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTools(tt.tools)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIsToolAllowed(t *testing.T) {
	t.Parallel()

	require.True(t, IsToolAllowed("Read"))
	require.True(t, IsToolAllowed("Bash"))
	require.False(t, IsToolAllowed("Exec"))
	require.False(t, IsToolAllowed(""))
}

func TestBuildAllowedToolsArgs(t *testing.T) {
	t.Parallel()

	args := BuildAllowedToolsArgs([]string{"Read", "Bash"})
	require.Equal(t, []string{"--allowed-tools", "Read", "--allowed-tools", "Bash"}, args)

	argsEmpty := BuildAllowedToolsArgs([]string{})
	require.Empty(t, argsEmpty)
}

// ─── Model validation ─────────────────────────────────────────────────────────

func TestValidateModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		model   string
		wantErr bool
	}{
		{"claude-sonnet-4-6 allowed", "claude-sonnet-4-6", false},
		{"claude-opus-4-6 allowed", "claude-opus-4-6", false},
		{"claude-3-5-sonnet allowed", "claude-3-5-sonnet-20241022", false},
		{"case insensitive", "CLAUDE-SONNET-4-6", false},
		{"empty rejected", "", true},
		{"unknown model rejected", "gpt-4o", true},
		{"random string rejected", "abc123", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateModel(tt.model)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIsModelAllowed(t *testing.T) {
	t.Parallel()

	require.True(t, IsModelAllowed("claude-sonnet-4-6"))
	require.True(t, IsModelAllowed("CLAUDE-SONNET-4-6"))
	require.False(t, IsModelAllowed("gpt-4o"))
	require.False(t, IsModelAllowed(""))
}

// ─── Bash command policy ──────────────────────────────────────────────────────

func TestCheckBashCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmd     string
		wantNil bool
		wantP0  bool
		wantP1  bool
	}{
		{"empty nil", "", true, false, false},
		{"harmless ls", "ls -la /tmp", true, false, false},
		{"harmless echo", "echo hello", true, false, false},
		{"rm -rf / P0", "rm -rf /", false, true, false},
		{"rm -rf / with spaces P0", "  rm  -rf  /  ", false, true, false},
		{"dd of=/dev/sda P0", "dd if=/dev/zero of=/dev/sda", false, true, false},
		{"mkfs P0", "mkfs.ext4 /dev/sda1", false, true, false},
		{"fdisk P0", "fdisk -l", false, true, false},
		{"fork bomb P0", ":(){:|:}", false, true, false},
		{"SSH key file P1", "scp -i ~/.ssh/id_rsa file.txt host:/tmp", false, false, true},
		{"reading ssh keys P1", "cat ~/.ssh/authorized_keys", false, false, true},
		{"gh auth token P1", "gh auth token", false, false, true},
		{"curl AWS metadata P1", "curl http://169.254.169.254/latest/meta-data/", false, false, true},
		{"wget AWS metadata P1", "wget -qO- http://169.254.169.254/", false, false, true},
		{"curl GCP metadata P1", "curl metadata.google.internal", false, false, true},
		{"crontab -e P1", "crontab -e", false, false, true},
		{"authorized_keys persistence P1", "echo 'ssh-rsa AAAA...' >> ~/.ssh/authorized_keys", false, false, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			violation := CheckBashCommand(tt.cmd)
			if tt.wantNil {
				require.Nil(t, violation)
			} else {
				require.NotNil(t, violation)
				if tt.wantP0 {
					require.True(t, violation.IsAutoDeny())
					require.Equal(t, "P0", violation.Severity)
				}
				if tt.wantP1 {
					require.False(t, violation.IsAutoDeny())
					require.Equal(t, "P1", violation.Severity)
				}
			}
		})
	}
}

func TestBashPolicyViolation_IsAutoDeny(t *testing.T) {
	t.Parallel()

	p0 := &BashPolicyViolation{Severity: "P0", Reason: "test", Command: "rm -rf /"}
	require.True(t, p0.IsAutoDeny())

	p1 := &BashPolicyViolation{Severity: "P1", Reason: "test", Command: "curl 169.254.169.254"}
	require.False(t, p1.IsAutoDeny())

	nilViolation := (*BashPolicyViolation)(nil)
	require.False(t, nilViolation.IsAutoDeny())
}

// ─── SSRF protection ─────────────────────────────────────────────────────────

func TestValidateURL(t *testing.T) {
	// Mock DNS resolution to avoid dependency on external DNS servers.
	prevLookupHost := LookupHost
	LookupHost = func(host string) ([]string, error) {
		if host == "example.com" {
			return []string{"93.184.216.34"}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	t.Cleanup(func() { LookupHost = prevLookupHost })

	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		// Allowed URLs
		{"https example.com", "https://example.com", false},
		{"http example.com", "http://example.com", false},
		{"https with path", "https://example.com/api/v1/users", false},
		{"https with query", "https://example.com/search?q=golang", false},
		{"https with port", "https://example.com:8443/api", false},

		// Protocol rejections
		{"ftp rejected", "ftp://example.com", true},
		{"file rejected", "file:///etc/passwd", true},
		{"javascript rejected", "javascript:alert(1)", true},
		{"data url rejected", "data:text/html,<script>alert(1)</script>", true},

		// Empty host
		{"empty host", "http://", true},
		{"missing scheme", "example.com", true},

		// Blocked IP: loopback
		{"loopback 127.0.0.1 blocked", "http://127.0.0.1/", true},
		{"loopback 127.1 blocked", "http://127.1/", true},

		// Blocked IP: private network 10.x.x.x
		{"private 10.x.x.x blocked", "http://10.0.0.1/", true},
		{"private 10.255.255.254 blocked", "http://10.255.255.254/", true},

		// Blocked IP: 172.16.x.x
		{"private 172.16.0.0 blocked", "http://172.16.0.1/", true},
		{"private 172.31.255.255 blocked", "http://172.31.255.255/", true},

		// Blocked IP: 192.168.x.x
		{"private 192.168.0.1 blocked", "http://192.168.0.1/", true},
		{"private 192.168.255.255 blocked", "http://192.168.255.255/", true},

		// Blocked IP: cloud metadata
		{"AWS metadata blocked", "http://169.254.169.254/", true},
		{"Alibaba metadata blocked", "http://100.100.100.200/", true},

		// Public IPs should pass (if not in a blocked range)
		{"public ip allowed", "https://1.1.1.1/", false},
		{"cloudflare DNS allowed", "https://1.1.1.1/cdn-cgi/trace", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateURL(tt.url)
			if tt.wantErr {
				require.Error(t, err)
				ssrfErr := new(SSRFProtectionError)
				require.ErrorAs(t, err, &ssrfErr)
				require.NotEmpty(t, ssrfErr.Reason)
				require.NotEmpty(t, ssrfErr.URL)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSSRFProtectionError_Error(t *testing.T) {
	t.Parallel()

	err := &SSRFProtectionError{
		URL:     "http://127.0.0.1/",
		Reason:  "bare IP in URL is blocked",
		Blocked: "127.0.0.1",
	}
	msg := err.Error()
	require.Contains(t, msg, "SSRF blocked")
	require.Contains(t, msg, "127.0.0.1")
	require.Contains(t, msg, "bare IP")
}

func TestIsIPBlocked(t *testing.T) {
	t.Parallel()

	blocked := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("10.0.0.1"),
		net.ParseIP("172.16.0.1"),
		net.ParseIP("192.168.1.1"),
		net.ParseIP("169.254.169.254"),
		net.ParseIP("::1"),
	}
	for _, ip := range blocked {
		require.True(t, isIPBlocked(ip), "expected %v to be blocked", ip)
	}

	allowed := []net.IP{
		net.ParseIP("8.8.8.8"),
		net.ParseIP("1.1.1.1"),
		net.ParseIP("140.82.112.4"), // github.com
	}
	for _, ip := range allowed {
		require.False(t, isIPBlocked(ip), "expected %v to be allowed", ip)
	}
}

// ─── Environment sanitization ─────────────────────────────────────────────────

func TestIsSensitive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n    string
		key  string
		want bool
	}{
		// AWS
		{"AWS_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID", true},
		{"AWS_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY", true},
		{"AWS_SESSION_TOKEN", "AWS_SESSION_TOKEN", true},
		// GitHub
		{"GITHUB_TOKEN", "GITHUB_TOKEN", true},
		{"GH_TOKEN", "GH_TOKEN", true},
		// Azure
		{"AZURE_CLIENT_ID", "AZURE_CLIENT_ID", true},
		{"AZURE_CLIENT_SECRET", "AZURE_CLIENT_SECRET", true},
		// API keys
		{"OPENAI_API_KEY", "OPENAI_API_KEY", true},
		{"ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY", true},
		{"GOOGLE_API_KEY", "GOOGLE_API_KEY", true},
		// Generic secrets
		{"API_SECRET", "API_SECRET", true},
		{"PRIVATE_KEY", "PRIVATE_KEY", true},
		{"SECRET_TOKEN", "SECRET_TOKEN", true},
		// Database
		{"DATABASE_URL", "DATABASE_URL", true},
		{"DB_PASSWORD", "DB_PASSWORD", true},
		{"POSTGRES_PASSWORD", "POSTGRES_PASSWORD", true},
		// Other
		{"SLACK_TOKEN", "SLACK_TOKEN", true},
		{"SENTRY_DSN", "SENTRY_DSN", true},
		// Safe
		{"HOME", "HOME", false},
		{"PATH", "PATH", false},
		{"USER", "USER", false},
		{"GOROOT", "GOROOT", false},
		{"GOPATH", "GOPATH", false},
		{"PWD matched by pwd$ pattern", "PWD", true},
		{"LANG", "LANG", false},
		{"CLAUDE_CODE", "CLAUDE_CODE", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.n, func(t *testing.T) {
			t.Parallel()
			got := IsSensitive(tt.key)
			require.Equal(t, tt.want, got, "IsSensitive(%q)", tt.key)
		})
	}
}

func TestBuildWorkerEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     map[string]string
		whitelist []string
		check     func(t *testing.T, result map[string]string)
	}{
		{
			name:      "nil input",
			input:     nil,
			whitelist: nil,
			check: func(t *testing.T, result map[string]string) {
				require.Empty(t, result)
			},
		},
		{
			name: "protected vars stripped",
			input: map[string]string{
				"CLAUDECODE":    "nested-agent-value",
				"GATEWAY_ADDR":  "localhost:8888",
				"GATEWAY_TOKEN": "secret-token",
			},
			whitelist: nil,
			check: func(t *testing.T, result map[string]string) {
				require.NotContains(t, result, "CLAUDECODE")
				require.NotContains(t, result, "GATEWAY_ADDR")
				require.NotContains(t, result, "GATEWAY_TOKEN")
			},
		},
		{
			name: "sensitive vars redacted",
			input: map[string]string{
				"HOME":           "/home/user",
				"PATH":           "/usr/bin",
				"GITHUB_TOKEN":   "ghs_secret",
				"OPENAI_API_KEY": "sk-secret",
			},
			whitelist: nil,
			check: func(t *testing.T, result map[string]string) {
				require.Equal(t, "/home/user", result["HOME"])
				require.Equal(t, "/usr/bin", result["PATH"])
				require.Equal(t, "[REDACTED]", result["GITHUB_TOKEN"])
				require.Equal(t, "[REDACTED]", result["OPENAI_API_KEY"])
			},
		},
		{
			name: "whitelisted sensitive vars pass through",
			input: map[string]string{
				"GITHUB_TOKEN": "ghs_secret",
				"AWS_API_KEY":  "ak_test",
			},
			whitelist: []string{"GITHUB_TOKEN"},
			check: func(t *testing.T, result map[string]string) {
				require.Equal(t, "ghs_secret", result["GITHUB_TOKEN"])
				require.Equal(t, "[REDACTED]", result["AWS_API_KEY"])
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := BuildWorkerEnv(tt.input, tt.whitelist)
			tt.check(t, result)
		})
	}
}

func TestStripNestedAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  []string
		want []string
	}{
		{
			name: "no CLAUDECODE",
			env:  []string{"HOME=/home/user", "PATH=/usr/bin"},
			want: []string{"HOME=/home/user", "PATH=/usr/bin"},
		},
		{
			name: "CLAUDECODE stripped",
			env:  []string{"HOME=/home/user", "CLAUDECODE=inner-agent", "PATH=/usr/bin"},
			want: []string{"HOME=/home/user", "PATH=/usr/bin"},
		},
		{
			name: "only CLAUDECODE",
			env:  []string{"CLAUDECODE=inner"},
			want: []string{},
		},
		{
			name: "multiple CLAUDECODE variants stripped",
			env:  []string{"CLAUDECODE=val1", "CLAUDECODE=val2", "PATH=/bin"},
			want: []string{"PATH=/bin"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StripNestedAgent(tt.env)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSanitizeArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"printable only", "abc123!@#", "abc123!@#"},
		{"spaces kept", "hello world", "hello world"}, // space (r=32) is printable ASCII
		{"newlines removed", "line1\nline2", "line1line2"},
		{"tabs removed", "col1\tcol2", "col1col2"},
		{"null byte removed", "hel\x00lo", "hello"},
		{"ANSI escape partially kept", "test\x1b[31m", "test[31m"}, // [, ], digits kept; \x1b (27) removed
		{"unicode removed", "hello\u4e16", "hello"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SanitizeArg(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}
