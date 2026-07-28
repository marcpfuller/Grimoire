package scrub_test

import (
	"strings"
	"testing"

	"github.com/marcpfuller/grimoire/internal/scrub"
	"github.com/stretchr/testify/assert"
)

func TestStrip(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{
			name: "empty input",
			in:   []byte{},
			want: "",
		},
		{
			name: "nil input",
			in:   nil,
			want: "",
		},
		{
			name: "plain text passthrough",
			in:   []byte("hello world"),
			want: "hello world",
		},
		{
			name: "SGR color preserved",
			in:   []byte("\x1b[32mhello\x1b[0m"),
			want: "\x1b[32mhello\x1b[0m",
		},
		{
			name: "SGR bold and color preserved",
			in:   []byte("\x1b[1;32mFOUND\x1b[0m: /some/path"),
			want: "\x1b[1;32mFOUND\x1b[0m: /some/path",
		},
		{
			name: "SGR true color preserved",
			in:   []byte("\x1b[38;2;255;100;50mtext\x1b[0m"),
			want: "\x1b[38;2;255;100;50mtext\x1b[0m",
		},
		{
			name: "cursor up stripped",
			in:   []byte("before\x1b[1Aafter"),
			want: "beforeafter",
		},
		{
			name: "cursor movement stripped",
			in:   []byte("\x1b[2A\x1b[5C\x1b[1;1H"),
			want: "",
		},
		{
			name: "clear screen stripped",
			in:   []byte("\x1b[2J\x1b[H"),
			want: "",
		},
		{
			name: "erase line stripped",
			in:   []byte("text\x1b[Kmore"),
			want: "textmore",
		},
		{
			name: "OSC window title stripped",
			in:   []byte("\x1b]0;My Terminal\x07text"),
			want: "text",
		},
		{
			name: "OSC terminated by ST stripped",
			in:   []byte("\x1b]0;title\x1b\\text"),
			want: "text",
		},
		{
			name: "charset designation stripped",
			in:   []byte("\x1b(Btext\x1b)0"),
			want: "text",
		},
		{
			name: "two-byte Fe escape (reverse index) stripped",
			in:   []byte("line1\x1bMline2"),
			want: "line1line2",
		},
		{
			name: "application keypad mode stripped",
			in:   []byte("\x1b=text\x1b>"),
			want: "text",
		},
		{
			name: "DCS string stripped",
			in:   []byte("\x1bPq#0;2;0;0;0!\x1b\\text"),
			want: "text",
		},
		{
			name: "CRLF normalised to LF",
			in:   []byte("line1\r\nline2\r\nline3"),
			want: "line1\nline2\nline3",
		},
		{
			name: "bare CR preserved for overwrite pattern",
			in:   []byte("loading...\rloading... done"),
			want: "loading...\rloading... done",
		},
		{
			name: "bare CR at end preserved",
			in:   []byte("text\r"),
			want: "text\r",
		},
		{
			name: "8-bit CSI SGR preserved",
			in:   []byte{0x9b, '3', '2', 'm', 'h', 'i', 0x9b, '0', 'm'},
			want: "\x9b32mhi\x9b0m",
		},
		{
			name: "8-bit CSI cursor movement stripped",
			in:   []byte{0x9b, '1', 'A', 'h', 'i'},
			want: "hi",
		},
		{
			name: "8-bit OSC stripped",
			in:   []byte{0x9d, 0x30, 0x3b, 't', 'i', 't', 'l', 'e', 0x07, 'x'},
			want: "x",
		},
		{
			name: "other C1 controls stripped",
			in:   []byte{0x80, 0x90, 0x9e, 'a'},
			want: "a",
		},
		{
			name: "truncated CSI at end-of-input discarded",
			in:   []byte("text\x1b[32"),
			want: "text",
		},
		{
			name: "truncated ESC at end-of-input discarded",
			in:   []byte("text\x1b"),
			want: "text",
		},
		{
			name: "mixed: color output with movement and CRLF",
			in:   []byte("\x1b[1;32mFOUND\x1b[0m: /etc/passwd\r\n\x1b[2K\x1b[31merror\x1b[0m: denied\r\n"),
			want: "\x1b[1;32mFOUND\x1b[0m: /etc/passwd\n\x1b[31merror\x1b[0m: denied\n",
		},
		{
			name: "progress bar with SGR",
			in:   []byte("\x1b[32m[##  ]\x1b[0m 50%\r\x1b[32m[####]\x1b[0m 100%"),
			want: "\x1b[32m[##  ]\x1b[0m 50%\r\x1b[32m[####]\x1b[0m 100%",
		},
		{
			name: "nul bytes passed through",
			in:   []byte("a\x00b"),
			want: "a\x00b",
		},
		{
			name: "large input with many sequences",
			in:   []byte(strings.Repeat("\x1b[32mok\x1b[0m\x1b[1A", 1000)),
			want: strings.Repeat("\x1b[32mok\x1b[0m", 1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scrub.Strip(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}
