package service

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

const (
	launchdLabel   = "com.grimoire.daemon"
	launchdPlistName = launchdLabel + ".plist"
)

// darwinManager implements Manager using launchd LaunchAgents.
type darwinManager struct {
	binPath    string // path to the grim binary
	configPath string // canonical config: ~/.config/grimoire/config.yaml
	plistPath  string // ~/Library/LaunchAgents/com.grimoire.daemon.plist
	logPath    string // ~/.config/grimoire/grimoire.log
}

// New returns a Manager for macOS launchd.
//
// binPath and configPath are typically populated by the caller from
// os.Executable() and the resolved service config destination respectively.
func New(binPath, configPath string) (Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("service: resolve home dir: %w", err)
	}
	return &darwinManager{
		binPath:    binPath,
		configPath: configPath,
		plistPath:  filepath.Join(home, "Library", "LaunchAgents", launchdPlistName),
		logPath:    filepath.Join(home, ".config", "grimoire", "grimoire.log"),
	}, nil
}

// newForTest is a constructor for tests that overrides the plist and log paths.
func newForTest(binPath, configPath, plistPath, logPath string) *darwinManager {
	return &darwinManager{
		binPath:    binPath,
		configPath: configPath,
		plistPath:  plistPath,
		logPath:    logPath,
	}
}

func (m *darwinManager) Install() error {
	if _, err := os.Stat(m.plistPath); err == nil {
		return fmt.Errorf("service: already installed at %s; run 'grim service uninstall' first", m.plistPath)
	}

	// Write canonical config from current Viper settings.
	if err := os.MkdirAll(filepath.Dir(m.configPath), 0o700); err != nil {
		return fmt.Errorf("service: create config dir: %w", err)
	}
	if err := viper.WriteConfigAs(m.configPath); err != nil {
		return fmt.Errorf("service: write config: %w", err)
	}

	// Write launchd plist.
	if err := os.MkdirAll(filepath.Dir(m.plistPath), 0o750); err != nil {
		return fmt.Errorf("service: create LaunchAgents dir: %w", err)
	}
	content := m.plistContent()
	if err := os.WriteFile(m.plistPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("service: write plist: %w", err)
	}

	// Load (starts immediately due to RunAtLoad=true).
	if err := run("launchctl", "load", m.plistPath); err != nil {
		return fmt.Errorf("service: launchctl load: %w", err)
	}
	return nil
}

// InstallUnitOnly writes the plist file without invoking launchctl.
// Used in tests to verify unit file content without OS subprocess calls.
func (m *darwinManager) InstallUnitOnly() error {
	if _, err := os.Stat(m.plistPath); err == nil {
		return fmt.Errorf("service: already installed at %s; run 'grim service uninstall' first", m.plistPath)
	}
	if err := os.MkdirAll(filepath.Dir(m.plistPath), 0o750); err != nil {
		return fmt.Errorf("service: create dir: %w", err)
	}
	return os.WriteFile(m.plistPath, []byte(m.plistContent()), 0o600)
}

func (m *darwinManager) Uninstall() error {
	// Unload (stops + deregisters). Ignore "not found" — already stopped is fine.
	if err := run("launchctl", "unload", m.plistPath); err != nil {
		if !strings.Contains(err.Error(), "Could not find specified service") {
			return fmt.Errorf("service: launchctl unload: %w", err)
		}
	}

	if err := os.Remove(m.plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove plist: %w", err)
	}
	return nil
}

func (m *darwinManager) Stop() error {
	return run("launchctl", "unload", m.plistPath)
}

func (m *darwinManager) Restart() error {
	fmt.Println("restarting — shell hooks will silently drop commands for ~2s")
	_ = run("launchctl", "unload", m.plistPath)
	return run("launchctl", "load", m.plistPath)
}

func (m *darwinManager) Status() (StatusInfo, error) {
	info := StatusInfo{}

	_, err := os.Stat(m.plistPath)
	info.Installed = err == nil

	// Query launchctl for running state and PID.
	out, err := output("launchctl", "print", fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel))
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "pid = ") {
				pid, _ := strconv.Atoi(strings.TrimPrefix(line, "pid = "))
				if pid > 0 {
					info.Running = true
					info.PID = pid
				}
			}
		}
	}

	info.LogTail = tailFile(m.logPath, 10)
	return info, nil
}

// plistContent renders the launchd plist XML for this manager's configuration.
func (m *darwinManager) plistContent() string {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString("<dict>\n")
	b.WriteString("\t<key>Label</key>\n\t<string>" + launchdLabel + "</string>\n")
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	b.WriteString("\t\t<string>" + m.binPath + "</string>\n")
	b.WriteString("\t\t<string>--config</string>\n")
	b.WriteString("\t\t<string>" + m.configPath + "</string>\n")
	b.WriteString("\t\t<string>daemon</string>\n")
	b.WriteString("\t\t<string>start</string>\n")
	b.WriteString("\t</array>\n")
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")
	b.WriteString("\t<key>StandardOutPath</key>\n\t<string>" + m.logPath + "</string>\n")
	b.WriteString("\t<key>StandardErrorPath</key>\n\t<string>" + m.logPath + "</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}


