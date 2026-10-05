package editor

import (
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

// Render prints the editor's Markdown projection without an active source line.
// Fenced blocks omit their fences and use a single style for literal contents.
func Render(out io.Writer, source string, styled bool) error {
	if !utf8.ValidString(source) || strings.IndexByte(source, 0) >= 0 {
		return errors.New("input must contain UTF-8 text without binary data")
	}
	p := project(source)
	var result strings.Builder
	var current format
	for pos := 0; pos < len(source); {
		if p.hidden[pos] {
			pos++
			continue
		}
		f := p.formats[pos]
		if styled && f != current {
			if current != 0 {
				result.WriteString("\x1b[0m")
			}
			if f != 0 {
				var codes []string
				if f&bold != 0 {
					codes = append(codes, "1")
				}
				if f&italic != 0 {
					codes = append(codes, "3")
				}
				if f&code != 0 {
					codes = append(codes, "38;2;229;167;107")
				}
				if f&heading != 0 {
					codes = append(codes, "33")
				}
				if f&codeBlock != 0 {
					codes = append(codes, "48;2;22;22;22")
				}
				result.WriteString("\x1b[" + strings.Join(codes, ";") + "m")
			}
			current = f
		}
		if r, ok := p.replacements[pos]; ok {
			result.WriteString(r.text)
			pos = r.end
			continue
		}
		r, size := utf8.DecodeRuneInString(source[pos:])
		if r == '\n' || r == '\t' || (r == '\r' && pos+size < len(source) && source[pos+size] == '\n') {
			result.WriteRune(r)
		} else {
			result.WriteString(safeText(source[pos : pos+size]))
		}
		pos += size
	}
	if styled && current != 0 {
		result.WriteString("\x1b[0m")
	}
	n, err := io.WriteString(out, result.String())
	if err == nil && n != result.Len() {
		return io.ErrShortWrite
	}
	return err
}
