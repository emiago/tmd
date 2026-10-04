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
		{"```json\n{\"text\": \"**literal**\"}\n```\n", "{\"text\": \"**literal**\"}\n"},
		{"before\n```bash\n  echo '**literal**'\n\t# comment\n```\nafter", "before\n  echo '**literal**'\n\t# comment\nafter"},
		{"~~~bash\r\necho hello\r\n~~~~\r\n", "echo hello\r\n"},
		{"````md\n```\n**literal**\n````", "```\n**literal**\n"},
		{"```bash\necho hello", "echo hello"},
		{"```\n```\n**after**", "after"},
		{"```", ""},
		{"```bash\n~~~\n``` not a closing fence\n```", "~~~\n``` not a closing fence\n"},
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

func TestRenderFencedCodeUsesUniformStyle(t *testing.T) {
	for _, language := range []string{"", "bash", "json"} {
		var out bytes.Buffer
		source := "```" + language + "\n**literal** # text\n```"
		if err := Render(&out, source, true); err != nil {
			t.Fatal(err)
		}
		if want := "\x1b[36;48;2;22;22;22m**literal** # text\n\x1b[0m"; out.String() != want {
			t.Fatalf("language %q: got %q; want %q", language, out.String(), want)
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

func TestRenderCodeBackgroundStopsAfterBlock(t *testing.T) {
	var out bytes.Buffer
	if err := Render(&out, "```bash\necho hi\n```\nafter `inline`", true); err != nil {
		t.Fatal(err)
	}
	want := "\x1b[36;48;2;22;22;22mecho hi\n\x1b[0mafter \x1b[36minline\x1b[0m"
	if out.String() != want {
		t.Fatalf("output=%q; want %q", out.String(), want)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }
