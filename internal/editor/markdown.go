package editor

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type format uint8

const (
	bold format = 1 << iota
	italic
	code
	heading
	codeBlock
	heading1
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
	codeLines    map[int]bool
	tableLines   map[int][]projectedCell
	tables       []*projectedTable
	layoutCache  *projectionLayout
}

// Goldmark's fenced nodes retain content spans but not the fences themselves.
// Record those offsets while parsing, including empty and unclosed blocks.
type fencedParser struct{ parser.BlockParser }

func (p fencedParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	_, segment := reader.PeekLine()
	node, state := p.BlockParser.Open(parent, reader, pc)
	if node != nil {
		node.SetAttributeString("openingFence", segment.Start)
	}
	return node, state
}

func (p fencedParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	_, segment := reader.PeekLine()
	state := p.BlockParser.Continue(node, reader, pc)
	if state == parser.Close {
		node.SetAttributeString("closingFence", segment.Start)
	}
	return state
}

var markdown = goldmark.New(
	goldmark.WithExtensions(extension.TaskList),
	goldmark.WithParserOptions(parser.WithBlockParsers(
		util.Prioritized(fencedParser{parser.NewFencedCodeBlockParser()}, 699),
	), parser.WithParagraphTransformers(
		util.Prioritized(tableParser{extension.NewTableParagraphTransformer()}, 200),
	), parser.WithASTTransformers(
		util.Prioritized(extension.NewTableASTTransformer(), 0),
	)),
)

// project annotates source ranges instead of rendering HTML and attempting to
// reconstruct Markdown. Code contents and unsupported syntax stay literal.
func project(source string) *projection {
	data := []byte(source)
	p := &projection{hidden: make([]bool, len(data)), formats: make([]format, len(data)), replacements: make(map[int]replacement), lines: sourceLines(source), codeLines: make(map[int]bool), tableLines: make(map[int][]projectedCell)}
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
		switch node := n.(type) {
		case *ast.FencedCodeBlock:
			for _, name := range []string{"openingFence", "closingFence"} {
				if offset, ok := node.AttributeString(name); ok {
					index := lineAt(p.lines, offset.(int))
					line := p.lines[index]
					p.codeLines[index] = true
					hide(line.start, line.after)
				}
			}
			for i := 0; i < node.Lines().Len(); i++ {
				seg := node.Lines().At(i)
				index := lineAt(p.lines, seg.Start)
				p.codeLines[index] = true
				line := p.lines[index]
				style(line.start, line.after, code|codeBlock)
			}
			return -1, -1
		case *ast.CodeBlock, *ast.HTMLBlock, *ast.RawHTML:
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
		case *extast.Table:
			p.projectTable(source, node)
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
			f := heading
			switch node.Level {
			case 1:
				f |= bold | heading1
			case 2, 3:
				f |= bold
			}
			for i := 0; i < node.Lines().Len(); i++ {
				seg := node.Lines().At(i)
				line := p.lines[lineAt(p.lines, seg.Start)]
				style(line.start, line.end, f)
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
