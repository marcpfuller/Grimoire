package db_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/marcpfuller/grimoire/internal/db"
)

func TestDefaultCommandMetadata(t *testing.T) {
	cleanExit := 0
	failedExit := 1
	tests := []struct {
		name            string
		command         string
		exitCode        *int
		wantTool        string
		wantDescription string
	}{
		{
			name:            "clean exit",
			command:         "cat /etc/hosts",
			exitCode:        &cleanExit,
			wantTool:        "cat",
			wantDescription: "Command Executed",
		},
		{
			name:            "failed exit",
			command:         "nmap -sV target",
			exitCode:        &failedExit,
			wantTool:        "nmap",
			wantDescription: "Command Failed",
		},
		{
			name:     "still running",
			command:  "whoami",
			wantTool: "whoami",
		},
		{
			name:            "blank command",
			exitCode:        &cleanExit,
			wantDescription: "Command Executed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := db.DefaultCommandMetadata(&db.Command{
				ID:       42,
				Command:  tt.command,
				ExitCode: tt.exitCode,
			})

			assert.Equal(t, int64(42), metadata.CommandID)
			assert.Equal(t, tt.wantTool, metadata.Tool)
			assert.Equal(t, tt.wantDescription, metadata.Description)
		})
	}
}
