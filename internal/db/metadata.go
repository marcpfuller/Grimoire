package db

import "strings"

// DefaultCommandMetadata returns the metadata presented and synchronized when
// no operator-supplied row exists yet. Stored metadata always takes precedence,
// including explicitly empty fields.
func DefaultCommandMetadata(cmd *Command) *CommandMetadata {
	metadata := &CommandMetadata{CommandID: cmd.ID}
	for _, token := range strings.Fields(cmd.Command) {
		metadata.Tool = token
		break
	}

	if cmd.ExitCode == nil {
		return metadata
	}
	if *cmd.ExitCode == 0 {
		metadata.Description = "Command Executed"
	} else {
		metadata.Description = "Command Failed"
	}
	return metadata
}
