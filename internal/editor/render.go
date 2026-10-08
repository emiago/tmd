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
	tableStarts := make(map[int]int, len(p.tableLines))
	for index := range p.tableLines {
		tableStarts[p.lines[index].start] = index
	}
	var result strings.Builder
	var current format
	write := func(value string, f format) {
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
					if f&heading1 != 0 {
						codes = append(codes, "38;2;255;223;93")
					} else {
						codes = append(codes, "33")
					}
				}
				if f&codeBlock != 0 {
					codes = append(codes, "48;2;22;22;22")
				}
				result.WriteString("\x1b[" + strings.Join(codes, ";") + "m")
			}
			current = f
		}
		result.WriteString(value)
	}
	for pos := 0; pos < len(source); {
		if lineIndex, ok := tableStarts[pos]; ok {
			for _, cell := range p.tableLines[lineIndex] {
				write(cell.text, cell.format)
			}
			pos = p.lines[lineIndex].end
			continue
		}
		if p.hidden[pos] {
			pos++
			continue
		}
		f := p.formats[pos]
		if r, ok := p.replacements[pos]; ok {
			write(r.text, f)
			pos = r.end
			continue
		}
		r, size := utf8.DecodeRuneInString(source[pos:])
		if r == '\n' || r == '\t' || (r == '\r' && pos+size < len(source) && source[pos+size] == '\n') {
			write(string(r), f)
		} else {
			write(safeText(source[pos:pos+size]), f)
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
