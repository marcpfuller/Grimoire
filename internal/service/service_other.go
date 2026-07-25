//go:build !linux && !darwin

package service

import "fmt"

// New returns an error on unsupported platforms.
func New(_, _ string) (Manager, error) {
	return nil, fmt.Errorf("service: platform not supported; install/manage the daemon manually")
}
