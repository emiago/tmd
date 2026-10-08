package editor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestRenderTables(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
	}{
		{"alignment and inline styles", "| Name | Qty | Note |\n| :--- | ---: | :---: |\n| **Tea** | 2 | *hot* |\n| 界 | 12 | `a\\|b` |\n", "│ Name │ Qty │ Note │\n├──────┼─────┼──────┤\n│ Tea  │   2 │ hot  │\n│ 界   │  12 │ a|b  │\n"},
		{"optional outer pipes", "A | B\n--- | ---\nx | y", "│ A │ B │\n├───┼───┤\n│ x │ y │"},
		{"empty missing and extra cells", "| A | B |\n| - | - |\n| | |\n| x |\n| x | y | ignored |", "│ A │ B │\n├───┼───┤\n│   │   │\n│ x │   │\n│ x │ y │"},
		{"entirely empty header and row", "|\n|-|\n|", "│   │\n├───┤\n│   │"},
		{"header only CRLF", "| A | B |\r\n| --- | --- |\r\n", "│ A │ B │\r\n├───┼───┤\r\n"},
		{"preceding paragraph", "before\nA | B\n--- | ---\nx | y\n\nafter", "before\n│ A │ B │\n├───┼───┤\n│ x │ y │\n\nafter"},
		{"separate tables", "A | B\n--- | ---\n\nC | D\n--- | ---", "│ A │ B │\n├───┼───┤\n\n│ C │ D │\n├───┼───┤"},
		{"escaped pipe", "A | B\n--- | ---\nx\\|y | z", "│ A   │ B │\n├─────┼───┤\n│ x|y │ z │"},
		{"combining text and emoji", "A | B\n--- | ---\ne\u0301 | 👩‍💻", "│ A │ B  │\n├───┼────┤\n│ e\u0301 │ 👩‍💻 │"},
		{"tab stops", "A | B\n--- | ---\na\tb | x", "│ A     │ B │\n├───────┼───┤\n│ a   b │ x │"},
		{"blockquote", "> A | B\n> --- | ---\n> x | y", "> │ A │ B │\n> ├───┼───┤\n> │ x │ y │"},
		{"list", "- A | B\n  --- | ---\n  x | y", "- │ A │ B │\n  ├───┼───┤\n  │ x │ y │"},
		{"invalid delimiter", "A | B\n--- | nope\nx | y", "A | B\n--- | nope\nx | y"},
		{"mismatched header", "A | B\n---\nx | y", "A | B\n---\nx | y"},
		{"fenced literal", "```md\nA | B\n--- | ---\nx | y\n```", "A | B\n--- | ---\nx | y\n"},
		{"indented literal", "    A | B\n    - | -\n    x | y", "    A | B\n    - | -\n    x | y"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Render(&out, tc.source, false); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Fatalf("output=%q; want %q", out.String(), tc.want)
			}
		})
	}
}

func TestTableSourceCursorAndWrapping(t *testing.T) {
	source := "| **Name** | Count |\n| :--- | ---: |\n| 界 | 12 |\n\nafter"
	d := newDocument([]byte(source))
	p := project(source)
	d.cursor = len(source)
	l := makeLayout(d, p, 80)
	want := []string{"│ Name │ Count │", "├──────┼───────┤", "│ 界   │    12 │", "", "after"}
	for i, row := range l.rows {
		if got := rowText(row); got != want[i] {
			t.Fatalf("row %d=%q; want %q", i, got, want[i])
		}
		for _, pt := range row.points {
			if pt.pos < p.lines[row.line].start || pt.pos > p.lines[row.line].end {
				t.Fatalf("point outside source row: %#v", pt)
			}
		}
	}
	// Every insertion point on each table line must expose the original source,
	// including syntax and whitespace, and remain reachable after wrapping.
	for _, line := range p.lines[:3] {
		for pos := line.start; pos <= line.end; pos = next(source, pos) {
			d.cursor = pos
			for _, width := range []int{1, 3, 8, 80} {
				l := makeLayout(d, p, width)
				if l.cursorX < 0 || l.cursorX >= width || l.rows[l.cursorRow].line != lineAt(p.lines, pos) {
					t.Fatalf("cursor %d at width %d: %d:%d", pos, width, l.cursorRow, l.cursorX)
				}
				if width == 80 && rowText(l.rows[l.cursorRow]) != source[line.start:line.end] {
					t.Fatal("active table line did not expose its source")
				}
				for _, row := range l.rows {
					for _, cell := range row.cells {
						if cell.x >= width {
							t.Fatal("table cell outside terminal")
						}
					}
				}
			}
			if pos == line.end {
				break
			}
		}
	}
	if d.text != source {
		t.Fatal("table rendering mutated the source")
	}
	d.edit(p.lines[1].start, p.lines[1].end, "plain")
	if len(project(d.text).tableLines) != 0 {
		t.Fatal("editing the delimiter did not remove the table")
	}
	d.undoEdit()
	if d.text != source || len(project(d.text).tableLines) != 3 {
		t.Fatal("undo did not restore the table")
	}
}

func TestTableStyles(t *testing.T) {
	source := "A | B\n--- | ---\n**bold** | *italic* and `code`\n\nafter"
	a, screen := testApp(t, source, 80, 24)
	a.doc.cursor = len(source)
	a.render()
	for index, row := range a.layout.rows {
		for _, cell := range row.cells {
			if (row.line == 0 && cell.text == "A") || (row.line == 2 && cell.text == "b") {
				_, _, style, _ := screen.GetContent(cell.x, index+1)
				_, _, attrs := style.Decompose()
				if attrs&tcell.AttrBold == 0 {
					t.Fatal("table header or inline bold lost its style")
				}
			}
		}
	}
	var out bytes.Buffer
	if err := Render(&out, source, true); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\x1b[1mA\x1b[0m", "\x1b[1mbold\x1b[0m", "\x1b[3mitalic\x1b[0m", "\x1b[38;2;229;167;107mcode\x1b[0m", "\n\nafter"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("styled table lacks %q: %q", want, out.String())
		}
	}
}
