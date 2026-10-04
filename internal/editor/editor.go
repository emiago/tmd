package editor

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
	"golang.org/x/term"
)

type prompt struct {
	kind, label, value string
	cursor             int
}

type app struct {
	screen               tcell.Screen
	file                 *fileState
	doc                  *document
	options              saveOptions
	projection           *projection
	projectedRevision    uint64
	layout               layout
	top                  int
	message              string
	prompt               *prompt
	help                 bool
	quit                 bool
	quitAfterSave        bool
	query, replacement   string
	matchStart, matchEnd int
	replaced             int
	pasting              bool
	paste                strings.Builder
	goalColumn           int
}

func newApp(screen tcell.Screen, f *fileState, d *document, options saveOptions) *app {
	screen.SetStyle(baseStyle)
	return &app{screen: screen, file: f, doc: d, options: options, matchStart: -1, matchEnd: -1, goalColumn: -1}
}

// Run opens one Markdown document in the terminal.
func Run(path string) error {
	options := saveOptions{QueueLock: lockQueue}
	f, d, err := openFile(path)
	if err != nil {
		return err
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) || os.Getenv("TERM") == "dumb" {
		return errors.New("tmd requires an interactive terminal on stdin and stdout")
	}
	screen, err := tcell.NewScreen()
	if err != nil {
		return fmt.Errorf("initialize terminal: %w", err)
	}
	if err = screen.Init(); err != nil {
		screen.Fini()
		return fmt.Errorf("initialize terminal: %w", err)
	}
	defer screen.Fini()
	screen.EnablePaste()
	// Wake PollEvent on handled process termination, then restore the terminal.
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	defer close(done)
	go func() {
		select {
		case sig := <-signals:
			_ = screen.PostEvent(tcell.NewEventInterrupt(sig))
		case <-done:
		}
	}()
	a := newApp(screen, f, d, options)
	for !a.quit {
		a.render()
		event := screen.PollEvent()
		if event == nil {
			return errors.New("terminal event stream closed")
		}
		switch e := event.(type) {
		case *tcell.EventError:
			return errors.New(e.Error())
		case *tcell.EventInterrupt:
			return fmt.Errorf("editor interrupted by %v", e.Data())
		default:
			a.handle(event)
		}
	}
	return nil
}

func (a *app) refreshLayout() {
	if a.projection == nil || a.projectedRevision != a.doc.revision {
		a.projection = project(a.doc.text)
		a.projectedRevision = a.doc.revision
	}
	w, _ := a.screen.Size()
	a.layout = makeLayout(a.doc, a.projection, w)
}

func (a *app) render() {
	a.refreshLayout()
	w, h := a.screen.Size()
	a.screen.Clear()
	if w < 1 || h < 1 {
		a.screen.HideCursor()
		a.screen.Show()
		return
	}
	height := max(1, h-3)
	a.top = max(0, min(a.top, max(0, len(a.layout.rows)-height)))
	if a.layout.cursorRow < a.top {
		a.top = a.layout.cursorRow
	}
	if a.layout.cursorRow >= a.top+height {
		a.top = a.layout.cursorRow - height + 1
	}
	bar := baseStyle.Reverse(true)
	line := a.projection.lines[lineAt(a.projection.lines, a.doc.cursor)]
	modified := ""
	if a.file.modified(a.doc) {
		modified = " *"
	}
	column := uniseg.GraphemeClusterCount(a.doc.text[line.start:min(a.doc.cursor, line.end)]) + 1
	position := fmt.Sprintf("%s  %d:%d", modified, lineAt(a.projection.lines, a.doc.cursor)+1, column)
	positionX := max(0, w-uniseg.StringWidth(position))
	drawText(a.screen, 0, 0, max(0, positionX-1), "tmd  "+filepath.Base(a.file.path), bar)
	drawText(a.screen, positionX, 0, w, position, bar)
	if a.help {
		for i, text := range strings.Split(HelpText, "\n") {
			if i+1 >= h-2 {
				break
			}
			drawText(a.screen, 0, i+1, w, text, baseStyle)
		}
		a.screen.HideCursor()
	} else {
		for r := a.top; r < len(a.layout.rows) && r-a.top < height && r-a.top+1 < h-2; r++ {
			if a.projection.codeLines[a.layout.rows[r].line] {
				for x := 0; x < w; x++ {
					a.screen.SetContent(x, r-a.top+1, ' ', nil, cellStyle(codeBlock))
				}
			}
			for _, cell := range a.layout.rows[r].cells {
				style := cellStyle(cell.format)
				if a.matchStart >= 0 && cell.start < a.matchEnd && cell.end > a.matchStart {
					style = style.Reverse(true)
				}
				drawCell(a.screen, cell.x, r-a.top+1, cell.text, style)
			}
		}
		if h >= 4 {
			a.screen.ShowCursor(a.layout.cursorX, a.layout.cursorRow-a.top+1)
		} else {
			a.screen.HideCursor()
		}
	}
	if h >= 2 {
		if a.prompt != nil {
			a.renderPrompt(w, h-2)
		} else {
			drawText(a.screen, 0, h-2, w, a.message, baseStyle)
		}
	}
	if h >= 3 {
		drawText(a.screen, 0, h-1, w, "^G Help  ^O Save  ^X Exit  ^W Find  ^\\ Replace  ^U Undo  ^R Redo", bar)
	}
	a.screen.Show()
}

func (a *app) renderPrompt(width, y int) {
	p := a.prompt
	labelWidth := min(uniseg.StringWidth(p.label), max(0, width/2))
	x := drawText(a.screen, 0, y, labelWidth, p.label, baseStyle.Bold(true))
	if p.kind == "exit" || p.kind == "confirm" {
		drawText(a.screen, x, y, width, p.value, baseStyle)
		a.screen.HideCursor()
		return
	}
	start := 0
	for start < p.cursor && x+uniseg.StringWidth(p.value[start:p.cursor]) >= width {
		start = next(p.value, start)
	}
	drawText(a.screen, x, y, width, p.value[start:], baseStyle)
	a.screen.ShowCursor(min(width-1, x+uniseg.StringWidth(p.value[start:p.cursor])), y)
}

func (a *app) beginPrompt(kind, label, value string) {
	a.prompt = &prompt{kind: kind, label: label, value: value, cursor: len(value)}
}

func (a *app) handle(event tcell.Event) {
	switch e := event.(type) {
	case *tcell.EventResize:
		a.screen.Sync()
	case *tcell.EventPaste:
		if e.Start() {
			a.pasting = true
			a.paste.Reset()
		}
		if e.End() && a.pasting {
			a.pasting = false
			text := a.paste.String()
			a.paste.Reset()
			if a.prompt != nil && a.prompt.kind != "exit" && a.prompt.kind != "confirm" {
				// Prompts are single-line; pasted line breaks become spaces.
				a.promptInsert(strings.Map(func(r rune) rune {
					if r == '\n' || r == '\r' {
						return ' '
					}
					if unicode.IsControl(r) {
						return -1
					}
					return r
				}, text))
			} else if a.prompt == nil && !a.help {
				a.doc.insert(text)
				a.clearMatch()
			}
		}
	case *tcell.EventKey:
		if a.pasting {
			switch e.Key() {
			case tcell.KeyRune:
				a.paste.WriteRune(e.Rune())
			case tcell.KeyEnter:
				a.paste.WriteByte('\n')
			case tcell.KeyTab:
				a.paste.WriteByte('\t')
			default:
				// Control bytes in a bracketed paste are text, never commands.
				if e.Key() >= tcell.KeyCtrlSpace && e.Key() <= tcell.KeyCtrlUnderscore {
					a.paste.WriteByte(byte(e.Key()))
				}
			}
			return
		}
		if a.help {
			a.help = false
			return
		}
		if a.prompt != nil {
			a.promptKey(e)
			return
		}
		a.editorKey(e)
	}
}

func (a *app) clearMatch() { a.matchStart, a.matchEnd = -1, -1; a.goalColumn = -1 }

func (a *app) editorKey(e *tcell.EventKey) {
	if e.Key() == tcell.KeyRune && e.Modifiers()&tcell.ModAlt != 0 {
		switch unicode.ToLower(e.Rune()) {
		case 'u':
			a.doc.undoEdit()
			a.clearMatch()
		case 'e':
			a.doc.redoEdit()
			a.clearMatch()
		case 'w':
			a.findNext()
		}
		return
	}
	move := true
	switch e.Key() {
	case tcell.KeyLeft:
		a.doc.cursor = previous(a.doc.text, a.doc.cursor)
	case tcell.KeyRight:
		a.doc.cursor = next(a.doc.text, a.doc.cursor)
	case tcell.KeyUp:
		a.moveVertical(-1)
		move = false
	case tcell.KeyDown:
		a.moveVertical(1)
		move = false
	case tcell.KeyPgUp:
		_, h := a.screen.Size()
		a.moveVertical(-max(1, h-3))
		move = false
	case tcell.KeyPgDn:
		_, h := a.screen.Size()
		a.moveVertical(max(1, h-3))
		move = false
	case tcell.KeyHome, tcell.KeyCtrlA:
		lines := sourceLines(a.doc.text)
		a.doc.cursor = lines[lineAt(lines, a.doc.cursor)].start
	case tcell.KeyEnd, tcell.KeyCtrlE:
		lines := sourceLines(a.doc.text)
		a.doc.cursor = lines[lineAt(lines, a.doc.cursor)].end
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		a.doc.backspace()
		a.clearMatch()
	case tcell.KeyDelete:
		a.doc.deleteForward()
		a.clearMatch()
	case tcell.KeyEnter:
		a.doc.insert("\n")
		a.clearMatch()
	case tcell.KeyTab:
		a.doc.insert("\t")
		a.clearMatch()
	case tcell.KeyRune:
		if e.Modifiers()&tcell.ModCtrl == 0 && !unicode.IsControl(e.Rune()) {
			a.doc.insert(string(e.Rune()))
			a.clearMatch()
		}
	case tcell.KeyCtrlO:
		a.quitAfterSave = false
		a.beginPrompt("save", "Save as: ", a.file.path)
	case tcell.KeyCtrlX:
		if a.file.modified(a.doc) {
			a.beginPrompt("exit", "Save changes? ", "[Y]es / [N]o / Esc cancel")
		} else {
			a.quit = true
		}
	case tcell.KeyCtrlW:
		a.beginPrompt("search", "Search: ", a.query)
	case tcell.KeyCtrlBackslash:
		a.beginPrompt("replace-search", "Replace text: ", a.query)
	case tcell.KeyCtrlG:
		a.help = true
	case tcell.KeyCtrlU:
		a.doc.undoEdit()
		a.clearMatch()
	case tcell.KeyCtrlR:
		a.doc.redoEdit()
		a.clearMatch()
	case tcell.KeyCtrlC:
		a.message = "Use Ctrl+X to exit."
	}
	if move {
		a.goalColumn = -1
	}
}

func (a *app) moveVertical(delta int) {
	a.refreshLayout()
	if a.goalColumn < 0 {
		a.goalColumn = a.layout.cursorX
	}
	row := max(0, min(len(a.layout.rows)-1, a.layout.cursorRow+delta))
	a.doc.cursor = nearest(a.layout.rows[row], a.goalColumn)
	a.doc.snapCursor()
}

func (a *app) promptInsert(text string) {
	p := a.prompt
	p.value = p.value[:p.cursor] + text + p.value[p.cursor:]
	p.cursor = snapPosition(p.value, p.cursor+len(text))
}

func (a *app) promptKey(e *tcell.EventKey) {
	p := a.prompt
	if e.Key() == tcell.KeyEsc || e.Key() == tcell.KeyCtrlC {
		a.prompt = nil
		a.quitAfterSave = false
		a.message = "Cancelled"
		return
	}
	if p.kind == "exit" {
		if e.Key() == tcell.KeyRune {
			switch unicode.ToLower(e.Rune()) {
			case 'y':
				a.quitAfterSave = true
				a.beginPrompt("save", "Save as: ", a.file.path)
			case 'n':
				a.quit = true
			}
		}
		return
	}
	if p.kind == "confirm" {
		if e.Key() == tcell.KeyRune {
			a.confirmReplacement(unicode.ToLower(e.Rune()))
		}
		return
	}
	switch e.Key() {
	case tcell.KeyRune:
		if e.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) == 0 && !unicode.IsControl(e.Rune()) {
			a.promptInsert(string(e.Rune()))
		}
	case tcell.KeyLeft:
		p.cursor = previous(p.value, p.cursor)
	case tcell.KeyRight:
		p.cursor = next(p.value, p.cursor)
	case tcell.KeyHome, tcell.KeyCtrlA:
		p.cursor = 0
	case tcell.KeyEnd, tcell.KeyCtrlE:
		p.cursor = len(p.value)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		start := previous(p.value, p.cursor)
		p.value = p.value[:start] + p.value[p.cursor:]
		p.cursor = start
	case tcell.KeyDelete:
		end := next(p.value, p.cursor)
		p.value = p.value[:p.cursor] + p.value[end:]
	case tcell.KeyCtrlU:
		p.value = ""
		p.cursor = 0
	case tcell.KeyEnter:
		a.acceptPrompt(*p)
	}
}

func (a *app) acceptPrompt(p prompt) {
	a.prompt = nil
	switch p.kind {
	case "save":
		if p.value == "" {
			a.message = "Save path cannot be empty"
			a.quitAfterSave = false
			return
		}
		if err := a.file.save(p.value, a.doc, a.options); err != nil {
			a.message = err.Error()
			a.quitAfterSave = false
			return
		}
		a.message = "Saved " + a.file.path
		if a.quitAfterSave {
			a.quit = true
		}
	case "search":
		if p.value == "" {
			a.message = "Search text cannot be empty"
			return
		}
		a.query = p.value
		a.search(a.doc.cursor)
	case "replace-search":
		if p.value == "" {
			a.message = "Search text cannot be empty"
			return
		}
		a.query = p.value
		a.beginPrompt("replacement", "Replace with: ", a.replacement)
	case "replacement":
		a.replacement = p.value
		a.replaced = 0
		// Replacement visits the document once from the beginning, so an
		// inserted occurrence can never become another replacement candidate.
		a.nextReplacement(0)
	}
}

func (a *app) search(from int) {
	a.matchStart, a.matchEnd = find(a.doc.text, a.query, from)
	if a.matchStart < 0 {
		a.message = "Text not found"
		return
	}
	a.doc.cursor = a.matchStart
	a.doc.snapCursor()
	a.goalColumn = -1
	a.message = "Match found (Alt+W for next)"
}

func (a *app) findNext() {
	if a.query == "" {
		a.beginPrompt("search", "Search: ", "")
		return
	}
	from := next(a.doc.text, a.doc.cursor)
	if a.matchStart >= 0 {
		from = a.matchEnd
	}
	a.search(from)
}

func (a *app) nextReplacement(from int) {
	if from <= len(a.doc.text) {
		if i := strings.Index(a.doc.text[from:], a.query); i >= 0 {
			a.matchStart = from + i
			a.matchEnd = a.matchStart + len(a.query)
			a.doc.cursor = a.matchStart
			a.doc.snapCursor()
			a.beginPrompt("confirm", "Replace match? ", "[Y]es / [N]o / [A]ll / Esc cancel")
			return
		}
	}
	a.prompt = nil
	a.clearMatch()
	a.message = fmt.Sprintf("Replaced %d occurrence(s)", a.replaced)
}

func (a *app) confirmReplacement(answer rune) {
	start, end := a.matchStart, a.matchEnd
	switch answer {
	case 'y':
		a.doc.edit(start, end, a.replacement)
		a.replaced++
		a.nextReplacement(start + len(a.replacement))
	case 'n':
		a.nextReplacement(end)
	case 'a':
		// Replace all remaining matches as one undoable edit.
		count := a.doc.replaceAll(a.query, a.replacement, start)
		a.replaced += count
		a.nextReplacement(len(a.doc.text) + 1)
	}
}

// HelpText is shared by terminal help and the CLI's edit help.
const HelpText = `Markdown is rendered; the current line exposes its source.
Type, delete, or paste plain text. No automatic formatting.

Arrows          Move cursor       Home/End   Line start/end
Page Up/Down    Move by a page     Enter      New line
Ctrl+O          Save / Save As     Ctrl+X     Exit
Ctrl+W          Literal search    Alt+W      Find next
Ctrl+\          Find and replace  Ctrl+U/R   Undo / redo
Esc / Ctrl+C    Cancel a prompt   Ctrl+U     Clear prompt

Search is case-sensitive and wraps through the source.
Replace offers yes, no, all, or cancel; All is undoable.
Checkboxes are display-only: edit [ ] / [x] by typing.
Save As requires a new destination; existing files are preserved.
Press any key to return to editing.`
