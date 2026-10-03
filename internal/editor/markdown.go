package editor

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

type format uint8

const (
	bold format = 1 << iota
	italic
	code
	heading
)

type replacement struct {
	end  int
	text string
}
type projection struct {
	hidden       []bool
	formats      []format
	replacements map[int]replacement
	lines        []sourceLine
}

var markdown = goldmark.New(goldmark.WithExtensions(extension.TaskList))

// project annotates source ranges instead of rendering HTML and attempting to
// reconstruct Markdown. Unsupported syntax and fenced examples stay literal.
func project(source string) *projection {
	data := []byte(source)
	p := &projection{hidden: make([]bool, len(data)), formats: make([]format, len(data)), replacements: make(map[int]replacement), lines: sourceLines(source)}
	hide := func(a, b int) {
		for i := max(0, a); i < min(len(data), b); i++ {
			p.hidden[i] = true
		}
	}
	style := func(a, b int, f format) {
		for i := max(0, a); i < min(len(data), b); i++ {
			p.formats[i] |= f
		}
	}
	// Child spans include their own delimiters, allowing nested emphasis to
	// identify the enclosing delimiters without consuming unrelated punctuation.
	var visit func(ast.Node) (int, int)
	visit = func(n ast.Node) (int, int) {
		if t, ok := n.(*ast.Text); ok {
			return t.Segment.Start, t.Segment.Stop
		}
		switch n.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock, *ast.HTMLBlock, *ast.RawHTML:
			return -1, -1
		}
		a, b := -1, -1
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			start, end := visit(child)
			if start >= 0 {
				if a < 0 || start < a {
					a = start
				}
				b = max(b, end)
			}
		}
		switch node := n.(type) {
		case *ast.Emphasis:
			if a >= 0 {
				f := italic
				if node.Level == 2 {
					f = bold
				}
				style(a, b, f)
				size := node.Level
				if a >= size && b+size <= len(data) {
					left, right := data[a-size:a], data[b:b+size]
					if bytes.Equal(left, right) && (left[0] == '*' || left[0] == '_') {
						hide(a-size, a)
						hide(b, b+size)
						a -= size
						b += size
					}
				}
			}
		case *ast.CodeSpan:
			if a >= 0 {
				style(a, b, code)
				start, end := a, b
				// Goldmark trims a surrounding space from nonblank code spans.
				if start > 0 && data[start-1] == ' ' {
					start--
				}
				if end < len(data) && data[end] == ' ' {
					end++
				}
				left, right := start, end
				for left > 0 && data[left-1] == '`' {
					left--
				}
				for right < len(data) && data[right] == '`' {
					right++
				}
				if start-left > 0 && start-left == right-end {
					hide(left, a)
					hide(b, right)
					a, b = left, right
				}
			}
		case *ast.Heading:
			for i := 0; i < node.Lines().Len(); i++ {
				seg := node.Lines().At(i)
				line := p.lines[lineAt(p.lines, seg.Start)]
				style(line.start, line.end, bold|heading)
				prefix := source[line.start:seg.Start]
				if strings.HasPrefix(strings.TrimLeft(prefix, " \t"), "#") {
					hide(line.start, seg.Start)
					if strings.Trim(source[seg.Stop:line.end], "# \t\r\n") == "" {
						hide(seg.Stop, line.end)
					}
				}
			}
		case *ast.ListItem:
			first := node.FirstChild()
			if first != nil && first.Type() == ast.TypeBlock && first.Lines().Len() > 0 {
				seg := first.Lines().At(0)
				line := p.lines[lineAt(p.lines, seg.Start)]
				prefix := source[line.start:seg.Start]
				marker := strings.IndexAny(prefix, "-*+")
				if marker >= 0 && strings.TrimSpace(prefix[:marker]) == "" {
					p.replacements[line.start+marker] = replacement{line.start + marker + 1, "•"}
				}
				if seg.Start+3 <= len(data) && data[seg.Start] == '[' && data[seg.Start+2] == ']' {
					state := data[seg.Start+1]
					if state == ' ' || state == 'x' || state == 'X' {
						glyph := "☐"
						if state != ' ' {
							glyph = "☑"
						}
						p.replacements[seg.Start] = replacement{seg.Start + 3, glyph}
					}
				}
			}
		}
		return a, b
	}
	visit(markdown.Parser().Parse(text.NewReader(data)))
	return p
}
