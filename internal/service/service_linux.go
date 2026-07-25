//go:build linux

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

const systemdServiceName = "grimoire"

// linuxManager implements Manager using a systemd user service.
type linuxManager struct {
	binPath    string // path to the grim binary
	configPath string // canonical config: ~/.config/grimoire/config.yaml
	unitPath   string // ~/.config/systemd/user/grimoire.service
	logPath    string // ~/.config/grimoire/grimoire.log
}

// New returns a Manager for Linux systemd.
func New(binPath, configPath string) (Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("service: resolve home dir: %w", err)
	}
	return &linuxManager{
		binPath:    binPath,
		configPath: configPath,
		unitPath:   filepath.Join(home, ".config", "systemd", "user", systemdServiceName+".service"),
		logPath:    filepath.Join(home, ".config", "grimoire", "grimoire.log"),
	}, nil
}

// newForTest is a constructor for tests that overrides paths.
func newForTest(binPath, configPath, unitPath, logPath string) *linuxManager {
	return &linuxManager{
		binPath:    binPath,
		configPath: configPath,
		unitPath:   unitPath,
		logPath:    logPath,
	}
}

func (m *linuxManager) Install() error {
	if _, err := os.Stat(m.unitPath); err == nil {
		return fmt.Errorf("service: already installed at %s; run 'grim service uninstall' first", m.unitPath)
	}

	if err := os.MkdirAll(filepath.Dir(m.configPath), 0o700); err != nil {
		return fmt.Errorf("service: create config dir: %w", err)
	}
	if err := viper.WriteConfigAs(m.configPath); err != nil {
		return fmt.Errorf("service: write config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(m.unitPath), 0o750); err != nil {
		return fmt.Errorf("service: create systemd user dir: %w", err)
	}
	if err := os.WriteFile(m.unitPath, []byte(m.unitFileContent()), 0o600); err != nil {
		return fmt.Errorf("service: write unit file: %w", err)
	}

	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("service: daemon-reload: %w", err)
	}
	if err := run("systemctl", "--user", "enable", "--now", systemdServiceName); err != nil {
		return fmt.Errorf("service: enable: %w", err)
	}
	return nil
}

func (m *linuxManager) Uninstall() error {
	_ = m.Stop()
	_ = run("systemctl", "--user", "disable", systemdServiceName)
	if err := os.Remove(m.unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove unit file: %w", err)
	}
	_ = run("systemctl", "--user", "daemon-reload")
	return nil
}

func (m *linuxManager) Stop() error {
	return run("systemctl", "--user", "stop", systemdServiceName)
}

func (m *linuxManager) Restart() error {
	fmt.Println("restarting — shell hooks will silently drop commands for ~2s")
	return run("systemctl", "--user", "restart", systemdServiceName)
}

func (m *linuxManager) Status() (StatusInfo, error) {
	info := StatusInfo{}

	_, err := os.Stat(m.unitPath)
	info.Installed = err == nil

	out, err := output("systemctl", "--user", "show", systemdServiceName,
		"--property=ActiveState,MainPID")
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			switch {
			case line == "ActiveState=active":
				info.Running = true
			case strings.HasPrefix(line, "MainPID="):
				pid, _ := strconv.Atoi(strings.TrimPrefix(line, "MainPID="))
				info.PID = pid
			}
		}
	}

	info.LogTail = tailFile(m.logPath, 10)
	return info, nil
}

// unitFileContent renders the systemd unit file for this manager's configuration.
func (m *linuxManager) unitFileContent() string {
	var b bytes.Buffer
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Grimoire shell history daemon\n")
	b.WriteString("After=network.target\n\n")
	b.WriteString("[Service]\n")
	// Quote paths to handle spaces in home directories.
	b.WriteString("ExecStart=" + m.binPath + " --config \"" + m.configPath + "\" daemon start\n")
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=5\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String()
}
