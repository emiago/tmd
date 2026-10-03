package editor

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"# Heading\n- [x] **done** and *more*", "Heading\n• ☑ done and more"},
		{"```json\n{\"text\": \"**literal**\"}\n```\n", "```json\n{\"text\": \"**literal**\"}\n```\n"},
		{"plain\r\n\ttext\r\n", "plain\r\n\ttext\r\n"},
		{"界 **café**\n", "界 café\n"},
		{"\x1b[31mtext", "�[31mtext"},
		{"", ""},
	} {
		var out bytes.Buffer
		if err := Render(&out, tc.source, false); err != nil {
			t.Fatal(err)
		}
		if out.String() != tc.want {
			t.Errorf("Render(%q) = %q; want %q", tc.source, out.String(), tc.want)
		}
	}
}

func TestRenderStylesAndErrors(t *testing.T) {
	var out bytes.Buffer
	if err := Render(&out, "**bold**", true); err != nil || out.String() != "\x1b[1mbold\x1b[0m" {
		t.Fatalf("styled output=%q err=%v", out.String(), err)
	}
	for _, input := range []string{"\xff", "a\x00b"} {
		out.Reset()
		if err := Render(&out, input, false); err == nil || out.Len() != 0 {
			t.Fatalf("invalid input %q: output=%q err=%v", input, out.String(), err)
		}
	}
	if err := Render(shortWriter{}, strings.Repeat("text", 2), false); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write: %v", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }
