// Package editor implements the built-in Markdown terminal editor.
package editor

import (
	"bytes"
	"strings"

	"github.com/rivo/uniseg"
)

type change struct {
	start                     int
	before, after             string
	cursorBefore, cursorAfter int
}

// document keeps Markdown source authoritative. All positions are UTF-8 byte
// offsets; movement and deletion operate on whole grapheme clusters.
type document struct {
	text       string
	cursor     int
	newline    string
	undo, redo []change
	revision   uint64
}

func newDocument(data []byte) *document {
	crlf := bytes.Count(data, []byte("\r\n"))
	eol := "\n"
	if crlf > bytes.Count(data, []byte("\n"))-crlf {
		eol = "\r\n"
	}
	return &document{text: string(data), newline: eol}
}

func (d *document) edit(start, end int, text string) {
	if d.text[start:end] == text {
		return
	}
	c := change{start: start, before: d.text[start:end], after: text, cursorBefore: d.cursor, cursorAfter: start + len(text)}
	d.apply(c, false)
	d.undo = append(d.undo, c)
	// Bound retained edits, rather than saving a full document per keystroke.
	if len(d.undo) > 1000 {
		d.undo = d.undo[len(d.undo)-1000:]
	}
	d.redo = nil
}

func (d *document) apply(c change, reverse bool) {
	before, after, cursor := c.before, c.after, c.cursorAfter
	if reverse {
		before, after, cursor = after, before, c.cursorBefore
	}
	d.text = d.text[:c.start] + after + d.text[c.start+len(before):]
	d.cursor = cursor
	d.snapCursor()
	d.revision++
}

func (d *document) undoEdit() {
	if len(d.undo) == 0 {
		return
	}
	c := d.undo[len(d.undo)-1]
	d.undo = d.undo[:len(d.undo)-1]
	d.apply(c, true)
	d.redo = append(d.redo, c)
}

func (d *document) redoEdit() {
	if len(d.redo) == 0 {
		return
	}
	c := d.redo[len(d.redo)-1]
	d.redo = d.redo[:len(d.redo)-1]
	d.apply(c, false)
	d.undo = append(d.undo, c)
}

func (d *document) insert(text string) {
	// Terminal paste must not create a binary file that cannot be reopened.
	text = strings.ReplaceAll(text, "\x00", "�")
	// Only inserted line breaks are normalized; existing bytes stay untouched.
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	text = strings.ReplaceAll(text, "\n", d.newline)
	d.edit(d.cursor, d.cursor, text)
}

func previous(s string, pos int) int {
	start := strings.LastIndexByte(s[:pos], '\n') + 1
	if start == pos && pos > 0 {
		if pos > 1 && s[pos-2:pos] == "\r\n" {
			return pos - 2
		}
		return pos - 1
	}
	g := uniseg.NewGraphemes(s[start:pos])
	last := start
	for g.Next() {
		a, _ := g.Positions()
		last = start + a
	}
	return last
}

func next(s string, pos int) int {
	if pos >= len(s) {
		return len(s)
	}
	g := uniseg.NewGraphemes(s[pos:])
	g.Next()
	_, end := g.Positions()
	return pos + end
}

func (d *document) snapCursor() {
	d.cursor = snapPosition(d.text, d.cursor)
}

func snapPosition(s string, pos int) int {
	pos = max(0, min(len(s), pos))
	start := strings.LastIndexByte(s[:pos], '\n') + 1
	g := uniseg.NewGraphemes(s[start:])
	for g.Next() {
		a, b := g.Positions()
		if start+b >= pos {
			if start+a < pos && pos < start+b {
				return start + b
			}
			return pos
		}
	}
	return pos
}

func (d *document) backspace() {
	if d.cursor > 0 {
		d.edit(previous(d.text, d.cursor), d.cursor, "")
	}
}

func (d *document) deleteForward() {
	if d.cursor < len(d.text) {
		d.edit(d.cursor, next(d.text, d.cursor), "")
	}
}

type sourceLine struct{ start, end, after int }

func sourceLines(s string) []sourceLine {
	var lines []sourceLine
	start := 0
	for {
		i := strings.IndexByte(s[start:], '\n')
		if i < 0 {
			lines = append(lines, sourceLine{start, len(s), len(s)})
			break
		}
		end := start + i
		if end > start && s[end-1] == '\r' {
			end--
		}
		lines = append(lines, sourceLine{start, end, start + i + 1})
		start += i + 1
	}
	return lines
}

func lineAt(lines []sourceLine, pos int) int {
	// A cursor on a newline belongs to the line before it.
	low, high := 0, len(lines)
	for low < high {
		mid := (low + high) / 2
		if lines[mid].start <= pos {
			low = mid + 1
		} else {
			high = mid
		}
	}
	return max(0, low-1)
}

// find searches source, wrapping once, and returns a byte range.
func find(s, query string, from int) (int, int) {
	if query == "" {
		return -1, -1
	}
	from = max(0, min(len(s), from))
	if i := strings.Index(s[from:], query); i >= 0 {
		return from + i, from + i + len(query)
	}
	if i := strings.Index(s, query); i >= 0 {
		return i, i + len(query)
	}
	return -1, -1
}

func (d *document) replaceAll(query, replacement string, from int) int {
	if query == "" {
		return 0
	}
	tail := d.text[from:]
	count := strings.Count(tail, query)
	if count != 0 {
		d.edit(from, len(d.text), strings.ReplaceAll(tail, query, replacement))
	}
	return count
}
