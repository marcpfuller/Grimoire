//go:build darwin

package service

// NewForTest creates a darwinManager with injected paths for testing,
// bypassing os.UserHomeDir() so tests are hermetic.
func NewForTest(binPath, configPath, plistPath, logPath string) *darwinManager {
	return newForTest(binPath, configPath, plistPath, logPath)
}

// PlistContent exposes the unexported plistContent method for testing.
func (m *darwinManager) PlistContent() string { return m.plistContent() }

// TailFile exposes the unexported tailFile helper for testing.
func TailFile(path string, n int) string { return tailFile(path, n) }
