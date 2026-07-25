package service

// NewForTest creates a linuxManager with injected paths for testing,
// bypassing os.UserHomeDir() so tests are hermetic.
func NewForTest(binPath, configPath, unitPath, logPath string) *linuxManager {
	return newForTest(binPath, configPath, unitPath, logPath)
}

// UnitFileContent exposes the unexported unitFileContent method for testing.
func (m *linuxManager) UnitFileContent() string { return m.unitFileContent() }

// TailFile exposes the unexported tailFile helper for testing.
func TailFile(path string, n int) string { return tailFile(path, n) }
