package editor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// saveOptions lets persistence tests inject the interoperable queue-write lock.
type saveOptions struct {
	QueueLock func(path string) (unlock func(), err error)
}

type fileState struct {
	path string
	data []byte
	info os.FileInfo
}

func markdownPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdx":
		return true
	}
	return false
}

func readFile(path string) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("Markdown file must be a regular file, not a directory or symlink")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return nil, nil, errors.New("Markdown file must contain UTF-8 text without binary data")
	}
	return data, info, nil
}

func openFile(path string) (*fileState, *document, error) {
	if !markdownPath(path) {
		return nil, nil, errors.New("editor requires a .md, .markdown, or .mdx file")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	data, info, err := readFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	if os.IsNotExist(err) {
		parent, parentErr := os.Stat(filepath.Dir(path))
		if parentErr != nil {
			return nil, nil, parentErr
		}
		if !parent.IsDir() {
			return nil, nil, errors.New("parent path is not a directory")
		}
	}
	return &fileState{path: path, data: data, info: info}, newDocument(data), nil
}

func (f *fileState) modified(d *document) bool { return d.text != string(f.data) }

func (f *fileState) save(path string, d *document, options saveOptions) error {
	if !utf8.ValidString(d.text) || strings.IndexByte(d.text, 0) >= 0 {
		return errors.New("buffer must contain UTF-8 text without binary data")
	}
	if !markdownPath(path) {
		return errors.New("save requires a .md, .markdown, or .mdx path")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if strings.EqualFold(filepath.Base(path), "QUEUE.md") {
		if options.QueueLock == nil {
			return errors.New("queue save requires the shared queue-write lock")
		}
		unlock, err := options.QueueLock(path)
		if err != nil {
			return err
		}
		defer unlock()
	}

	var expected *fileState
	if path == f.path {
		expected = f
	} else {
		expected = &fileState{path: path}
	}
	check := func() error {
		latest, info, err := readFile(path)
		if expected.info == nil {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			return errors.New("destination already exists; choose another Save As path")
		}
		if err != nil {
			return fmt.Errorf("file changed on disk; buffer retained: %w", err)
		}
		if !os.SameFile(expected.info, info) || !bytes.Equal(expected.data, latest) || expected.info.Mode().Perm() != info.Mode().Perm() {
			return errors.New("file changed on disk; buffer retained; use Save As to keep your edits")
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".tmd-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	mode := os.FileMode(0600)
	if expected.info != nil {
		mode = expected.info.Mode().Perm()
	}
	err = temp.Chmod(mode)
	if err == nil {
		_, err = temp.WriteString(d.text)
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := check(); err != nil {
		return err
	}
	// Linking installs a new file without overwriting an unexpected creation.
	if expected.info == nil {
		err = os.Link(temp.Name(), path)
	} else {
		err = os.Rename(temp.Name(), path)
	}
	if err != nil {
		return err
	}
	f.path, f.data, f.info = path, []byte(d.text), nil
	f.info, err = os.Lstat(path)
	return err
}
