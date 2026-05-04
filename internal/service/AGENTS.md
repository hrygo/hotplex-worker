# Service Management Package

## OVERVIEW
Cross-platform system service management (systemd/launchd/Windows SCM). Template-based unit/plist generation with user-level and system-level support. Privilege detection and log directory resolution.

## STRUCTURE
```
service/
  service.go           # Manager interface, InstallOptions, Status, Level type, ParseLevel, ResolveBinaryPath, LogDir
  manager_linux.go     # linuxManager: systemd unit install/uninstall/status/start/stop/restart/logs
  manager_darwin.go    # darwinManager: launchd plist install/uninstall/status/start/stop/restart/logs
  manager_windows.go   # windowsManager: Windows SCM install/uninstall/status/start/stop/restart/logs
  templates.go         # BuildSystemdUnit, BuildLaunchdPlist, resolveWorkDir, logDirForHome
  admin_windows.go     # IsPrivileged (Windows admin check)
  admin_other.go       # IsPrivileged (POSIX uid check)
  logdir_windows.go    # systemLogDir (Windows event log)
  logdir_other.go      # systemLogDir (/var/log)
  templates_test.go    # Template generation tests, ParseLevel tests
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Manager interface | `service.go` Manager | Install, Uninstall, Status, Start, Stop, Restart, Logs |
| Install options | `service.go:28` InstallOptions | BinaryPath, ConfigPath, Level, Name, WorkDir |
| Service level | `service.go` Level | User vs System (affects install path and privileges) |
| Systemd unit template | `templates.go:17` BuildSystemdUnit | User: ~/.config/systemd, System: /etc/systemd |
| Launchd plist template | `templates.go:78` BuildLaunchdPlist | User: ~/Library/LaunchAgents, System: /Library/LaunchDaemons |
| Windows SCM | `manager_windows.go` | golang.org/x/sys/windows/svc/mgr for service control |
| Binary path resolution | `service.go:55` ResolveBinaryPath | os.Executable → symlink resolve → /usr/local/bin fallback |
| Log directory | `service.go:81` LogDir | Platform + level dependent |
| Privilege check | `admin_*.go` IsPrivileged | Windows: admin group, POSIX: uid == 0 |

## KEY PATTERNS

**Platform-conditional compilation**: `manager_linux.go`, `manager_darwin.go`, `manager_windows.go` with build tags. `NewManager()` returns platform-specific implementation.

**Template-based config generation**: BuildSystemdUnit/BuildLaunchdPlist generate complete unit/plist files from InstallOptions. Includes: WorkingDirectory, ExecStart, log paths, auto-restart, environment variables.

**User vs System level**: User-level installs to home directory (no root). System-level requires elevated privileges. Affects unit path, log directory, and plist label.

**Linger enablement** (Linux): User-level systemd auto-enables linger for the user to ensure service survives logout.

**Logs subcommand**: Platform-specific log access — journalctl (Linux), log show (macOS), Windows event log.

## ANTI-PATTERNS
- ❌ Hard-code service paths — use template generation functions
- ❌ Install system-level service without privilege check — IsPrivileged() gate required
- ❌ Assume systemd on Linux — verify via systemctl availability
- ❌ Skip WorkDir in service unit — worker processes need WorkingDirectory
