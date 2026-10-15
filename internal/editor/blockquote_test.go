package editor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestBlockquoteProjection(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
		quoted             []int
	}{
		{"single line", "> **quote**", "   quote", []int{0}},
		{"empty quote", ">", "  ", []int{0}},
		{"final newline", "> quote\n", "   quote\n", []int{0}},
		{"blank quoted line", "> first\n>\n> second\n\nafter", "   first\n  \n   second\n\nafter", []int{0, 1, 2}},
		{"lazy continuation", "> first\nsecond\n\nafter", "   first\nsecond\n\nafter", []int{0, 1}},
		{"nested quotes", "> first\n> > **nested**\n> last\n\nafter", "   first\n      nested\n   last\n\nafter", []int{0, 1, 2}},
		{"heading and task", "> # Heading\n> - [x] **done**\n\nafter", "   Heading\n   • ☑ done\n\nafter", []int{0, 1}},
		{"setext heading", "> Heading\n> =======\n\nafter", "   Heading\n   =======\n\nafter", []int{0, 1}},
		{"fenced code", "> ```md\n> **literal**\n> ```\n\nafter", "   **literal**\n\nafter", []int{0, 1, 2}},
		{"empty fenced code", "> ```\n> ```\n\nafter", "\nafter", []int{0, 1}},
		{"table", "> A | B\n> --- | ---\n> **x** | y\n\nafter", "   │ A │ B │\n   ├───┼───┤\n   │ x │ y │\n\nafter", []int{0, 1, 2}},
		{"HTML", "> <div>\n> **literal**\n> </div>\n\nafter", "   <div>\n   **literal**\n   </div>\n\nafter", []int{0, 1, 2}},
		{"list container", "- > quoted\n  > continued\n\nafter", "-    quoted\n     continued\n\nafter", []int{0, 1}},
		{"separate quotes", "> first\n\n> second\n", "   first\n\n   second\n", []int{0, 2}},
		{"interruption", "> quote\n# after", "   quote\nafter", []int{0}},
		{"CRLF", "  > **quote**\r\n  >\r\n\r\nafter", "     quote\r\n    \r\n\r\nafter", []int{0, 1}},
		{"literal markers in quote", "> `a > b` and a > b\n>     > code\n\nafter", "   a > b and a > b\n       > code\n\nafter", []int{0, 1}},
		{"lazy literal marker", "> first\ntext > literal\n\nafter", "   first\ntext > literal\n\nafter", []int{0, 1}},
		{"literal markers", "text > quote\n\\> escaped\n\n    > indented\n\n```\n> code\n```", "text > quote\n\\> escaped\n\n    > indented\n\n> code\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Render(&out, tc.source, false); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Fatalf("output=%q; want %q", out.String(), tc.want)
			}
			p := project(tc.source)
			want := make(map[int]bool)
			for _, line := range tc.quoted {
				want[line] = true
			}
			for index, line := range p.lines {
				if p.quoteLines[index] != want[index] {
					t.Errorf("line %d: quoted=%v; want %v", index, p.quoteLines[index], want[index])
				}
				for pos := line.start; pos < line.after; pos++ {
					if got := p.formats[pos]&blockquote != 0; got != want[index] {
						t.Fatalf("line %d, byte %d: quote style=%v; want %v", index, pos, got, want[index])
					}
				}
			}
		})
	}
}

func TestRenderBlockquoteBackground(t *testing.T) {
	var out bytes.Buffer
	source := "> **bold** and *italic* `code`\n>\n> > nested\n\nafter"
	if err := Render(&out, source, true); err != nil {
		t.Fatal(err)
	}
	want := "\x1b[48;2;24;28;36m   \x1b[0m\x1b[1;48;2;24;28;36mbold\x1b[0m\x1b[48;2;24;28;36m and \x1b[0m\x1b[3;48;2;24;28;36mitalic\x1b[0m\x1b[48;2;24;28;36m \x1b[0m\x1b[38;2;229;167;107;48;2;24;28;36mcode\x1b[0m\x1b[48;2;24;28;36m\n  \n      nested\n\x1b[0m\nafter"
	if out.String() != want {
		t.Fatalf("output=%q; want %q", out.String(), want)
	}
	// Table borders and padding need the quote style too, and literal code
	// retains its existing, darker code background inside a quote.
	for _, tc := range []struct{ source, want string }{
		{"> A | B\n> --- | ---\n> x | y\n\nafter", "\x1b[48;2;24;28;36m   │ \x1b[0m\x1b[1;48;2;24;28;36mA\x1b[0m\x1b[48;2;24;28;36m │ \x1b[0m\x1b[1;48;2;24;28;36mB\x1b[0m\x1b[48;2;24;28;36m │\n   ├───┼───┤\n   │ x │ y │\n\x1b[0m\nafter"},
		{"> ```\n> **literal**\n> ```\n\nafter", "\x1b[38;2;229;167;107;48;2;22;22;22m   **literal**\n\x1b[0m\nafter"},
	} {
		out.Reset()
		if err := Render(&out, tc.source, true); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Fatalf("output=%q; want to contain %q", out.String(), tc.want)
		}
	}
}

func TestBlockquoteScreenBackgroundAndEditing(t *testing.T) {
	source := "before\n> **long quoted text**\n>\n> > nested\ncontinuation\n> A | B\n> --- | ---\n> x | y\n\nafter"
	a, screen := testApp(t, source, 16, 40)
	check := func() {
		t.Helper()
		width, _ := screen.Size()
		for index, row := range a.layout.rows {
			want := tcell.NewHexColor(0x181818)
			if row.line >= 1 && row.line <= 7 {
				want = tcell.NewHexColor(0x181c24)
			}
			for x := 0; x < width; x++ {
				_, _, style, _ := screen.GetContent(x, index+1)
				_, bg, _ := style.Decompose()
				if bg == tcell.ColorDefault {
					_, bg, _ = baseStyle.Decompose()
				}
				if bg != want {
					t.Fatalf("line %d, cell %d:%d: background %v; want %v", row.line, x, index+1, bg, want)
				}
			}
		}
	}
	check()
	p := a.projection
	for _, width := range []int{1, 16, 40} {
		screen.SetSize(width, 100)
		for _, line := range p.lines[1:8] {
			for pos := line.start; pos <= line.end; pos = next(source, pos) {
				a.doc.cursor = pos
				a.render()
				check()
				var active strings.Builder
				for _, row := range a.layout.rows {
					if row.line == lineAt(p.lines, pos) {
						active.WriteString(rowText(row))
					}
				}
				if active.String() != source[line.start:line.end] {
					t.Fatalf("active line lost source: %q", active.String())
				}
				x, y, visible := screen.GetCursor()
				if !visible || x < 0 || x >= width || a.layout.rows[y-1].line != lineAt(p.lines, pos) {
					t.Fatalf("cursor at %d:%d, visible=%v, source offset %d", x, y, visible, pos)
				}
				if pos == line.end {
					break
				}
			}
		}
	}
	if a.doc.text != source {
		t.Fatal("rendering changed the document")
	}
	a.doc.edit(0, len(source), "plain")
	a.render()
	for x := 0; x < 40; x++ {
		_, _, style, _ := screen.GetContent(x, 1)
		_, bg, _ := style.Decompose()
		if bg != tcell.ColorDefault && bg != tcell.NewHexColor(0x181818) {
			t.Fatal("quote background persisted after removing the quote")
		}
	}
	a.doc.undoEdit()
	a.render()
	check()
}

func TestBlockquoteMarkerSpacingLayout(t *testing.T) {
	source := "> > **quote** and `a > b`\n>\n> ```\n> > literal\n> ```\n\nafter"
	d := newDocument([]byte(source))
	d.cursor = len(source)
	p := project(source)
	want := []string{"      quote and a > b", "  ", "", "   > literal", "", "", "after"}
	for _, width := range []int{1, 2, 8, 80} {
		l := makeLayout(d, p, width)
		got := make([]string, len(p.lines))
		for _, row := range l.rows {
			got[row.line] += rowText(row)
		}
		for index, value := range got {
			if value != want[index] {
				t.Errorf("width %d, line %d: %q; want %q", width, index, value, want[index])
			}
		}
	}
}
