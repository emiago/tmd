package editor

import (
	"strings"
	"testing"
)

func TestDocumentPreservesSourceAndLineEndings(t *testing.T) {
	original := "# Notes\r\n- [ ] **First**\r\nuntouched\nlast"
	d := newDocument([]byte(original))
	d.cursor = strings.Index(d.text, "First") + len("First")
	d.insert(" task\nsecond")
	want := strings.Replace(original, "First", "First task\r\nsecond", 1)
	if d.text != want {
		t.Fatalf("edited source: %q; want %q", d.text, want)
	}
	d.undoEdit()
	if d.text != original {
		t.Fatalf("undo changed unrelated bytes: %q", d.text)
	}
	d.redoEdit()
	if d.text != want {
		t.Fatalf("redo: %q", d.text)
	}
}

func TestGraphemeMovementAndDeletion(t *testing.T) {
	d := newDocument([]byte("a界e\u0301👩‍💻\r\nb"))
	d.cursor = len("a界e\u0301👩‍💻")
	d.backspace()
	if d.text != "a界e\u0301\r\nb" {
		t.Fatalf("split emoji: %q", d.text)
	}
	d.backspace()
	if d.text != "a界\r\nb" {
		t.Fatalf("split combining cluster: %q", d.text)
	}
	d.cursor = next(d.text, d.cursor)
	d.backspace()
	if d.text != "a界b" {
		t.Fatalf("split CRLF: %q", d.text)
	}
	d.undoEdit()
	d.cursor = 1
	d.deleteForward()
	if d.text != "a\r\nb" {
		t.Fatalf("split wide rune: %q", d.text)
	}
}

func TestSearchAndReplacement(t *testing.T) {
	s := "**needle** and needle, NEEDLE"
	for _, tc := range []struct {
		query       string
		from, start int
	}{
		{"needle", 0, 2}, {"needle", 3, 15}, {"needle", 20, 2}, {"NEEDLE", 0, 23}, {"absent", 0, -1}, {"", 0, -1},
	} {
		start, _ := find(s, tc.query, tc.from)
		if start != tc.start {
			t.Errorf("find %q from %d = %d; want %d", tc.query, tc.from, start, tc.start)
		}
	}
	d := newDocument([]byte("cat cat"))
	if n := d.replaceAll("cat", "catcat", 0); n != 2 || d.text != "catcat catcat" {
		t.Fatalf("replace-all: %d %q", n, d.text)
	}
	d.undoEdit()
	if d.text != "cat cat" {
		t.Fatalf("replace-all undo: %q", d.text)
	}
	d.cursor = 3
	d.insert("!")
	d.redoEdit()
	if d.text != "cat! cat" {
		t.Fatalf("new edit did not discard redo: %q", d.text)
	}
}
