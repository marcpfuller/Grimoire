//go:build darwin

package service_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marcpfuller/grimoire/internal/service"
)

func TestPlistContent(t *testing.T) {
	m := service.NewForTest(
		"/usr/local/bin/grim",
		"/Users/op/.config/grimoire/config.yaml",
		"/tmp/com.grimoire.daemon.plist",
		"/Users/op/.config/grimoire/grimoire.log",
	)
	got := m.PlistContent()

	assert.Contains(t, got, `<string>com.grimoire.daemon</string>`, "label")
	assert.Contains(t, got, `<string>/usr/local/bin/grim</string>`, "binary path")
	assert.Contains(t, got, `<string>--config</string>`, "--config flag")
	assert.Contains(t, got, `<string>/Users/op/.config/grimoire/config.yaml</string>`, "config path")
	assert.Contains(t, got, `<string>daemon</string>`, "daemon subcommand")
	assert.Contains(t, got, `<string>start</string>`, "start subcommand")
	assert.Contains(t, got, `<true/>`, "RunAtLoad and KeepAlive")
	assert.Contains(t, got, `<string>/Users/op/.config/grimoire/grimoire.log</string>`, "log path")
	assert.True(t, strings.HasPrefix(got, `<?xml`), "must start with XML declaration")
}

func TestInstallErrorIfAlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.grimoire.daemon.plist")

	// Pre-create the plist to simulate existing installation.
	require.NoError(t, os.WriteFile(plistPath, []byte("existing"), 0o600))

	m := service.NewForTest("/bin/grim", "/tmp/config.yaml", plistPath, "/tmp/grimoire.log")
	err := m.InstallUnitOnly()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already installed")
	assert.Contains(t, err.Error(), "uninstall")
}

func TestInstallWritesPlist(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.grimoire.daemon.plist")

	m := service.NewForTest("/bin/grim", "/tmp/config.yaml", plistPath, "/tmp/grimoire.log")
	require.NoError(t, m.InstallUnitOnly())

	content, err := os.ReadFile(plistPath) //nolint:gosec // G304: plistPath is from t.TempDir(), not user input
	require.NoError(t, err)
	assert.Contains(t, string(content), "com.grimoire.daemon")
	assert.Contains(t, string(content), "/bin/grim")
}

func TestTailFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	lines := make([]string, 20)
	for i := range lines {
		lines[i] = strings.Repeat("x", 10)
	}
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))

	result := service.TailFile(path, 10)
	resultLines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	assert.Len(t, resultLines, 10, "should return exactly 10 lines")
}

func TestTailFileEmpty(t *testing.T) {
	result := service.TailFile("/nonexistent/path/grimoire.log", 10)
	assert.Empty(t, result, "missing file should return empty string")
}

func TestPrintStatus(t *testing.T) {
	var buf strings.Builder
	service.PrintStatus(&buf, service.StatusInfo{
		Installed: true,
		Running:   true,
		PID:       12345,
		LogTail:   "line1\nline2\n",
	})
	out := buf.String()
	assert.Contains(t, out, "installed : yes")
	assert.Contains(t, out, "state     : running")
	assert.Contains(t, out, "12345")
	assert.Contains(t, out, "line1")
}
