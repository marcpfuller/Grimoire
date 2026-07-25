package service

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// run executes a command and returns a combined-output error on non-zero exit.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...) //nolint:gosec // args are internal constants
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, bytes.TrimSpace(out))
	}
	return nil
}

// output runs a command and returns its stdout as a string.
func output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...) //nolint:gosec // args are internal constants
	out, err := cmd.Output()
	return string(out), err
}

// tailFile returns the last n lines of the file at path, or an empty string
// if the file cannot be read. Reads the entire file; suitable for log files
// up to a few hundred MB on operator hardware.
func tailFile(path string, n int) string {
	f, err := os.Open(path) //nolint:gosec // path is derived from config, not user input
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
