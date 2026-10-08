package editor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
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

func TestTableFitting(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		width        int
		want         []string
	}{
		{
			name: "word wrapping and short cell padding", width: 14,
			source: "A | B\n:--- | ---:\nhello world | 7",
			want:   []string{"│ A      │ B │", "├────────┼───┤", "│ hello  │ 7 │", "│ world  │   │"},
		},
		{
			name: "long words", width: 12,
			source: "A | B\n--- | ---\nabcdefghij | x",
			want:   []string{"│ A    │ B │", "├──────┼───┤", "│ abcd │ x │", "│ efgh │   │", "│ ij   │   │"},
		},
		{
			name: "wide and combining graphemes", width: 11,
			source: "A | B\n--- | ---\n界界 | e\u0301👩‍💻",
			want:   []string{"│ A  │ B  │", "├────┼────┤", "│ 界 │ e\u0301  │", "│ 界 │ 👩‍💻 │"},
		},
		{
			name: "left to right tie breaking", width: 10,
			source: "A | B\n--- | ---\nabcd | efgh",
			want:   []string{"│ A │ B  │", "├───┼────┤", "│ a │ ef │", "│ b │ gh │", "│ c │    │", "│ d │    │"},
		},
		{
			name: "alignment on continuation rows", width: 16,
			source: "A | B | C\n:--- | ---: | :---:\nabcdefgh | 12x45 | uv",
			want:   []string{"│ A  │  B │ C  │", "├────┼────┼────┤", "│ ab │ 12 │ uv │", "│ cd │ x4 │    │", "│ ef │  5 │    │", "│ gh │    │    │"},
		},
		{
			name: "wrapped list header", width: 16,
			source: "- Long header | B\n  --- | ---\n  x | y",
			want:   []string{"- │ Long   │ B │", "  │ header │   │", "  ├────────┼───┤", "  │ x      │ y │"},
		},
		{
			name: "blockquote", width: 16,
			source: "> A | B\n> --- | ---\n> hello world | x",
			want:   []string{"> │ A      │ B │", "> ├────────┼───┤", "> │ hello  │ x │", "> │ world  │   │"},
		},
		{
			name: "tabs and empty cells", width: 9,
			source: "A | B\n--- | ---\na\tb |\n|",
			want:   []string{"│ A │ B │", "├───┼───┤", "│ a │   │", "│ b │   │", "│   │   │"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.source + "\n\nafter"
			d := newDocument([]byte(source))
			d.cursor = len(source)
			p := project(source)
			l := makeLayout(d, p, tc.width)
			want := append(tc.want, "", "after")
			if len(l.rows) != len(want) {
				var got []string
				for _, row := range l.rows {
					got = append(got, rowText(row))
				}
				t.Fatalf("rows=%q; want %q", got, want)
			}
			for i, row := range l.rows {
				if got := rowText(row); got != want[i] {
					t.Errorf("row %d=%q; want %q", i, got, want[i])
				}
				if uniseg.StringWidth(rowText(row)) > tc.width {
					t.Fatalf("row outside terminal: %q", rowText(row))
				}
				for _, pt := range row.points {
					line := p.lines[row.line]
					if pt.pos < line.start || pt.pos > line.end {
						t.Fatalf("point outside source line: %#v", pt)
					}
				}
			}
			if d.text != source {
				t.Fatal("fitting mutated source")
			}
		})
	}
}

func TestTableNarrowSourceFallback(t *testing.T) {
	for _, source := range []string{
		"A | B\n--- | ---\n**hello** | *world*\n\nafter",
		"> A | B\n> --- | ---\n> **hello** | *world*\n\nafter",
		"A | B\n--- | ---\n界 | 👩‍💻\n\nafter",
	} {
		d := newDocument([]byte(source))
		d.cursor = len(source)
		p := project(source)
		for _, width := range []int{1, 3, 8} {
			l := makeLayout(d, p, width)
			got := make([]string, len(p.lines))
			for _, row := range l.rows {
				got[row.line] += rowText(row)
			}
			for i, line := range p.lines {
				want := source[line.start:line.end]
				if width == 1 {
					want = strings.ReplaceAll(strings.ReplaceAll(want, "界", "�"), "👩‍💻", "�")
				}
				if got[i] != want {
					t.Fatalf("width %d line %d=%q; want source %q", width, i, got[i], want)
				}
			}
		}
		// A two-cell grapheme increases the minimum width of its column.
		if strings.Contains(source, "界") {
			for _, row := range makeLayout(d, p, 10).rows {
				if strings.ContainsAny(rowText(row), "│─") {
					t.Fatal("table rendered below its Unicode minimum width")
				}
			}
		}
	}
}

func TestFittedTableResizeStylesAndNavigation(t *testing.T) {
	source := "A | B\n:--- | ---:\n**hello world** | *xy* `z`\n\nend"
	a, screen := testApp(t, source, 80, 24)
	a.doc.cursor = len(source)
	a.render()
	p := a.projection
	var natural []string
	for _, row := range a.layout.rows {
		natural = append(natural, rowText(row))
	}
	var output bytes.Buffer
	if err := Render(&output, source, true); err != nil {
		t.Fatal(err)
	}
	wantOutput := output.String()
	for _, width := range []int{17, 8, 1, 80, 17} {
		screen.SetSize(width, 24)
		a.handle(tcell.NewEventResize(width, 24))
		a.render()
		if a.projection != p {
			t.Fatal("resize unnecessarily reparsed Markdown")
		}
		for _, row := range a.layout.rows {
			if uniseg.StringWidth(rowText(row)) > width {
				t.Fatalf("row outside resized terminal: %q", rowText(row))
			}
		}
		if width == 80 {
			if len(a.layout.rows) != len(natural) {
				t.Fatal("widening did not restore natural layout")
			}
			for i, row := range a.layout.rows {
				if rowText(row) != natural[i] {
					t.Fatal("widening did not restore natural column sizes")
				}
			}
		}
		x, y, visible := screen.GetCursor()
		if !visible || x < 0 || x >= width || y < 1 || y >= 22 {
			t.Fatalf("cursor outside resized viewport: %d:%d, visible=%v", x, y, visible)
		}
	}
	// A search highlight and inline styles survive on a continuation line.
	a.matchStart = strings.Index(source, "world")
	a.matchEnd = a.matchStart + len("world")
	a.render()
	seen := make(map[int]bool)
	var continuation displayRow
	for index, row := range a.layout.rows {
		if row.line == 2 {
			continuation = row
		}
		for _, cell := range row.cells {
			if cell.start == cell.end {
				continue
			}
			seen[cell.start] = true
			_, _, style, _ := screen.GetContent(cell.x, index+1)
			foreground, _, attrs := style.Decompose()
			if cell.format&bold != 0 && attrs&tcell.AttrBold == 0 {
				t.Fatal("wrapped header or bold text lost style")
			}
			if cell.format&italic != 0 && attrs&tcell.AttrItalic == 0 {
				t.Fatal("wrapped italic text lost style")
			}
			if cell.format&code != 0 && foreground != tcell.NewHexColor(0xe5a76b) {
				t.Fatal("wrapped code lost style")
			}
			if cell.start >= a.matchStart && cell.end <= a.matchEnd && attrs&tcell.AttrReverse == 0 {
				t.Fatal("search highlight lost on continuation line")
			}
		}
	}
	for _, word := range []string{"hello", "world", "xy", "z"} {
		start := strings.Index(source, word)
		for i := start; i < start+len(word); i++ {
			if !seen[i] {
				t.Fatalf("fitting lost content at source offset %d", i)
			}
		}
	}
	expected := nearest(continuation, 3)
	a.goalColumn = 3
	key(a, tcell.KeyUp)
	key(a, tcell.KeyUp)
	a.render()
	if a.doc.cursor != expected || lineAt(p.lines, a.doc.cursor) != 2 {
		t.Fatalf("navigation into continuation line landed at %d; want %d", a.doc.cursor, expected)
	}
	var active strings.Builder
	for _, row := range a.layout.rows {
		if row.line == 2 {
			active.WriteString(rowText(row))
		}
	}
	line := p.lines[2]
	if active.String() != source[line.start:line.end] {
		t.Fatal("active wrapped row did not expose full source")
	}
	if a.doc.text != source {
		t.Fatal("resize or navigation mutated source")
	}
	output.Reset()
	if err := Render(&output, source, true); err != nil {
		t.Fatal(err)
	}
	if output.String() != wantOutput {
		t.Fatal("interactive fitting changed printed output")
	}
}

func TestVerticalTableNavigationMakesProgress(t *testing.T) {
	source := "before\nA | B\n--- | ---\n" + strings.Repeat("word ", 12) + " | value\n" +
		"a\tb | " + strings.Repeat("long ", 12) + "\n\nafter"
	for _, width := range []int{1, 3, 9, 14, 30, 80} {
		a, _ := testApp(t, source, width, 10)
		for _, direction := range []int{1, -1} {
			if direction > 0 {
				a.doc.cursor = 0
			} else {
				a.doc.cursor = len(source)
			}
			a.goalColumn = 0
			for step := 0; step <= len(source); step++ {
				a.refreshLayout()
				boundary := 0
				if direction > 0 {
					boundary = len(a.layout.rows) - 1
				}
				if a.layout.cursorRow == boundary {
					break
				}
				before := a.doc.cursor
				a.moveVertical(direction)
				if (a.doc.cursor-before)*direction <= 0 {
					t.Fatalf("width %d direction %d: cursor stalled or reversed at %d -> %d", width, direction, before, a.doc.cursor)
				}
				if step == len(source) {
					t.Fatal("vertical navigation never reached document boundary")
				}
			}
		}
		if a.doc.text != source {
			t.Fatal("navigation changed source")
		}
	}
}

func BenchmarkVerticalTableNavigation(b *testing.B) {
	source := "A | B\n--- | ---\n" + strings.Repeat(strings.Repeat("word ", 30)+" | value\n", 200) + "\nafter"
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(screen.Fini)
	screen.SetSize(80, 24)
	a := newApp(screen, nil, newDocument([]byte(source)))
	a.refreshLayout()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.moveVertical(1)
		a.refreshLayout()
		a.moveVertical(-1)
		a.refreshLayout()
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
