package editor

import "github.com/gdamore/tcell/v2"

// Terminals report key repeats, not key releases. A short idle interval marks
// the end of a navigation burst; until then table rows keep their display view.
func (a *app) deferNavigation(event tcell.Event) bool {
	e, ok := event.(*tcell.EventKey)
	if !ok || a.prompt != nil || a.help || a.pasting {
		return false
	}
	delta := 0
	_, h := a.screen.Size()
	switch e.Key() {
	case tcell.KeyUp:
		delta = -1
	case tcell.KeyDown:
		delta = 1
	case tcell.KeyPgUp:
		delta = -max(1, h-3)
	case tcell.KeyPgDn:
		delta = max(1, h-3)
	default:
		return false
	}
	if a.navigating {
		return true
	}
	target := max(0, min(len(a.layout.rows)-1, a.layout.cursorRow+delta))
	start, end := min(a.layout.cursorRow, target), max(a.layout.cursorRow, target)
	for _, row := range a.layout.rows[start : end+1] {
		if _, table := a.projection.tableLines[row.line]; table {
			return true
		}
	}
	return false
}

func (a *app) beginNavigation() bool {
	a.refreshLayout()
	a.navigating = true
	a.navigationOrigin = nil
	line := lineAt(a.projection.lines, a.doc.cursor)
	if _, table := a.projection.tableLines[line]; !table {
		return false
	}
	origin := a.layout
	a.navigationOrigin = &origin
	// If the burst starts inside a table, put its previously active row back
	// into display form once. No table row exposes source during the burst.
	cache := a.projection.layoutCache
	start, end := cache.activeStart, cache.activeStart+cache.activeCount
	rows := make([]displayRow, 0, len(a.layout.rows)-cache.activeCount+len(cache.rendered[line]))
	rows = append(rows, a.layout.rows[:start]...)
	rows = append(rows, cache.rendered[line]...)
	rows = append(rows, a.layout.rows[end:]...)
	a.layout.rows = rows
	best := int(^uint(0) >> 1)
	for r, row := range cache.rendered[line] {
		for _, pt := range row.points {
			if pt.x >= cache.width {
				continue
			}
			if distance := abs(pt.pos - a.doc.cursor); distance < best {
				best = distance
				a.layout.cursorRow, a.layout.cursorX = start+r, pt.x
			}
		}
	}
	return true
}

// The first arrow uses the view the user was looking at. Fitting a source row
// can introduce continuation rows that must not swallow that initial movement.
func (a *app) moveFromNavigationOrigin(delta int) {
	origin := a.navigationOrigin
	a.navigationOrigin = nil
	row, pt, ok := sourceVerticalTarget(*origin, a.doc.cursor, a.goalColumn, delta)
	if !ok {
		return
	}
	a.doc.cursor = pt.pos
	a.doc.snapCursor()
	w, _ := a.screen.Size()
	if origin.rows[row].line == origin.rows[origin.cursorRow].line {
		// This was a real source wrap. Locate the chosen insertion point in
		// the fitted row, rather than treating a new fitted wrap as movement.
		best := int(^uint(0) >> 1)
		for index, display := range a.layout.rows {
			if display.line != origin.rows[row].line {
				continue
			}
			for _, candidate := range display.points {
				if candidate.x >= max(1, w) {
					continue
				}
				if distance := abs(candidate.pos - a.doc.cursor); distance < best {
					best = distance
					a.layout.cursorRow, a.layout.cursorX = index, candidate.x
				}
			}
		}
	} else {
		// Other rows retain their layout; only indices below the previously
		// active row shift when its source view is replaced by its fitted view.
		if row > origin.cursorRow {
			row += len(a.layout.rows) - len(origin.rows)
		}
		a.layout.cursorRow, a.layout.cursorX = row, min(max(0, w-1), pt.x)
	}
}

func sourceVerticalTarget(l layout, cursor, column, delta int) (int, point, bool) {
	row := max(0, min(len(l.rows)-1, l.cursorRow+delta))
	if row == l.cursorRow {
		return 0, point{}, false
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	for ; row >= 0 && row < len(l.rows); row += step {
		// Expanded tabs can share a source position across display rows.
		// Choose a reachable insertion point in the requested direction.
		var best point
		distance := int(^uint(0) >> 1)
		for _, pt := range l.rows[row].points {
			if (pt.pos-cursor)*step <= 0 {
				continue
			}
			if d := abs(pt.x - column); d < distance {
				best, distance = pt, d
			}
		}
		if distance < int(^uint(0)>>1) {
			return row, best, true
		}
	}
	return 0, point{}, false
}

func (a *app) renderNavigationCursor() {
	w, h := a.screen.Size()
	height := max(1, h-3)
	previousTop := a.top
	a.top = max(0, min(a.top, max(0, len(a.layout.rows)-height)))
	if a.layout.cursorRow < a.top {
		a.top = a.layout.cursorRow
	}
	if a.layout.cursorRow >= a.top+height {
		a.top = a.layout.cursorRow - height + 1
	}
	if a.top != previousTop {
		// Scrolling redraws the viewport using the same frozen table layout.
		a.render()
		return
	}
	if w > 0 && h >= 4 {
		a.screen.ShowCursor(a.layout.cursorX, a.layout.cursorRow-a.top+1)
	} else {
		a.screen.HideCursor()
	}
	a.screen.Show()
}
