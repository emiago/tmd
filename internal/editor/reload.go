package editor

import (
	"bytes"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

type filePoll struct{}

// Only the event loop reads files or modifies editor state.
func pollFileEvents(screen tcell.Screen, done <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			_ = screen.PostEvent(tcell.NewEventInterrupt(filePoll{}))
		}
	}
}

func (p *prompt) choice() bool {
	return p.kind == "exit" || p.kind == "confirm" || p.kind == "external"
}

func sameSnapshot(a, b *fileState) bool {
	return a != nil && b != nil && a.path == b.path && a.info != nil && b.info != nil &&
		os.SameFile(a.info, b.info) && a.info.Mode().Perm() == b.info.Mode().Perm() && bytes.Equal(a.data, b.data)
}

func (a *app) checkExternalChange() {
	if a.prompt != nil || a.pasting || a.help {
		return
	}
	data, info, err := readFile(a.file.path)
	if err != nil {
		// A new, still-missing file is an ordinary empty buffer.
		if !os.IsNotExist(err) || a.file.info != nil {
			a.message = "Cannot reload; buffer retained: " + err.Error()
		}
		return
	}
	latest := &fileState{path: a.file.path, data: data, info: info}
	if bytes.Equal(data, a.file.data) {
		// Identity and permissions may change without a text edit.
		a.file.info = info
		a.ignoredChange = nil
		return
	}
	if !a.file.modified(a.doc) {
		a.reload(latest)
		return
	}
	if sameSnapshot(latest, a.ignoredChange) {
		return
	}
	a.ignoredChange = latest
	a.quitAfterSave = false
	a.beginPrompt("external", "Disk changed: ", "[S]ave As / [R]eload (discard edits) / Esc cancel")
}

func (a *app) reloadFromDisk() {
	// Read again: the file may have changed while the prompt was open.
	data, info, err := readFile(a.file.path)
	if err != nil {
		a.message = "Cannot reload; buffer retained: " + err.Error()
		return
	}
	a.reload(&fileState{path: a.file.path, data: data, info: info})
}

func (a *app) reload(latest *fileState) {
	lines := sourceLines(a.doc.text)
	row := lineAt(lines, a.doc.cursor)
	column := uniseg.GraphemeClusterCount(a.doc.text[lines[row].start:min(a.doc.cursor, lines[row].end)])
	a.doc = newDocument(latest.data)
	lines = sourceLines(a.doc.text)
	line := lines[min(row, len(lines)-1)]
	a.doc.cursor = line.start
	for column > 0 && a.doc.cursor < line.end {
		a.doc.cursor = next(a.doc.text, a.doc.cursor)
		column--
	}
	a.file = latest
	a.ignoredChange = nil
	a.projection = nil
	a.clearMatch()
	a.quitAfterSave = false
	a.message = "Reloaded " + latest.path
}
