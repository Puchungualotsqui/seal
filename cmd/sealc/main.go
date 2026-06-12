package main

import (
	"fmt"
	"os"

	"seal/internal/checker"
	"seal/internal/diag"
	"seal/internal/lexer"
	"seal/internal/parser"
	"seal/internal/resolver"
	"seal/internal/source"
	"seal/internal/token"
)

func main() {
	if len(os.Args) < 3 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "lex":
		runLex(os.Args[2])

	case "parse":
		runParse(os.Args[2])

	case "resolve":
		runResolve(os.Args[2])

	case "check":
		runCheck(os.Args[2])

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  sealc lex <file.seal>")
	fmt.Fprintln(os.Stderr, "  sealc parse <file.seal>")
	fmt.Fprintln(os.Stderr, "  sealc resolve <file.seal>")
}

func readAndLex(path string) ([]token.Token, *diag.Reporter) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read %s: %v\n", path, err)
		os.Exit(1)
	}

	file := source.NewFile(path, string(bytes))
	reporter := diag.NewReporter()

	lex := lexer.New(file, reporter)
	tokens := lex.LexAll()

	return tokens, reporter
}

func runLex(path string) {
	tokens, reporter := readAndLex(path)

	for _, tok := range tokens {
		printToken(tok)
	}

	if reporter.HasErrors() {
		fmt.Fprint(os.Stderr, reporter.String())
		os.Exit(1)
	}
}

func runParse(path string) {
	tokens, reporter := readAndLex(path)

	if reporter.HasErrors() {
		fmt.Fprint(os.Stderr, reporter.String())
		os.Exit(1)
	}

	p := parser.New(tokens, reporter)
	file := p.ParseFile()

	if reporter.HasErrors() {
		fmt.Fprint(os.Stderr, reporter.String())
		os.Exit(1)
	}

	fmt.Println(parser.DebugSummary(file))
}

func runResolve(path string) {
	tokens, reporter := readAndLex(path)

	if reporter.HasErrors() {
		fmt.Fprint(os.Stderr, reporter.String())
		os.Exit(1)
	}

	p := parser.New(tokens, reporter)
	file := p.ParseFile()

	if reporter.HasErrors() {
		fmt.Fprint(os.Stderr, reporter.String())
		os.Exit(1)
	}

	r := resolver.New(reporter)
	scope := r.ResolveFile(file)

	if reporter.HasErrors() {
		fmt.Fprint(os.Stderr, reporter.String())
		os.Exit(1)
	}

	fmt.Println(resolver.DebugSummary(scope))
}

func runCheck(path string) {
	tokens, reporter := readAndLex(path)

	if reporter.HasErrors() {
		fmt.Fprint(os.Stderr, reporter.String())
		os.Exit(1)
	}

	p := parser.New(tokens, reporter)
	file := p.ParseFile()

	if reporter.HasErrors() {
		fmt.Fprint(os.Stderr, reporter.String())
		os.Exit(1)
	}

	r := resolver.New(reporter)
	r.ResolveFile(file)

	if reporter.HasErrors() {
		fmt.Fprint(os.Stderr, reporter.String())
		os.Exit(1)
	}

	c := checker.New(reporter)
	scope := c.CheckFile(file)

	if reporter.HasErrors() {
		fmt.Fprint(os.Stderr, reporter.String())
		os.Exit(1)
	}

	fmt.Println(checker.DebugSummary(scope))
}

func printToken(tok token.Token) {
	pos := tok.Span.File.Position(tok.Span.Start)

	fmt.Printf("%s:%d:%d  %-12s  %q\n",
		tok.Span.File.Path,
		pos.Line,
		pos.Column,
		tok.Kind.String(),
		tok.Lexeme,
	)
}
