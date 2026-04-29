//go:build darwin || linux

package security

// AllowedBaseDirs is the set of permitted base directories for session work dirs.
var AllowedBaseDirs = map[string]bool{
	"/var/hotplex/projects": true,
	"/tmp/hotplex":          true,
}

// ForbiddenWorkDirs are system directories that must never be used as session work dirs.
var ForbiddenWorkDirs = []string{
	"/bin",    // FHS: essential user binaries
	"/sbin",   // FHS: essential system binaries
	"/usr",    // FHS: system-wide read-only programs & libraries
	"/etc",    // FHS: system configuration
	"/boot",   // FHS: kernel & bootloader
	"/lib",    // FHS: shared libraries
	"/lib64",  // FHS: 64-bit shared libraries
	"/root",   // FHS: superuser home (systemd ProtectHome)
	"/home",   // FHS: user homes (systemd ProtectHome)
	"/System", // macOS SIP: system files
	"/dev",    // POSIX: device files
	"/proc",   // Linux: process & kernel info
	"/sys",    // Linux: kernel objects
	"/run",    // FHS: runtime data (PID files, sockets, locks)
	"/srv",    // FHS: service data
}
