package editor

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// Replay the production event loop, including rendering between events.
// Calling handle alone misses the per-character redraw regression.
type pasteScreen struct {
	tcell.Screen
	events []tcell.Event
	shows  int
}

func (s *pasteScreen) PollEvent() tcell.Event {
	if len(s.events) == 0 {
		return nil
	}
	event := s.events[0]
	s.events = s.events[1:]
	return event
}

func (s *pasteScreen) Show() {
	s.shows++
	s.Screen.Show()
}

func pasteEvents(text string) []tcell.Event {
	events := []tcell.Event{tcell.NewEventPaste(true)}
	for _, r := range text {
		events = append(events, tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	return append(events, tcell.NewEventPaste(false))
}

func TestLargePasteEventLoop(t *testing.T) {
	original := strings.Repeat("unchanged **Markdown**\n", 50)
	payload := strings.Repeat("界 e\u0301 👩‍💻\t**pasted**\n", 100) + "last"
	a, screen := testApp(t, original, 80, 24)
	events := pasteEvents(payload)
	// Controls in a paste must not save, exit, cut, or undo the document.
	controls := []tcell.Key{tcell.KeyCtrlO, tcell.KeyCtrlX, tcell.KeyCtrlK, tcell.KeyCtrlU}
	events = events[:len(events)-1]
	for _, control := range controls {
		events = append(events, tcell.NewEventKey(control, 0, tcell.ModNone))
		payload += string(rune(control))
	}
	events = append(events, tcell.NewEventPaste(false), tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	replay := &pasteScreen{Screen: screen, events: events}
	a.screen = replay
	if err := a.run(); err == nil {
		t.Fatal("expected the replayed event stream to close")
	}
	if a.doc.text != payload+original || len(a.doc.undo) != 1 || a.pasting || a.prompt != nil || a.quit {
		t.Fatal("paste changed text, triggered a command, or was not one edit")
	}
	if a.doc.cursor != previous(a.doc.text, len(payload)) {
		t.Fatal("cursor did not respond after paste")
	}
	if replay.shows != 3 {
		t.Fatalf("paste caused %d redraws; want initial, completed paste, and cursor move", replay.shows)
	}
	a.doc.undoEdit()
	if a.doc.text != original || a.doc.cursor != 0 {
		t.Fatal("one undo did not restore the original document and cursor")
	}
}

func TestLongPromptPasteEventLoop(t *testing.T) {
	a, screen := testApp(t, "document stays here", 80, 24)
	a.beginPrompt("search", "", "")
	payload := strings.Repeat("x", 8192) + "END\n\t\x00"
	events := pasteEvents(payload)
	replay := &pasteScreen{Screen: screen, events: events}
	a.screen = replay
	_ = a.run()
	want := strings.Repeat("x", 8192) + "END "
	if a.prompt.value != want || a.prompt.cursor != len(want) || a.doc.text != "document stays here" || len(a.doc.undo) != 0 {
		t.Fatal("prompt paste lost text, retained controls, or edited the document")
	}
	if replay.shows != 2 {
		t.Fatalf("prompt paste caused %d redraws; want 2", replay.shows)
	}
	x, y, visible := screen.GetCursor()
	if x != 79 || y != 22 || !visible {
		t.Fatalf("prompt cursor: %d:%d visible=%v", x, y, visible)
	}
	for i, r := range "END " {
		got, _, _, _ := screen.GetContent(75+i, 22)
		if got != r {
			t.Fatalf("prompt suffix column %d: %q; want %q", 75+i, got, r)
		}
	}
}

func TestPromptScrollGraphemes(t *testing.T) {
	for _, tc := range []struct {
		value string
		first rune
	}{
		{"abcdefgh", 'f'},
		{"aaa界e\u0301", '界'},
		{"aaa👩‍💻", 'a'},
	} {
		t.Run(tc.value, func(t *testing.T) {
			a, screen := testApp(t, "", 8, 24)
			a.beginPrompt("search", "Find: ", tc.value)
			a.render()
			x, y, visible := screen.GetCursor()
			if x != 7 || y != 22 || !visible {
				t.Fatalf("scrolled prompt cursor: %d:%d visible=%v", x, y, visible)
			}
			got, _, _, _ := screen.GetContent(4, 22)
			if got != tc.first {
				t.Fatalf("visible suffix starts with %q; want %q", got, tc.first)
			}
		})
	}
}

func BenchmarkExternalPaste(b *testing.B) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		b.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	source := strings.Repeat("unchanged **Markdown**\n", 100)
	payload := strings.Repeat("pasted text\n", 100)
	events := pasteEvents(payload)
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		replay := &pasteScreen{Screen: screen, events: events}
		a := newApp(replay, &fileState{path: "notes.md", data: []byte(source)}, newDocument([]byte(source)))
		_ = a.run()
	}
}

func BenchmarkLongPastedPrompt(b *testing.B) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		b.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	a := newApp(screen, nil, nil)
	a.beginPrompt("search", "Search: ", strings.Repeat("a", 8192))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.renderPrompt(80, 22)
	}
}
