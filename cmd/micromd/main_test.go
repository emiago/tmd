package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		code       int
		text, path string
	}{
		{nil, 0, "Usage:", ""},
		{[]string{"--help"}, 0, "Ctrl+O", ""},
		{[]string{"-h"}, 0, "micromd <file>", ""},
		{[]string{"help"}, 0, "Ctrl+U/R", ""},
		{[]string{"--version"}, 0, "micromd dev\n", ""},
		{[]string{"notes.md"}, 0, "", "notes.md"},
		{[]string{"file with spaces.md"}, 0, "", "file with spaces.md"},
		{[]string{"--", "-notes.md"}, 0, "", "-notes.md"},
		{[]string{"--unknown"}, 2, "micromd --help", ""},
		{[]string{"one.md", "two.md"}, 2, "usage:", ""},
		{[]string{""}, 2, "usage:", ""},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, errOut bytes.Buffer
			opened := ""
			code := run(tc.args, &out, &errOut, func(path string) error { opened = path; return nil })
			if code != tc.code || opened != tc.path {
				t.Fatalf("exit=%d opened=%q; want %d %q", code, opened, tc.code, tc.path)
			}
			output, other := out.String(), errOut.String()
			if code != 0 {
				output, other = other, output
			}
			if !strings.Contains(output, tc.text) || other != "" {
				t.Fatalf("stdout=%q stderr=%q", out.String(), errOut.String())
			}
		})
	}
}

func TestOpenError(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"notes.md"}, &out, &errOut, func(string) error { return errors.New("file changed") })
	if code != 2 || out.Len() != 0 || errOut.String() != "micromd: file changed\n" {
		t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}
