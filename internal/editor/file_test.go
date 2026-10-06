package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0640); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("file %s: %q, %v; want %q", path, data, err, want)
	}
}

func TestSavePreservesPermissionsAndBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	original := "# title\r\n- [ ] **work**\r\nlast"
	writeTestFile(t, path, original)
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	f, d, err := openFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d.cursor = strings.Index(d.text, "work") + 4
	d.insert(" today")
	if err := f.save(path, d); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, strings.Replace(original, "work", "work today", 1))
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("permissions: %v %v", info, err)
	}
	if f.modified(d) {
		t.Fatal("successful save left document dirty")
	}
	d.insert("!")
	if err := f.save(path, d); err != nil {
		t.Fatalf("second save: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestSaveConflictsKeepBufferAndAllowSaveAs(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed contents", true: "replaced identity"}[replace], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "notes.md")
			writeTestFile(t, path, "original")
			f, d, err := openFile(path)
			if err != nil {
				t.Fatal(err)
			}
			d.insert("my edits ")
			latest := "external edit"
			if replace {
				latest = "original"
				other := filepath.Join(dir, "other.md")
				writeTestFile(t, other, latest)
				if err := os.Rename(other, path); err != nil {
					t.Fatal(err)
				}
			} else {
				writeTestFile(t, path, latest)
			}
			if err := f.save(path, d); err == nil || !strings.Contains(err.Error(), "changed on disk") {
				t.Fatalf("expected conflict: %v", err)
			}
			assertFile(t, path, latest)
			if d.text != "my edits original" || !f.modified(d) {
				t.Fatal("conflict lost editor changes")
			}
			copy := filepath.Join(dir, "copy.md")
			if err := f.save(copy, d); err != nil {
				t.Fatal(err)
			}
			assertFile(t, copy, "my edits original")
			if f.path != copy || f.modified(d) {
				t.Fatal("Save As did not update file identity")
			}
		})
	}
}

func TestMissingFilesAndSaveFailures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.markdown")
	f, d, err := openFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("opening created a file")
	}
	d.insert("hello")
	writeTestFile(t, path, "created externally")
	if err := f.save(path, d); err == nil {
		t.Fatal("overwrote unexpected creation")
	}
	assertFile(t, path, "created externally")
	if err := f.save(filepath.Join(dir, "missing", "copy.md"), d); err == nil {
		t.Fatal("expected missing-directory error")
	}
	if d.text != "hello" {
		t.Fatal("write failure lost buffer")
	}
	copy := filepath.Join(dir, "copy.MD")
	if err := f.save(copy, d); err != nil {
		t.Fatal(err)
	}
	assertFile(t, copy, "hello")
	info, _ := os.Stat(copy)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("new file mode: %o", info.Mode().Perm())
	}
	if err := f.save(path, d); err == nil {
		t.Fatal("Save As overwrote existing destination")
	}
}

func TestSaveRejectsExternalPermissionChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	writeTestFile(t, path, "original")
	f, d, err := openFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d.insert("mine ")
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.save(path, d); err == nil || !strings.Contains(err.Error(), "changed on disk") {
		t.Fatalf("expected permission conflict: %v", err)
	}
	assertFile(t, path, "original")
	if d.text != "mine original" {
		t.Fatal("save conflict lost local edits")
	}
}

func TestRejectedFileTypes(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, text string }{{"binary.md", "a\x00b"}, {"invalid.md", "\xff"}, {"plain.txt", "text"}} {
		path := filepath.Join(dir, tc.name)
		writeTestFile(t, path, tc.text)
		if _, _, err := openFile(path); err == nil {
			t.Errorf("accepted %s", tc.name)
		}
	}
	directory := filepath.Join(dir, "directory.md")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openFile(directory); err == nil {
		t.Fatal("accepted directory")
	}
	target := filepath.Join(dir, "target.md")
	writeTestFile(t, target, "target")
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openFile(link); err == nil {
		t.Fatal("accepted symlink")
	}
	f, d, _ := openFile(target)
	d.insert("new ")
	if err := f.save(link, d); err == nil {
		t.Fatal("saved over symlink")
	}
	assertFile(t, target, "target")
}
