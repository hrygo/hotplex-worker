package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func BuildSystemdUnit(opts InstallOptions, homeDir string) string {
	var b strings.Builder

	b.WriteString("[Unit]\n")
	b.WriteString("Description=HotPlex Worker Gateway\n")
	b.WriteString("Documentation=https://github.com/hrygo/hotplex\n")
	b.WriteString("After=network-online.target\n")
	b.WriteString("Wants=network-online.target\n\n")

	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")

	if opts.Level == LevelSystem {
		b.WriteString("User=hotplex\n")
		b.WriteString("Group=hotplex\n")
		b.WriteString("WorkingDirectory=/var/lib/hotplex\n")
	} else {
		b.WriteString("WorkingDirectory=" + homeDir + "\n")
	}

	b.WriteString("\nExecStart=" + opts.BinaryPath + " -config " + opts.ConfigPath + "\n")
	b.WriteString("ExecReload=/bin/kill -HUP $MAINPID\n")
	b.WriteString("TimeoutStopSec=30\n")
	b.WriteString("KillMode=mixed\n")
	b.WriteString("KillSignal=SIGTERM\n\n")
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=5\n\n")

	if opts.Level == LevelSystem {
		b.WriteString("# Security hardening\n")
		b.WriteString("NoNewPrivileges=true\n")
		b.WriteString("PrivateTmp=true\n")
		b.WriteString("ProtectSystem=strict\n")
		b.WriteString("ProtectHome=true\n")
		b.WriteString("ReadWritePaths=/var/lib/hotplex /var/log/hotplex\n")
		b.WriteString("LimitNOFILE=65536\n")
	} else {
		b.WriteString("LimitNOFILE=65536\n")
	}

	b.WriteString("\nStandardOutput=journal\n")
	b.WriteString("StandardError=journal\n")
	b.WriteString("SyslogIdentifier=hotplex\n\n")

	b.WriteString("[Install]\n")
	if opts.Level == LevelSystem {
		b.WriteString("WantedBy=multi-user.target\n")
	} else {
		b.WriteString("WantedBy=default.target\n")
	}

	return b.String()
}

func BuildLaunchdPlist(opts InstallOptions, homeDir string) string {
	var b strings.Builder

	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")

	label := launchdLabel(opts.Name, opts.Level)
	fmt.Fprintf(&b, "  <key>Label</key>\n  <string>%s</string>\n", label)

	fmt.Fprintf(&b, "  <key>ProgramArguments</key>\n  <array>\n")
	fmt.Fprintf(&b, "    <string>%s</string>\n", opts.BinaryPath)
	fmt.Fprintf(&b, "    <string>-config</string>\n")
	fmt.Fprintf(&b, "    <string>%s</string>\n", opts.ConfigPath)
	b.WriteString("  </array>\n")

	fmt.Fprintf(&b, "  <key>WorkingDirectory</key>\n  <string>%s</string>\n", homeDir)

	envVars := parseEnvFile(opts.EnvPath)
	if len(envVars) > 0 {
		b.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
		for _, k := range sortedEnvKeys(envVars) {
			fmt.Fprintf(&b, "    <key>%s</key>\n    <string>%s</string>\n", k, envVars[k])
		}
		b.WriteString("  </dict>\n")
	}

	b.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	b.WriteString("  <key>KeepAlive</key>\n  <true/>\n")

	logDir := filepath.Join(homeDir, ".hotplex", "logs")
	if opts.Level == LevelSystem {
		logDir = "/var/log/hotplex"
	}
	b.WriteString("  <key>StandardOutPath</key>\n")
	fmt.Fprintf(&b, "  <string>%s/launchd.stdout.log</string>\n", logDir)
	b.WriteString("  <key>StandardErrorPath</key>\n")
	fmt.Fprintf(&b, "  <string>%s/launchd.stderr.log</string>\n", logDir)

	b.WriteString("  <key>SoftResourceLimits</key>\n  <dict>\n")
	b.WriteString("    <key>NumberOfFiles</key>\n  <integer>65536</integer>\n")
	b.WriteString("  </dict>\n")

	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func launchdLabel(name string, level Level) string {
	if level == LevelUser {
		return "com.hrygo." + name + ".user"
	}
	return "com.hrygo." + name
}

func parseEnvFile(path string) map[string]string {
	if path == "" {
		return nil
	}
	env := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		return env
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		if val != "" {
			env[key] = val
		}
	}
	return env
}

func sortedEnvKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
