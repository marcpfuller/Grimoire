package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/marcpfuller/grimoire/internal/service"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage the Grimoire daemon as a background service",
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install and start the Grimoire daemon as a system service",
	RunE:  runServiceInstall,
}

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop and remove the Grimoire daemon service",
	RunE:  runServiceUninstall,
}

var serviceRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the Grimoire daemon service",
	RunE:  runServiceRestart,
}

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the status of the Grimoire daemon service",
	RunE:  runServiceStatus,
}

func init() {
	serviceCmd.AddCommand(serviceInstallCmd, serviceUninstallCmd, serviceRestartCmd, serviceStatusCmd)
}

func serviceManager() (service.Manager, error) {
	bin, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve binary path: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	configDest := filepath.Join(home, ".config", "grimoire", "config.yaml")
	return service.New(bin, configDest)
}

func runServiceInstall(_ *cobra.Command, _ []string) error {
	mgr, err := serviceManager()
	if err != nil {
		return err
	}
	if err := mgr.Install(); err != nil {
		return err
	}
	fmt.Println("Grimoire daemon installed and started.")
	return nil
}

func runServiceUninstall(_ *cobra.Command, _ []string) error {
	mgr, err := serviceManager()
	if err != nil {
		return err
	}
	if err := mgr.Uninstall(); err != nil {
		return err
	}
	fmt.Println("Grimoire daemon stopped and removed.")
	return nil
}

func runServiceRestart(_ *cobra.Command, _ []string) error {
	mgr, err := serviceManager()
	if err != nil {
		return err
	}
	return mgr.Restart()
}

func runServiceStatus(_ *cobra.Command, _ []string) error {
	mgr, err := serviceManager()
	if err != nil {
		return err
	}
	info, err := mgr.Status()
	if err != nil {
		return err
	}
	service.PrintStatus(os.Stdout, info)
	return nil
}
