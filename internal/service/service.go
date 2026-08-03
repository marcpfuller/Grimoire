// Package service manages the Grimoire daemon as a platform-native background
// service (systemd user service on Linux, launchd LaunchAgent on macOS).
//
// The Manager interface abstracts the platform-specific operations so the CLI
// layer stays portable. Use New() to obtain the correct implementation for the
// current runtime OS.
package service

import "io"

// StatusInfo holds the runtime state of the Grimoire daemon service.
type StatusInfo struct {
	Installed bool
	Running   bool
	PID       int    // 0 if not running
	LogTail   string // last 10 lines from the service log file; empty if unreadable
}

// Manager performs platform-specific service lifecycle operations.
type Manager interface {
	// Install writes the platform unit file, copies the current config to the
	// canonical service config path, and enables + starts the service.
	// Returns an error if the service is already installed.
	Install() error

	// Uninstall stops the running service (if any), disables it, and removes
	// the unit file.
	Uninstall() error

	// Stop stops the running service without removing or disabling it.
	// The service will start again on the next login/boot or when started
	// explicitly. Returns an error if the service is not installed.
	Stop() error

	// Restart performs a platform-native service restart. Active shell hooks
	// will silently tolerate the ~2s gap during restart.
	Restart() error

	// Status returns the current runtime state of the service.
	Status() (StatusInfo, error)
}

// PrintStatus writes a human-readable status block to w.
func PrintStatus(w io.Writer, info StatusInfo) {
	state := "stopped"
	if info.Running {
		state = "running"
	}

	installed := "no"
	if info.Installed {
		installed = "yes"
	}

	_, _ = io.WriteString(w, "installed : "+installed+"\n")
	_, _ = io.WriteString(w, "state     : "+state+"\n")
	if info.Running && info.PID > 0 {
		_, _ = io.WriteString(w, "pid       : "+itoa(info.PID)+"\n")
	}
	if info.LogTail != "" {
		_, _ = io.WriteString(w, "\n--- last 10 log lines ---\n")
		_, _ = io.WriteString(w, info.LogTail)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
