package editor

import (
	"strings"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

var baseStyle = tcell.StyleDefault.
	Foreground(tcell.NewHexColor(0xe6e6e6)).
	Background(tcell.NewHexColor(0x222222))

type displayCell struct {
	x          int
	text       string
	start, end int
	format     format
}
type point struct{ x, pos int }
type displayRow struct {
	line   int
	cells  []displayCell
	points []point
}
type layout struct {
	rows               []displayRow
	cursorX, cursorRow int
}

func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, s)
}

func makeLayout(d *document, p *projection, width int) layout {
	width = max(1, width)
	active := lineAt(p.lines, d.cursor)
	result := layout{}
	for index, line := range p.lines {
		row := displayRow{line: index, points: []point{{0, line.start}}}
		x := 0
		add := func(start, end int, text string, f format) {
			text = safeText(text)
			w := uniseg.StringWidth(text)
			if w == 0 {
				text, w = "◌"+text, 1
			}
			if w > width {
				text, w = "�", 1
			}
			if x+w > width {
				result.rows = append(result.rows, row)
				row = displayRow{line: index, points: []point{{0, start}}}
				x = 0
			}
			row.points = append(row.points, point{x, start})
			row.cells = append(row.cells, displayCell{x, text, start, end, f})
			x += w
			row.points = append(row.points, point{x, end})
		}
		for pos := line.start; pos < line.end; {
			if index != active {
				if r, ok := p.replacements[pos]; ok {
					add(pos, r.end, r.text, p.formats[pos])
					pos = r.end
					continue
				}
				if p.hidden[pos] {
					pos++
					continue
				}
			}
			end := min(next(d.text, pos), line.end)
			cluster := d.text[pos:end]
			if cluster == "\t" {
				// Tabs use four-cell stops; represent each cell independently so
				// even a one-column terminal can wrap safely.
				spaces := 4 - x%4
				for i := 0; i < spaces; i++ {
					add(pos, end, " ", p.formats[pos])
				}
			} else {
				add(pos, end, cluster, p.formats[pos])
			}
			pos = end
		}
		// An insertion point after a completely full line needs its own row.
		if x >= width && index == active && d.cursor == line.end {
			result.rows = append(result.rows, row)
			row = displayRow{line: index, points: []point{{0, line.end}}}
			x = 0
		}
		row.points = append(row.points, point{x, line.end})
		result.rows = append(result.rows, row)
	}
	// A token's first cell owns its starting insertion point. In particular,
	// repeated cells of an expanded tab must not move the cursor into the tab.
	for r, row := range result.rows {
		if row.line != active {
			continue
		}
		for _, cell := range row.cells {
			if cell.start == d.cursor {
				result.cursorX, result.cursorRow = cell.x, r
				return result
			}
		}
	}
	// For end-of-line insertion points, later rows win at a wrapping boundary.
	for r, row := range result.rows {
		if row.line != active {
			continue
		}
		for _, pt := range row.points {
			if pt.pos == d.cursor && pt.x < width {
				result.cursorX, result.cursorRow = pt.x, r
			}
		}
	}
	return result
}

func nearest(row displayRow, x int) int {
	pos, best := row.points[0].pos, int(^uint(0)>>1)
	for _, p := range row.points {
		distance := p.x - x
		if distance < 0 {
			distance = -distance
		}
		if distance < best {
			pos, best = p.pos, distance
		}
	}
	return pos
}

func cellStyle(f format) tcell.Style {
	s := baseStyle
	if f&bold != 0 {
		s = s.Bold(true)
	}
	if f&italic != 0 {
		s = s.Italic(true)
	}
	if f&code != 0 {
		s = s.Foreground(tcell.NewHexColor(0x7dcfff))
	}
	if f&heading != 0 {
		s = s.Foreground(tcell.NewHexColor(0xe5c07b))
	}
	return s
}

func drawCell(screen tcell.Screen, x, y int, text string, style tcell.Style) {
	runes := []rune(text)
	if len(runes) == 0 {
		return
	}
	if !screen.CanDisplay(runes[0], true) {
		switch runes[0] {
		case '☐':
			runes = []rune{'□'}
		case '☑':
			runes = []rune{'X'}
		case '•':
			runes = []rune{'-'}
		}
	}
	screen.SetContent(x, y, runes[0], runes[1:], style)
}

func drawText(screen tcell.Screen, x, y, width int, text string, style tcell.Style) int {
	g := uniseg.NewGraphemes(safeText(text))
	for g.Next() {
		w := g.Width()
		if x+w > width {
			break
		}
		drawCell(screen, x, y, g.Str(), style)
		x += w
	}
	return x
}
