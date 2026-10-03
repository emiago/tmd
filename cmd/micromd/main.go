package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"micromd/internal/editor"
)

var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, editor.Run)) }

func run(args []string, out, errOut io.Writer, open func(string) error) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")) {
		help(out)
		return 0
	}
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintf(out, "micromd %s\n", version)
		return 0
	}
	literalPath := len(args) == 2 && args[0] == "--"
	if literalPath {
		args = args[1:]
	}
	if len(args) != 1 || args[0] == "" || (strings.HasPrefix(args[0], "-") && !literalPath) {
		fmt.Fprintln(errOut, "usage: micromd <file>\nRun 'micromd --help' for details.")
		return 2
	}
	if err := open(args[0]); err != nil {
		fmt.Fprintf(errOut, "micromd: %v\n", err)
		return 2
	}
	return 0
}

func help(out io.Writer) {
	fmt.Fprint(out, `micromd — Markdown you can read. Plain text you can edit.

Usage:
  micromd <file>
  micromd --help
  micromd --version

A small terminal editor with nano-style controls and inline Markdown display.
Headings, bold, italic, inline code, lists, and checkboxes are rendered;
the current line reveals its source while you edit. Unsupported syntax
and fenced examples stay literal. There are no formatting commands.

Open .md, .markdown, or .mdx files. Missing files start empty and are created
only on save. No Git checkout, plugin, or background service is required.
Use an interactive terminal and regular UTF-8 files; symlinks, directories,
binary data, and invalid UTF-8 are rejected. Saves preserve existing file
permissions and line endings. New files use 0600. External edits prevent
overwrite; use Save As to keep your buffer in a new file.

`)
	fmt.Fprintln(out, editor.HelpText)
	fmt.Fprintln(out, "\nExamples:\n  micromd notes.md\n  micromd QUEUE.md\n  micromd -- ./-notes.md\n\nExit status: 0 on normal exit; 2 on error or handled termination.")
}
