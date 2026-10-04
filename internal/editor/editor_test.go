package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func testApp(t *testing.T, source string, width, height int) (*app, tcell.SimulationScreen) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "notes.md")
	writeTestFile(t, path, source)
	f, d, err := openFile(path)
	if err != nil {
		t.Fatal(err)
	}
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(width, height)
	a := newApp(screen, f, d, saveOptions{})
	a.render()
	return a, screen
}

func key(a *app, k tcell.Key) { a.handle(tcell.NewEventKey(k, 0, tcell.ModNone)) }
func typeText(a *app, text string) {
	for _, r := range text {
		a.handle(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
}
func alt(a *app, r rune) { a.handle(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModAlt)) }

func TestFencedCodeBackground(t *testing.T) {
	a, screen := testApp(t, "before\n```bash\nabcdefghijk\n\n```\nafter", 8, 24)
	check := func() {
		t.Helper()
		for index, row := range a.layout.rows {
			for x := 0; x < 8; x++ {
				_, _, style, _ := screen.GetContent(x, index+1)
				_, bg, _ := style.Decompose()
				if bg == tcell.ColorDefault {
					_, bg, _ = baseStyle.Decompose()
				}
				want := tcell.NewHexColor(0x202020)
				if row.line >= 1 && row.line <= 4 {
					want = tcell.NewHexColor(0x161616)
				}
				if bg != want {
					t.Fatalf("line %d, cell %d:%d: background %v; want %v", row.line, x, index+1, bg, want)
				}
			}
		}
	}
	check() // Includes wrapped text, blank content, and hidden fence rows.
	a.doc.cursor = strings.Index(a.doc.text, "```bash")
	a.render()
	check() // Revealed source keeps the block background.
	a.doc.edit(0, len(a.doc.text), "plain")
	a.render()
	for x := 0; x < 8; x++ {
		_, _, style, _ := screen.GetContent(x, 1)
		_, bg, _ := style.Decompose()
		if bg != tcell.ColorDefault && bg != tcell.NewHexColor(0x202020) {
			t.Fatal("block background persisted after editing away the block")
		}
	}
}

func TestKeyboardEditingPasteAndUndo(t *testing.T) {
	a, _ := testApp(t, "- [ ] **task**\n", 80, 24)
	key(a, tcell.KeyEnd)
	typeText(a, " today")
	key(a, tcell.KeyEnter)
	typeText(a, "plain")
	if a.doc.text != "- [ ] **task** today\nplain\n" {
		t.Fatalf("typing changed formatting: %q", a.doc.text)
	}
	a.handle(tcell.NewEventPaste(true))
	typeText(a, " first")
	key(a, tcell.KeyEnter)
	typeText(a, "second")
	key(a, tcell.KeyCtrlX)
	a.handle(tcell.NewEventPaste(false))
	if a.quit || a.prompt != nil || !strings.Contains(a.doc.text, " first\nsecond\x18") {
		t.Fatalf("paste interpreted a shortcut: %q", a.doc.text)
	}
	alt(a, 'u')
	if a.doc.text != "- [ ] **task** today\nplain\n" {
		t.Fatalf("paste was not one undo: %q", a.doc.text)
	}
	alt(a, 'e')
	if !strings.Contains(a.doc.text, "second\x18") {
		t.Fatal("paste redo failed")
	}
}

func TestUndoRedoShortcutsAndHiddenAliases(t *testing.T) {
	a, screen := testApp(t, "original", 80, 24)
	typeText(a, "!")
	key(a, tcell.KeyCtrlU)
	if a.doc.text != "original" {
		t.Fatal("Ctrl+U did not undo")
	}
	key(a, tcell.KeyCtrlR)
	if a.doc.text != "!original" {
		t.Fatal("Ctrl+R did not redo")
	}
	alt(a, 'u')
	if a.doc.text != "original" {
		t.Fatal("nano undo alias stopped working")
	}
	alt(a, 'e')
	if a.doc.text != "!original" {
		t.Fatal("nano redo alias stopped working")
	}
	a.render()
	var footer strings.Builder
	for x := 0; x < 80; x++ {
		r, _, _, _ := screen.GetContent(x, 23)
		footer.WriteRune(r)
	}
	if !strings.Contains(footer.String(), "^U Undo") || !strings.Contains(footer.String(), "^R Redo") || strings.Contains(footer.String(), "M-U") || strings.Contains(HelpText, "Alt+U/E") {
		t.Fatalf("incorrect advertised shortcuts: %q", footer.String())
	}
}

func TestSearchReplaceAndCancellation(t *testing.T) {
	a, _ := testApp(t, "**cat** cat cat", 80, 24)
	key(a, tcell.KeyCtrlW)
	typeText(a, "cat")
	key(a, tcell.KeyEnter)
	if a.doc.cursor != 2 || a.matchEnd != 5 {
		t.Fatalf("source search: %d..%d", a.matchStart, a.matchEnd)
	}
	alt(a, 'w')
	if a.doc.cursor != 8 {
		t.Fatalf("next match: %d", a.doc.cursor)
	}
	alt(a, 'w')
	alt(a, 'w')
	if a.doc.cursor != 2 {
		t.Fatal("search did not wrap")
	}
	key(a, tcell.KeyCtrlBackslash)
	key(a, tcell.KeyEnter)
	typeText(a, "catcat")
	key(a, tcell.KeyEnter)
	typeText(a, "n")
	typeText(a, "a")
	if a.doc.text != "**cat** catcat catcat" || a.prompt != nil {
		t.Fatalf("replace-all: %q", a.doc.text)
	}
	alt(a, 'u')
	if a.doc.text != "**cat** cat cat" {
		t.Fatal("replace-all was not one undo")
	}
	key(a, tcell.KeyCtrlBackslash)
	key(a, tcell.KeyEnter)
	key(a, tcell.KeyCtrlU)
	key(a, tcell.KeyEnter)
	typeText(a, "y")
	key(a, tcell.KeyEsc)
	if a.doc.text != "**** cat cat" || a.prompt != nil {
		t.Fatalf("empty replacement/cancel: %q", a.doc.text)
	}
	key(a, tcell.KeyCtrlW)
	key(a, tcell.KeyCtrlU)
	typeText(a, "absent")
	key(a, tcell.KeyEnter)
	if a.message != "Text not found" || a.matchStart != -1 {
		t.Fatal("missing search did not report correctly")
	}
}

func TestSaveExitAndDiscard(t *testing.T) {
	a, _ := testApp(t, "original", 80, 24)
	typeText(a, "new ")
	key(a, tcell.KeyCtrlX)
	key(a, tcell.KeyEsc)
	if a.quit || a.prompt != nil {
		t.Fatal("exit cancellation failed")
	}
	key(a, tcell.KeyCtrlO)
	key(a, tcell.KeyEnter)
	assertFile(t, a.file.path, "new original")
	if a.file.modified(a.doc) {
		t.Fatal("save left document dirty")
	}
	typeText(a, "unsaved ")
	key(a, tcell.KeyCtrlX)
	typeText(a, "n")
	if !a.quit {
		t.Fatal("discard did not exit")
	}
	assertFile(t, a.file.path, "new original")

	b, _ := testApp(t, "original", 80, 24)
	typeText(b, "save ")
	key(b, tcell.KeyCtrlX)
	typeText(b, "y")
	key(b, tcell.KeyEnter)
	if !b.quit {
		t.Fatal("save-and-exit did not exit")
	}
	assertFile(t, b.file.path, "save original")
}

func TestSaveFailureKeepsEditorOpenAndSaveAsRecovers(t *testing.T) {
	a, _ := testApp(t, "original", 80, 24)
	typeText(a, "mine ")
	writeTestFile(t, a.file.path, "external")
	key(a, tcell.KeyCtrlX)
	typeText(a, "y")
	key(a, tcell.KeyEnter)
	if a.quit || !strings.Contains(a.message, "changed on disk") || a.doc.text != "mine original" {
		t.Fatal("conflict lost editor state")
	}
	assertFile(t, a.file.path, "external")
	copy := filepath.Join(filepath.Dir(a.file.path), "copy.md")
	key(a, tcell.KeyCtrlO)
	key(a, tcell.KeyCtrlU)
	typeText(a, copy)
	key(a, tcell.KeyEnter)
	assertFile(t, copy, "mine original")
	key(a, tcell.KeyCtrlX)
	if !a.quit {
		t.Fatal("clean Save As document did not exit")
	}
}

func TestNavigationRenderingAndResize(t *testing.T) {
	a, screen := testApp(t, "# Title\n- [ ] **task**\nlast", 30, 8)
	key(a, tcell.KeyDown)
	if lineAt(a.projection.lines, a.doc.cursor) != 1 {
		t.Fatal("down did not navigate to task")
	}
	a.render()
	_, _, style, _ := screen.GetContent(0, 1)
	_, _, attrs := style.Decompose()
	if attrs&tcell.AttrBold == 0 {
		t.Fatal("heading was not styled")
	}
	key(a, tcell.KeyCtrlG)
	a.render()
	key(a, tcell.KeyEsc)
	if a.help {
		t.Fatal("help did not close")
	}
	for _, size := range [][2]int{{8, 5}, {1, 1}, {0, 0}, {80, 24}} {
		screen.SetSize(size[0], size[1])
		a.handle(tcell.NewEventResize(size[0], size[1]))
		a.render()
		if size[0] > 0 && size[1] >= 4 {
			x, y, visible := screen.GetCursor()
			if !visible || x < 0 || x >= size[0] || y < 0 || y >= size[1] {
				t.Fatalf("cursor after resize %v: %d:%d visible=%v", size, x, y, visible)
			}
		}
	}
	before := a.doc.text
	key(a, tcell.KeyPgDn)
	key(a, tcell.KeyPgUp)
	key(a, tcell.KeyHome)
	key(a, tcell.KeyEnd)
	if a.doc.text != before {
		t.Fatal("navigation changed source")
	}
}

func TestPromptGraphemeEditingAndPaste(t *testing.T) {
	a, _ := testApp(t, "original", 10, 5)
	key(a, tcell.KeyCtrlW)
	typeText(a, "e\u0301界")
	key(a, tcell.KeyLeft)
	key(a, tcell.KeyBackspace2)
	if a.prompt.value != "界" || a.prompt.cursor != 0 {
		t.Fatalf("prompt split a grapheme: %#v", a.prompt)
	}
	a.handle(tcell.NewEventPaste(true))
	typeText(a, "text")
	key(a, tcell.KeyEnter)
	typeText(a, "more")
	a.handle(tcell.NewEventPaste(false))
	if a.prompt.value != "text more界" {
		t.Fatalf("prompt paste: %q", a.prompt.value)
	}
	a.render()
	key(a, tcell.KeyCtrlC)
	if a.prompt != nil || a.doc.text != "original" {
		t.Fatal("prompt cancellation changed source")
	}
}

func TestEditorRejectsNonTerminalWithoutCreatingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.md")
	if err := Run(path); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("nonterminal: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("nonterminal launch created a file")
	}
}
