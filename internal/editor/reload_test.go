package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func poll(a *app) { a.handle(tcell.NewEventInterrupt(filePoll{})) }

func TestCleanExternalReload(t *testing.T) {
	a, _ := testApp(t, "# old\r\nabcdef\r\nlast", 80, 24)
	// A clean buffer can still have undo and redo history from saved edits.
	a.doc.insert("!")
	a.doc.undoEdit()
	a.doc.cursor = strings.Index(a.doc.text, "abcdef") + 3
	a.matchStart, a.matchEnd = 2, 5
	a.goalColumn = 20
	latest := "# new\r\na👩‍💻éz\r\nlast"
	// External editors commonly replace the file atomically.
	other := filepath.Join(filepath.Dir(a.file.path), "replacement.md")
	writeTestFile(t, other, latest)
	if err := os.Rename(other, a.file.path); err != nil {
		t.Fatal(err)
	}
	poll(a)
	a.render()
	wantCursor := strings.Index(latest, "z")
	if a.doc.text != latest || a.doc.cursor != wantCursor || a.doc.newline != "\r\n" {
		t.Fatalf("reload: text %q, cursor %d; want %d", a.doc.text, a.doc.cursor, wantCursor)
	}
	if a.file.modified(a.doc) || len(a.doc.undo) != 0 || len(a.doc.redo) != 0 || a.matchStart != -1 || a.goalColumn != -1 {
		t.Fatal("reload retained stale document state")
	}
	if a.projection == nil || a.projection.lines[0].end != len("# new") {
		t.Fatal("reload did not refresh rendering")
	}
	if !strings.HasPrefix(a.message, "Reloaded ") {
		t.Fatal("reload was not reported")
	}
	a.doc.insert("!")
	if err := a.file.save(a.file.path, a.doc); err != nil {
		t.Fatalf("reloaded baseline cannot save: %v", err)
	}
}

func TestExternalReloadClampsCursor(t *testing.T) {
	a, _ := testApp(t, "first\nsecond\nthird", 80, 24)
	a.doc.cursor = len(a.doc.text)
	writeTestFile(t, a.file.path, "x")
	poll(a)
	if a.doc.cursor != 1 {
		t.Fatalf("cursor not clamped: %d", a.doc.cursor)
	}
	writeTestFile(t, a.file.path, "")
	poll(a)
	if a.doc.cursor != 0 || a.doc.text != "" {
		t.Fatal("empty reload failed")
	}
}

func TestDirtyExternalCancel(t *testing.T) {
	a, _ := testApp(t, "original", 80, 24)
	typeText(a, "mine ")
	writeTestFile(t, a.file.path, "external")
	poll(a)
	if a.prompt == nil || a.prompt.kind != "external" || a.doc.text != "mine original" {
		t.Fatal("external edit did not retain local edits and prompt")
	}
	a.render()
	// Paste must not edit a choice prompt or resolve it accidentally.
	a.handle(tcell.NewEventPaste(true))
	typeText(a, "r")
	a.handle(tcell.NewEventPaste(false))
	if a.prompt.kind != "external" || a.doc.text != "mine original" {
		t.Fatal("paste activated conflict resolution")
	}
	key(a, tcell.KeyEsc)
	poll(a)
	if a.prompt != nil || a.doc.text != "mine original" {
		t.Fatal("cancel caused another prompt or lost edits")
	}
	if err := a.file.save(a.file.path, a.doc); err == nil {
		t.Fatal("cancel allowed overwriting external edits")
	}
	writeTestFile(t, a.file.path, "another external edit")
	poll(a)
	if a.prompt == nil || a.prompt.kind != "external" {
		t.Fatal("new disk version did not prompt again")
	}
	key(a, tcell.KeyCtrlC)
	// Undoing all local edits makes the next check a clean reload.
	for len(a.doc.undo) > 0 {
		a.doc.undoEdit()
	}
	poll(a)
	if a.doc.text != "another external edit" || a.prompt != nil {
		t.Fatal("clean buffer did not reload after cancelling")
	}
}

func TestDirtyExternalReloadReadsLatest(t *testing.T) {
	a, _ := testApp(t, "original", 80, 24)
	typeText(a, "mine ")
	writeTestFile(t, a.file.path, "external")
	poll(a)
	writeTestFile(t, a.file.path, "newer external")
	typeText(a, "r")
	if a.prompt != nil || a.doc.text != "newer external" || a.file.modified(a.doc) || len(a.doc.undo) != 0 {
		t.Fatal("reload did not discard edits and load the latest version")
	}
}

func TestDirtyExternalReloadFailureRetainsEdits(t *testing.T) {
	a, _ := testApp(t, "original", 80, 24)
	typeText(a, "mine ")
	writeTestFile(t, a.file.path, "external")
	poll(a)
	if err := os.Remove(a.file.path); err != nil {
		t.Fatal(err)
	}
	typeText(a, "r")
	if a.doc.text != "mine original" || !strings.Contains(a.message, "buffer retained") || !a.file.modified(a.doc) {
		t.Fatal("failed reload discarded unsaved edits")
	}
}

func TestUnchangedTextPreservesDocument(t *testing.T) {
	a, _ := testApp(t, "original", 80, 24)
	typeText(a, "mine ")
	doc := a.doc
	other := filepath.Join(filepath.Dir(a.file.path), "replacement.md")
	writeTestFile(t, other, "original")
	if err := os.Chmod(other, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, a.file.path); err != nil {
		t.Fatal(err)
	}
	poll(a)
	if a.doc != doc || a.prompt != nil || a.doc.text != "mine original" {
		t.Fatal("metadata-only change disturbed local edits")
	}
	if err := a.file.save(a.file.path, a.doc); err != nil {
		t.Fatalf("save after metadata refresh: %v", err)
	}
	poll(a)
	if a.doc != doc || len(a.doc.undo) == 0 || a.file.info.Mode().Perm() != 0600 {
		t.Fatal("own save reset the document or lost refreshed permissions")
	}
}

func TestExternalSaveAs(t *testing.T) {
	a, _ := testApp(t, "original", 80, 24)
	original := a.file.path
	typeText(a, "mine ")
	writeTestFile(t, original, "external")
	poll(a)
	typeText(a, "s")
	if a.prompt == nil || a.prompt.kind != "external-save" || a.prompt.value != "" {
		t.Fatal("Save As did not open an empty destination prompt")
	}
	typeText(a, original)
	key(a, tcell.KeyEnter)
	assertFile(t, original, "external")
	if !strings.Contains(a.message, "new Save As destination") || a.doc.text != "mine original" {
		t.Fatal("Save As accepted the original path")
	}
	copy := filepath.Join(filepath.Dir(original), "copy.md")
	key(a, tcell.KeyCtrlO)
	key(a, tcell.KeyCtrlU)
	typeText(a, copy)
	key(a, tcell.KeyEnter)
	assertFile(t, copy, "mine original")
	assertFile(t, original, "external")
	if a.file.path != copy || a.file.modified(a.doc) {
		t.Fatal("Save As did not switch the active file")
	}
	writeTestFile(t, original, "ignored old path")
	poll(a)
	if a.doc.text != "mine original" {
		t.Fatal("poll followed the old file after Save As")
	}
	writeTestFile(t, copy, "new active file")
	poll(a)
	if a.doc.text != "new active file" {
		t.Fatal("poll did not follow Save As destination")
	}
}

func TestExternalSaveAsDirectChoice(t *testing.T) {
	a, _ := testApp(t, "original", 80, 24)
	typeText(a, "mine ")
	writeTestFile(t, a.file.path, "external")
	poll(a)
	typeText(a, "s")
	copy := filepath.Join(filepath.Dir(a.file.path), "copy.md")
	typeText(a, copy)
	key(a, tcell.KeyEnter)
	assertFile(t, copy, "mine original")
	if a.file.path != copy || a.file.modified(a.doc) {
		t.Fatal("conflict Save As failed")
	}
}

func TestExternalCheckDeferred(t *testing.T) {
	t.Run("prompt", func(t *testing.T) {
		a, _ := testApp(t, "original", 80, 24)
		key(a, tcell.KeyCtrlW)
		typeText(a, "ori")
		writeTestFile(t, a.file.path, "external")
		poll(a)
		if a.prompt.kind != "search" || a.prompt.value != "ori" || a.doc.text != "original" {
			t.Fatal("external check interrupted the search prompt")
		}
		key(a, tcell.KeyEsc)
		poll(a)
		if a.doc.text != "external" {
			t.Fatal("deferred reload did not run")
		}
	})
	t.Run("paste", func(t *testing.T) {
		a, _ := testApp(t, "original", 80, 24)
		a.handle(tcell.NewEventPaste(true))
		typeText(a, "pasted ")
		writeTestFile(t, a.file.path, "external")
		poll(a)
		if a.doc.text != "original" || a.prompt != nil {
			t.Fatal("external check interrupted paste")
		}
		a.handle(tcell.NewEventPaste(false))
		poll(a)
		if a.doc.text != "pasted original" || a.prompt == nil || a.prompt.kind != "external" {
			t.Fatal("paste was lost or dirty conflict was skipped")
		}
	})
}

func TestInvalidExternalChangesRetainBuffer(t *testing.T) {
	for _, kind := range []string{"deleted", "binary", "invalid UTF-8", "symlink", "directory", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			a, _ := testApp(t, "original", 80, 24)
			switch kind {
			case "deleted", "symlink", "directory":
				if err := os.Remove(a.file.path); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					target := filepath.Join(filepath.Dir(a.file.path), "target.md")
					writeTestFile(t, target, "target")
					if err := os.Symlink(target, a.file.path); err != nil {
						t.Fatal(err)
					}
				} else if kind == "directory" {
					if err := os.Mkdir(a.file.path, 0700); err != nil {
						t.Fatal(err)
					}
				}
			case "binary":
				writeTestFile(t, a.file.path, "a\x00b")
			case "invalid UTF-8":
				writeTestFile(t, a.file.path, "\xff")
			case "unreadable":
				if os.Geteuid() == 0 {
					t.Skip("root can read files regardless of permissions")
				}
				if err := os.Chmod(a.file.path, 0000); err != nil {
					t.Fatal(err)
				}
			}
			poll(a)
			if a.doc.text != "original" || a.prompt != nil || !strings.Contains(a.message, "Cannot reload; buffer retained") {
				t.Fatal("invalid disk change lost buffer or was not reported")
			}
			if err := a.file.save(a.file.path, a.doc); err == nil {
				t.Fatal("save overwrote invalid disk change")
			}
		})
	}
}

func TestInitiallyMissingFileReload(t *testing.T) {
	a, _ := testApp(t, "", 80, 24)
	if err := os.Remove(a.file.path); err != nil {
		t.Fatal(err)
	}
	f, d, err := openFile(a.file.path)
	if err != nil {
		t.Fatal(err)
	}
	a.file, a.doc, a.message = f, d, ""
	poll(a)
	if a.message != "" {
		t.Fatal("initially missing file produced a reload error")
	}
	writeTestFile(t, a.file.path, "created externally")
	poll(a)
	if a.doc.text != "created externally" || a.file.modified(a.doc) {
		t.Fatal("new external file did not reload")
	}
}

func TestPollFileEventsStops(t *testing.T) {
	a, screen := testApp(t, "original", 80, 24)
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		pollFileEvents(screen, done)
		close(stopped)
	}()
	t.Cleanup(func() {
		close(done)
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Error("polling did not stop on exit")
		}
	})
	events := make(chan tcell.Event, 1)
	go func() { events <- screen.PollEvent() }()
	select {
	case event := <-events:
		interrupt, ok := event.(*tcell.EventInterrupt)
		if !ok {
			t.Fatalf("unexpected polling event: %T", event)
		}
		if _, ok := interrupt.Data().(filePoll); !ok {
			t.Fatal("poll used a termination interrupt")
		}
		a.handle(event)
	case <-time.After(3 * time.Second):
		t.Fatal("poll did not wake the idle terminal")
	}
}
