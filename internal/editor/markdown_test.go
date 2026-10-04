package editor

import (
	"strings"
	"testing"
)

func rowText(row displayRow) string {
	var s strings.Builder
	for _, c := range row.cells {
		s.WriteString(c.text)
	}
	return s.String()
}

func TestMarkdownProjectionAndActiveSource(t *testing.T) {
	source := "# Heading\n- [ ] **bold** and *italic* and `code`\n- [X] ***both***\n\n```md\n- [ ] **literal**\n```\n[link](https://example.com)\nactive"
	d := newDocument([]byte(source))
	d.cursor = len(source)
	p := project(source)
	l := makeLayout(d, p, 100)
	want := []string{"Heading", "• ☐ bold and italic and code", "• ☑ both", "", "", "- [ ] **literal**", "", "[link](https://example.com)", "active"}
	if len(l.rows) != len(want) {
		t.Fatalf("got %d rows; want %d", len(l.rows), len(want))
	}
	for i, row := range l.rows {
		if got := rowText(row); got != want[i] {
			t.Errorf("row %d: %q; want %q", i, got, want[i])
		}
	}
	if p.formats[strings.Index(source, "bold")]&bold == 0 || p.formats[strings.Index(source, "italic")]&italic == 0 || p.formats[strings.Index(source, "both")]&(bold|italic) != (bold|italic) {
		t.Fatal("lost inline styles")
	}
	d.cursor = strings.Index(source, "bold")
	l = makeLayout(d, p, 100)
	if got := rowText(l.rows[1]); got != "- [ ] **bold** and *italic* and `code`" {
		t.Fatalf("active line did not expose source: %q", got)
	}
	if l.cursorRow != 1 || l.cursorX != 8 {
		t.Fatalf("cursor source mapping: %d:%d", l.cursorRow, l.cursorX)
	}
	if d.text != source {
		t.Fatal("rendering mutated source")
	}
}

func TestWrappingWideTextAndCursor(t *testing.T) {
	d := newDocument([]byte("界e\u0301abcd\nlast"))
	d.cursor = len("界e\u0301ab")
	l := makeLayout(d, project(d.text), 4)
	if rowText(l.rows[0]) != "界e\u0301a" || rowText(l.rows[1]) != "bcd" || l.cursorRow != 1 || l.cursorX != 1 {
		t.Fatalf("wrapped layout: %#v", l)
	}
	for _, width := range []int{0, 1, 2, 4, 20} {
		d.cursor = len("界e\u0301abcd")
		l = makeLayout(d, project(d.text), width)
		if l.cursorX >= max(1, width) || l.cursorRow >= len(l.rows) {
			t.Fatalf("cursor outside width %d: %#v", width, l)
		}
	}
}

func TestFencedCodeSourceAndCursor(t *testing.T) {
	source := "before\n```bash\n\techo **literal**\n```\nafter"
	d := newDocument([]byte(source))
	p := project(source)
	for _, active := range []int{0, 1, 2, 3, 4} {
		d.cursor = p.lines[active].start
		l := makeLayout(d, p, 80)
		want := []string{"before", "", "    echo **literal**", "", "after"}
		if active == 1 {
			want[1] = "```bash"
		}
		if active == 3 {
			want[3] = "```"
		}
		for i, row := range l.rows {
			if got := rowText(row); got != want[i] {
				t.Fatalf("active line %d, row %d: %q; want %q", active, i, got, want[i])
			}
		}
		if l.cursorRow != active || l.cursorX != 0 {
			t.Fatalf("active line %d: cursor %d:%d", active, l.cursorRow, l.cursorX)
		}
	}
	for _, f := range p.formats[strings.Index(source, "echo"):strings.Index(source, "\n```\nafter")] {
		if f != code|codeBlock {
			t.Fatalf("code contents have format %d; want %d", f, code|codeBlock)
		}
	}
	// Editing a fence reprojects the source, and undo restores the block.
	d.edit(p.lines[1].start, p.lines[1].end, "plain")
	d.cursor = 0
	l := makeLayout(d, project(d.text), 80)
	if rowText(l.rows[1]) != "plain" || rowText(l.rows[2]) != "    echo literal" {
		t.Fatal("removing the opening fence did not reproject the body")
	}
	d.undoEdit()
	if d.text != source {
		t.Fatal("undo did not restore fenced source")
	}
}

func TestMarkdownEdgeCasesRemainEditable(t *testing.T) {
	for _, source := range []string{"#", "#\n", "## \r\n", "Heading\n=======\n", "***", "- [ ]", "- [x]\n", "` `", "` a `", "__bold *nested*__", "a\\*literal*", "\t\n", "> # quote\n", "- # item\n", "**line\nbreak**", "<div>**literal**</div>", "```", "```\n```", "~~~bash\r\n\t界\r\n~~~", "> ```bash\n> echo hi\n> ```", "- ```\n  code\n  ```"} {
		t.Run(source, func(t *testing.T) {
			d := newDocument([]byte(source))
			p := project(source)
			for i := 0; i <= len(source); i = next(source, i) {
				d.cursor = i
				for _, w := range []int{1, 3, 80} {
					_ = makeLayout(d, p, w)
				}
				if i == len(source) {
					break
				}
			}
			if d.text != source {
				t.Fatal("source changed")
			}
		})
	}
}

func TestCursorAroundExpandedTabs(t *testing.T) {
	d := newDocument([]byte("a\tb"))
	p := project(d.text)
	d.cursor = 1
	l := makeLayout(d, p, 80)
	if l.cursorX != 1 {
		t.Fatalf("cursor before tab: %d", l.cursorX)
	}
	d.cursor = 2
	l = makeLayout(d, p, 80)
	if l.cursorX != 4 {
		t.Fatalf("cursor after tab: %d", l.cursorX)
	}
	d.cursor = 1
	l = makeLayout(d, p, 2)
	if l.cursorX != 1 || l.cursorRow != 0 {
		t.Fatalf("cursor before wrapped tab: %d:%d", l.cursorRow, l.cursorX)
	}
}

func FuzzMarkdownLayout(f *testing.F) {
	for _, s := range []string{"# Heading\n- [ ] **bold**\n", "#\r\n", "👩‍💻\ttext", "**nested *word***", "```bash\n\techo **literal**\n```", "~~~\r\n~~~"} {
		f.Add(s, uint8(20))
	}
	f.Fuzz(func(t *testing.T, s string, w uint8) {
		if len(s) > 10000 {
			t.Skip()
		}
		d := newDocument([]byte(s))
		d.cursor = len(s)
		_ = makeLayout(d, project(s), int(w))
	})
}
