package editor

import (
	"strings"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

var baseStyle = tcell.StyleDefault.
	Foreground(tcell.NewHexColor(0xe6e6e6)).
	Background(tcell.NewHexColor(0x202020))

var codeBackground = tcell.NewHexColor(0x161616)
var quoteBackground = tcell.NewHexColor(0x181c24)

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

// Fitting and grapheme layout depend on source and width, not cursor movement.
// Keep both views of each line; changing the active line only assembles rows.
type projectionLayout struct {
	width               int
	rendered, source    [][]displayRow
	rows                []displayRow
	active, activeStart int
	activeCount         int
	endRow              bool
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
	cache := p.layoutCache
	if cache == nil || cache.width != width {
		cache = &projectionLayout{
			width: width, active: -1,
			rendered: make([][]displayRow, len(p.lines)),
			source:   make([][]displayRow, len(p.lines)),
		}
		tableLines := p.fitTables(width)
		for index := range p.lines {
			tableRows, tableLine := tableLines[index]
			exposeSource := tableLine && tableRows == nil
			cache.rendered[index] = layoutLine(d.text, p, index, width, tableRows, tableLine, exposeSource)
			if exposeSource {
				cache.source[index] = cache.rendered[index]
			}
		}
		p.layoutCache = cache
	}
	if cache.source[active] == nil {
		cache.source[active] = layoutLine(d.text, p, active, width, nil, false, true)
	}
	activeRows := cache.source[active]
	last := activeRows[len(activeRows)-1]
	x := last.points[len(last.points)-1].x
	endRow := x >= width && d.cursor == p.lines[active].end
	if cache.active != active || cache.endRow != endRow {
		rows := make([]displayRow, 0, len(cache.rows))
		for index, rendered := range cache.rendered {
			if index == active {
				cache.activeStart = len(rows)
				rows = append(rows, activeRows...)
				if endRow {
					rows = append(rows, displayRow{line: index, points: []point{{0, d.cursor}}})
				}
				cache.activeCount = len(rows) - cache.activeStart
			} else {
				rows = append(rows, rendered...)
			}
		}
		cache.rows, cache.active, cache.endRow = rows, active, endRow
	}
	result := layout{rows: cache.rows}
	// Search only the active source line, including its optional end row.
	rows := result.rows[cache.activeStart : cache.activeStart+cache.activeCount]
	for r, row := range rows {
		for _, cell := range row.cells {
			if cell.start == d.cursor {
				result.cursorX, result.cursorRow = cell.x, cache.activeStart+r
				return result
			}
		}
	}
	for r, row := range rows {
		for _, pt := range row.points {
			if pt.pos == d.cursor && pt.x < width {
				result.cursorX, result.cursorRow = pt.x, cache.activeStart+r
			}
		}
	}
	return result
}

func layoutLine(source string, p *projection, index, width int, tableRows [][]projectedCell, tableLine, exposeSource bool) []displayRow {
	var rows []displayRow
	line := p.lines[index]
	row := displayRow{line: index, points: []point{{0, line.start}}}
	x := 0
	add := func(start, end int, text string, f format) {
		f |= p.lineFormat(index)
		text = safeText(text)
		w := uniseg.StringWidth(text)
		if w == 0 {
			text, w = "◌"+text, 1
		}
		if w > width {
			text, w = "�", 1
		}
		if x+w > width {
			rows = append(rows, row)
			row = displayRow{line: index, points: []point{{0, start}}}
			x = 0
		}
		row.points = append(row.points, point{x, start})
		row.cells = append(row.cells, displayCell{x, text, start, end, f})
		x += w
		row.points = append(row.points, point{x, end})
	}
	if tableLine && !exposeSource {
		for n, cells := range tableRows {
			if n > 0 {
				rows = append(rows, row)
				row = displayRow{line: index}
				x = 0
			}
			for _, cell := range cells {
				g := uniseg.NewGraphemes(cell.text)
				for g.Next() {
					add(cell.start, cell.end, g.Str(), cell.format)
				}
			}
		}
	} else {
		for pos := line.start; pos < line.end; {
			if !exposeSource {
				if p.hidden[pos] {
					pos++
					continue
				}
				if r, ok := p.replacements[pos]; ok {
					g := uniseg.NewGraphemes(r.text)
					for g.Next() {
						add(pos, r.end, g.Str(), p.formats[pos])
					}
					pos = r.end
					continue
				}
			}
			end := min(next(source, pos), line.end)
			cluster := source[pos:end]
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
	}
	row.points = append(row.points, point{x, line.end})
	return append(rows, row)
}

func nearest(row displayRow, x int) int {
	return nearestPoint(row, x).pos
}

func nearestPoint(row displayRow, x int) point {
	pt, best := row.points[0], int(^uint(0)>>1)
	for _, p := range row.points {
		distance := p.x - x
		if distance < 0 {
			distance = -distance
		}
		if distance < best {
			pt, best = p, distance
		}
	}
	return pt
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
		s = s.Foreground(tcell.NewHexColor(0xe5a76b))
	}
	if f&heading != 0 {
		s = s.Foreground(tcell.NewHexColor(0xe5c07b))
		if f&heading1 != 0 {
			s = s.Foreground(tcell.NewHexColor(0xffdf5d))
		}
	}
	if f&blockquote != 0 {
		s = s.Background(quoteBackground)
	}
	if f&codeBlock != 0 {
		s = s.Background(codeBackground)
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
