package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"tmd/internal/editor"
)

var version = "dev"

func main() {
	os.Exit(runWithInput(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, editor.Run, term.IsTerminal(int(os.Stdout.Fd()))))
}

func run(args []string, out, errOut io.Writer, open func(string) error) int {
	return runWithInput(args, strings.NewReader(""), out, errOut, open, false)
}

func runWithInput(args []string, in io.Reader, out, errOut io.Writer, open func(string) error, styled bool) int {
	if len(args) > 0 && args[0] == "--render" {
		if len(args) > 2 || (len(args) == 2 && args[1] == "") {
			fmt.Fprintln(errOut, "usage: tmd --render [file|-]")
			return 2
		}
		var data []byte
		var err error
		if len(args) == 2 && args[1] != "-" {
			data, err = os.ReadFile(args[1])
		} else {
			data, err = io.ReadAll(in)
		}
		if err == nil {
			err = editor.Render(out, string(data), styled)
		}
		if err != nil {
			fmt.Fprintf(errOut, "tmd: %v\n", err)
			return 2
		}
		return 0
	}
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")) {
		help(out)
		return 0
	}
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintf(out, "tmd %s\n", version)
		return 0
	}
	literalPath := len(args) == 2 && args[0] == "--"
	if literalPath {
		args = args[1:]
	}
	if len(args) != 1 || args[0] == "" || (strings.HasPrefix(args[0], "-") && !literalPath) {
		fmt.Fprintln(errOut, "usage: tmd <file>\nRun 'tmd --help' for details.")
		return 2
	}
	if err := open(args[0]); err != nil {
		fmt.Fprintf(errOut, "tmd: %v\n", err)
		return 2
	}
	return 0
}

func help(out io.Writer) {
	fmt.Fprint(out, `TUI Markdown (tmd) — Markdown you can read. Plain text you can edit.

Usage:
  tmd <file>
  tmd --render [file|-]
  tmd --help
  tmd --version

A small terminal editor with nano-style controls and inline Markdown display.
Headings, bold, italic, inline code, lists, and checkboxes are rendered;
the current line reveals its source while you edit. Unsupported syntax
and fenced examples stay literal. There are no formatting commands.

Use --render to print Markdown from a file or stdin without opening the editor.
Terminal output uses styles; redirected output is plain text. Fenced blocks
stay literal, with no language-specific parsing. Input may use any file extension.

Open .md, .markdown, or .mdx files. Missing files start empty and are created
only on save. No Git checkout, plugin, or background service is required.
Use an interactive terminal and regular UTF-8 files; symlinks, directories,
binary data, and invalid UTF-8 are rejected. Saves preserve existing file
permissions and line endings. New files use 0600. External edits prevent
overwrite; use Save As to keep your buffer in a new file.

`)
	fmt.Fprintln(out, editor.HelpText)
	fmt.Fprintln(out, "\nExamples:\n  tmd notes.md\n  tmd QUEUE.md\n  tmd -- ./-notes.md\n\nExit status: 0 on normal exit; 2 on error or handled termination.")
}
