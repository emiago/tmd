package editor

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

type navigationScreen struct {
	tcell.Screen
	poll           func() tcell.Event
	clears, writes int
}

func (s *navigationScreen) PollEvent() tcell.Event { return s.poll() }

func (s *navigationScreen) Clear() {
	s.clears++
	s.Screen.Clear()
}

func (s *navigationScreen) SetContent(x, y int, main rune, combining []rune, style tcell.Style) {
	s.writes++
	s.Screen.SetContent(x, y, main, combining, style)
}

func TestHeldTableNavigationDefersSourceAndRedraw(t *testing.T) {
	source := "before\nA | B\n--- | ---\n**hello world again** | x\n\nafter"
	a, screen := testApp(t, source, 14, 24)
	replay := &navigationScreen{Screen: screen}
	a.screen = replay
	keys := []tcell.Key{tcell.KeyDown, tcell.KeyDown, tcell.KeyDown, tcell.KeyDown, tcell.KeyUp}
	poll, writes := 0, 0
	var frozen *displayRow
	replay.poll = func() tcell.Event {
		defer func() { poll++ }()
		if poll == 1 {
			writes = replay.writes
			frozen = &a.layout.rows[0]
		}
		if poll > 0 && poll <= len(keys) {
			if !a.navigating || &a.layout.rows[0] != frozen {
				t.Fatal("held navigation rebuilt the layout")
			}
			if replay.clears != 1 || replay.writes != writes {
				t.Fatal("held navigation rewrote document text")
			}
			for _, row := range a.layout.rows {
				if _, table := a.projection.tableLines[row.line]; table && strings.ContainsAny(rowText(row), "|*") {
					t.Fatalf("table exposed source while navigation was held: %q", rowText(row))
				}
			}
			wantRow := poll
			if poll == len(keys) {
				wantRow = 3
			}
			if a.layout.cursorRow != wantRow {
				t.Fatalf("cursor did not move over frozen rows: %d; want %d", a.layout.cursorRow, wantRow)
			}
			if poll == 4 && a.doc.cursor != strings.Index(source, "world") {
				t.Fatal("continuation row's border mapped the cursor back to the start of the cell")
			}
			x, y, visible := screen.GetCursor()
			if !visible || y != wantRow+1 || x != a.layout.cursorX {
				t.Fatalf("screen cursor did not follow held navigation: %d:%d", x, y)
			}
		}
		if poll < len(keys) {
			return tcell.NewEventKey(keys[poll], 0, tcell.ModNone)
		}
		if poll == len(keys) {
			// Exercise the real timer wake-up, with no additional key event.
			return screen.PollEvent()
		}
		if a.navigating || replay.clears != 2 {
			t.Fatal("idle did not redraw once and resume source editing")
		}
		var active strings.Builder
		for _, row := range a.layout.rows {
			if row.line == 3 {
				active.WriteString(rowText(row))
			}
		}
		line := a.projection.lines[3]
		if active.String() != source[line.start:line.end] {
			t.Fatal("idle did not reveal the current row's complete source")
		}
		return nil
	}
	_ = a.run()
	if a.doc.text != source {
		t.Fatal("held navigation changed the document")
	}
}

func TestTableNavigationStartsInWrappedSourceRow(t *testing.T) {
	source := "A | B\n--- | ---\n**hello world again** | x\n\nafter"
	a, _ := testApp(t, source, 14, 24)
	a.doc.cursor = strings.Index(source, "hello")
	a.render()
	if !a.beginNavigation() {
		t.Fatal("navigation did not render the previously active table row")
	}
	for _, row := range a.layout.rows {
		if _, table := a.projection.tableLines[row.line]; table && strings.ContainsAny(rowText(row), "|*") {
			t.Fatalf("source remained exposed during navigation: %q", rowText(row))
		}
	}
	a.moveVertical(1)
	if a.doc.cursor != strings.Index(source, "again")+2 {
		t.Fatal("first arrow did not advance within the original wrapped source")
	}
	start := a.layout.cursorRow
	a.moveVertical(-1)
	if a.layout.cursorRow != start-1 {
		t.Fatal("held navigation did not move over the fitted continuation row")
	}
	a.moveVertical(1)
	if a.layout.cursorRow != start {
		t.Fatal("reversing direction did not return to the same fitted display row")
	}
}

func TestTableFirstArrowUsesVisibleSourceRow(t *testing.T) {
	source := "A | B | C | D\n--- | --- | --- | ---\nabcdefgh | x | y | z\n" +
		"long content | long content | long content | long content\nnext | row | here | end\n\nafter"
	for _, direction := range []int{1, -1} {
		a, _ := testApp(t, source, 29, 24)
		line := a.projection.lines[2]
		for pos := line.start; pos <= line.end; pos++ {
			a.navigating = false
			a.goalColumn = -1
			a.doc.cursor = pos
			a.render()
			a.beginNavigation()
			a.moveVertical(direction)
			if got := lineAt(a.projection.lines, a.doc.cursor); got != 2+direction {
				t.Fatalf("direction %d cursor %d: moved to source row %d; want %d", direction, pos-line.start, got, 2+direction)
			}
		}
	}
}

func TestSingleDownLeavesTableSourceRowAfterIdle(t *testing.T) {
	source := "A | B | C | D\n--- | --- | --- | ---\nabcdefgh | x | y | z\n" +
		"long content | long content | long content | long content\nnext | row | here | end\n\nafter"
	a, screen := testApp(t, source, 29, 24)
	a.doc.cursor = a.projection.lines[2].start
	a.render()
	replay := &navigationScreen{Screen: screen}
	a.screen = replay
	poll := 0
	replay.poll = func() tcell.Event {
		defer func() { poll++ }()
		switch poll {
		case 0:
			return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
		case 1:
			if a.layout.rows[a.layout.cursorRow].line != 3 {
				t.Fatal("Down landed on a newly fitted continuation of the original row")
			}
			return screen.PollEvent()
		default:
			if a.navigating || lineAt(a.projection.lines, a.doc.cursor) != 3 || a.layout.rows[a.layout.cursorRow].line != 3 {
				t.Fatal("redraw returned the cursor to the original source row")
			}
			return nil
		}
	}
	_ = a.run()
	if a.doc.text != source {
		t.Fatal("navigation changed the document")
	}
}

func TestTableNavigationEndsImmediatelyForEditing(t *testing.T) {
	for _, action := range []tcell.Event{
		tcell.NewEventKey(tcell.KeyRune, '!', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyCtrlW, 0, tcell.ModNone),
		tcell.NewEventResize(12, 24),
	} {
		source := "before\nA | B\n--- | ---\nhello world | x\n\nafter"
		a, screen := testApp(t, source, 14, 24)
		replay := &navigationScreen{Screen: screen}
		a.screen = replay
		poll := 0
		replay.poll = func() tcell.Event {
			defer func() { poll++ }()
			if poll == 0 {
				return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
			}
			if poll == 1 {
				if !a.navigating {
					t.Fatal("table navigation was not deferred")
				}
				if _, resize := action.(*tcell.EventResize); resize {
					screen.SetSize(12, 24)
				}
				return action
			}
			if a.navigating || replay.clears != 2 {
				t.Fatal("non-navigation action did not immediately resume normal rendering")
			}
			return nil
		}
		_ = a.run()
		if e, ok := action.(*tcell.EventKey); ok {
			if e.Key() == tcell.KeyRune && !strings.Contains(a.doc.text, "!") {
				t.Fatal("typing during held navigation lost the edit")
			}
			if e.Key() == tcell.KeyCtrlW && a.prompt == nil {
				t.Fatal("search prompt did not open immediately")
			}
		}
	}
}

func TestLayoutCacheTracksWidthAndEdits(t *testing.T) {
	a, screen := testApp(t, "A | B\n--- | ---\nhello world | x\n\nafter", 14, 24)
	a.doc.cursor = len(a.doc.text)
	a.refreshLayout()
	cache := a.projection.layoutCache
	fitted := &cache.rendered[2][0]
	a.moveVertical(-1)
	a.refreshLayout()
	a.moveVertical(-1)
	a.refreshLayout()
	if a.projection.layoutCache != cache || &cache.rendered[2][0] != fitted {
		t.Fatal("cursor movement refitted the table")
	}
	screen.SetSize(20, 24)
	a.refreshLayout()
	if a.projection.layoutCache == cache || a.projection.layoutCache.width != 20 {
		t.Fatal("resize did not invalidate the layout cache")
	}
	cache = a.projection.layoutCache
	a.doc.insert("!")
	a.refreshLayout()
	if a.projection.layoutCache == cache {
		t.Fatal("editing did not invalidate the layout cache")
	}
}

func TestHeldTableNavigationScrollsWithoutReflow(t *testing.T) {
	source := "before\nA | B\n--- | ---\n" + strings.Repeat("hello world | x\n", 20) + "\nafter"
	a, screen := testApp(t, source, 14, 8)
	a.beginNavigation()
	frozen := &a.layout.rows[0]
	for _, direction := range []int{1, -1} {
		for step := 0; step < 25; step++ {
			before := a.layout.cursorRow
			a.moveVertical(direction)
			a.renderNavigationCursor()
			if a.layout.cursorRow != before+direction || &a.layout.rows[0] != frozen {
				t.Fatal("scrolling over a held table changed its layout or lost cursor movement")
			}
			x, y, visible := screen.GetCursor()
			if !visible || x < 0 || x >= 14 || y < 1 || y >= 6 {
				t.Fatalf("held cursor escaped scrolling viewport: %d:%d", x, y)
			}
		}
	}
	if a.doc.text != source {
		t.Fatal("scrolling changed source")
	}
}

func BenchmarkHeldTableNavigation(b *testing.B) {
	source := "before\nA | B\n--- | ---\n" + strings.Repeat(strings.Repeat("word ", 30)+" | value\n", 200) + "\nafter"
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(screen.Fini)
	screen.SetSize(80, 24)
	a := newApp(screen, nil, newDocument([]byte(source)))
	a.beginNavigation()
	a.moveVertical(3)
	a.goalColumn = 4
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.moveVertical(1)
		a.moveVertical(-1)
	}
}
