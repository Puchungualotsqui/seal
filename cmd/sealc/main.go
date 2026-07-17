package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"seal/internal/build"
	"seal/internal/checker"
	cgen "seal/internal/codegen"
	"seal/internal/diag"
	"seal/internal/lexer"
	"seal/internal/parser"
	"seal/internal/resolver"
	"seal/internal/source"
	"seal/internal/token"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "lex":
		requireArgs(3)
		runLex(os.Args[2])

	case "parse":
		requireArgs(3)
		runParse(os.Args[2])

	case "resolve":
		requireArgs(3)
		runResolve(os.Args[2])

	case "check":
		requireArgs(3)
		runCheck(os.Args[2])

	case "emit-c":
		requireArgs(3)
		runEmitC(os.Args[2])

	case "packages":
		requireArgs(3)
		runPackages(os.Args[2])

	case "build":
		runBuild(os.Args[2:])

	case "run":
		runRun(os.Args[2:])

	default:
		printUsage()
		os.Exit(1)
	}
}

func requireArgs(count int) {
	if len(os.Args) < count {
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  sealc lex <file.seal>")
	fmt.Fprintln(os.Stderr, "  sealc parse <file.seal>")
	fmt.Fprintln(os.Stderr, "  sealc resolve <file.seal>")
	fmt.Fprintln(os.Stderr, "  sealc check <file.seal>")
	fmt.Fprintln(os.Stderr, "  sealc emit-c <file.seal>")
	fmt.Fprintln(os.Stderr, "  sealc packages <path>")
	fmt.Fprintln(os.Stderr, "  sealc build <path> [--emit-c] [-compiler compiler] [-o output]")
	fmt.Fprintln(os.Stderr, "  sealc run <path> [-compiler compiler] [-o output] [-- arguments...]")
}

func readAndLex(path string) ([]token.Token, *diag.Reporter) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(
			os.Stderr,
			"failed to read %s: %v\n",
			path,
			err,
		)

		os.Exit(1)
	}

	file := source.NewFile(
		path,
		string(bytes),
	)

	reporter := diag.NewReporter()

	lex := lexer.New(
		file,
		reporter,
	)

	tokens := lex.LexAll()

	return tokens, reporter
}

func runLex(path string) {
	tokens, reporter := readAndLex(path)

	for _, tok := range tokens {
		printToken(tok)
	}

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}
}

func runParse(path string) {
	tokens, reporter := readAndLex(path)

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	p := parser.New(
		tokens,
		reporter,
	)

	file := p.ParseFile()

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	fmt.Println(
		parser.DebugSummary(file),
	)
}

func runResolve(path string) {
	tokens, reporter := readAndLex(path)

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	p := parser.New(
		tokens,
		reporter,
	)

	file := p.ParseFile()

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	r := resolver.New(reporter)
	scope := r.ResolveFile(file)

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	fmt.Println(
		resolver.DebugSummary(scope),
	)
}

func runCheck(path string) {
	tokens, reporter := readAndLex(path)

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	p := parser.New(
		tokens,
		reporter,
	)

	file := p.ParseFile()

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	r := resolver.New(reporter)
	r.ResolveFile(file)

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	c := checker.New(reporter)
	scope := c.CheckFile(file)

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	fmt.Println(
		checker.DebugSummary(scope),
	)
}

func runEmitC(path string) {
	tokens, reporter := readAndLex(path)

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	p := parser.New(
		tokens,
		reporter,
	)

	file := p.ParseFile()

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	r := resolver.New(reporter)
	r.ResolveFile(file)

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	c := checker.New(reporter)
	c.CheckFile(file)

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	g := cgen.New(reporter)
	out := g.Generate(file)

	if reporter.HasErrors() {
		fmt.Fprint(
			os.Stderr,
			reporter.String(),
		)

		os.Exit(1)
	}

	fmt.Print(out)
}

func runPackages(path string) {
	graph, err :=
		build.DiscoverAndBuildGraph(path)

	if err != nil {
		fmt.Fprintln(
			os.Stderr,
			err,
		)

		os.Exit(1)
	}

	fmt.Print(
		build.DebugGraph(graph),
	)
}

func extractCompilerOverride(
	args []string,
) ([]string, string, error) {
	remaining := make(
		[]string,
		0,
		len(args),
	)

	compiler := ""
	foundCompiler := false

	setCompiler := func(
		option string,
		value string,
	) error {
		value = strings.TrimSpace(value)

		if value == "" {
			return fmt.Errorf(
				"%s requires a non-empty compiler name",
				option,
			)
		}

		if foundCompiler {
			return fmt.Errorf(
				"compiler option was specified more than once",
			)
		}

		compiler = value
		foundCompiler = true

		return nil
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "-compiler" ||
			arg == "--compiler":
			if i+1 >= len(args) {
				return nil,
					"",
					fmt.Errorf(
						"%s requires a compiler name",
						arg,
					)
			}

			i++

			if err := setCompiler(
				arg,
				args[i],
			); err != nil {
				return nil, "", err
			}

		case strings.HasPrefix(
			arg,
			"-compiler=",
		):
			if err := setCompiler(
				"-compiler",
				strings.TrimPrefix(
					arg,
					"-compiler=",
				),
			); err != nil {
				return nil, "", err
			}

		case strings.HasPrefix(
			arg,
			"--compiler=",
		):
			if err := setCompiler(
				"--compiler",
				strings.TrimPrefix(
					arg,
					"--compiler=",
				),
			); err != nil {
				return nil, "", err
			}

		default:
			remaining = append(
				remaining,
				arg,
			)
		}
	}

	return remaining,
		compiler,
		nil
}

func parseBuildLikeArguments(
	command string,
	args []string,
	allowEmitOnly bool,
) (
	string,
	build.BuildOptions,
	error,
) {
	remainingArgs,
		compilerOverride,
		err := extractCompilerOverride(args)

	if err != nil {
		return "",
			build.BuildOptions{},
			err
	}

	path := "."

	options := build.BuildOptions{
		Compiler: compilerOverride,
	}

	for i := 0; i < len(remainingArgs); i++ {
		arg := remainingArgs[i]

		switch arg {
		case "--emit-c":
			if !allowEmitOnly {
				return "",
					build.BuildOptions{},
					fmt.Errorf(
						"--emit-c cannot be used with %s",
						command,
					)
			}

			options.EmitOnly = true

		case "-o", "--output":
			if i+1 >= len(remainingArgs) {
				return "",
					build.BuildOptions{},
					fmt.Errorf(
						"missing output path after %s",
						arg,
					)
			}

			i++
			options.Output = remainingArgs[i]

		case "-compiler", "--compiler":
			// extractCompilerOverride should already have removed these.
			return "",
				build.BuildOptions{},
				fmt.Errorf(
					"internal error: compiler option was not extracted",
				)

		default:
			if strings.HasPrefix(
				arg,
				"-",
			) {
				return "",
					build.BuildOptions{},
					fmt.Errorf(
						"unknown %s option %q",
						command,
						arg,
					)
			}

			path = arg
		}
	}

	return path,
		options,
		nil
}

func splitProgramArguments(
	args []string,
) (
	[]string,
	[]string,
) {
	for index, arg := range args {
		if arg != "--" {
			continue
		}

		commandArgs := append(
			[]string(nil),
			args[:index]...,
		)

		programArgs := append(
			[]string(nil),
			args[index+1:]...,
		)

		return commandArgs,
			programArgs
	}

	return append(
			[]string(nil),
			args...,
		),
		nil
}

func runBuild(args []string) {
	path,
		options,
		err :=
		parseBuildLikeArguments(
			"build",
			args,
			true,
		)

	if err != nil {
		fmt.Fprintln(
			os.Stderr,
			err,
		)

		os.Exit(1)
	}

	result, err := build.BuildWorkspace(
		path,
		options,
	)

	if err != nil {
		fmt.Fprintln(
			os.Stderr,
			err,
		)

		os.Exit(1)
	}

	if options.EmitOnly {
		fmt.Printf(
			"generated C in %s\n",
			result.OutDir,
		)

		return
	}

	fmt.Printf(
		"built %s\n",
		result.Output,
	)
}

func runRun(args []string) {
	commandArgs,
		programArgs :=
		splitProgramArguments(args)

	path,
		options,
		err :=
		parseBuildLikeArguments(
			"run",
			commandArgs,
			false,
		)

	if err != nil {
		fmt.Fprintln(
			os.Stderr,
			err,
		)

		os.Exit(1)
	}

	_, err = build.RunWorkspace(
		path,
		options,
		programArgs,
	)

	if err == nil {
		return
	}

	var exitErr *exec.ExitError

	if errors.As(
		err,
		&exitErr,
	) {
		exitCode := exitErr.ExitCode()

		if exitCode < 0 {
			exitCode = 1
		}

		os.Exit(exitCode)
	}

	fmt.Fprintln(
		os.Stderr,
		err,
	)

	os.Exit(1)
}

func printToken(tok token.Token) {
	pos := tok.Span.File.Position(
		tok.Span.Start,
	)

	fmt.Printf(
		"%s:%d:%d  %-12s  %q\n",
		tok.Span.File.Path,
		pos.Line,
		pos.Column,
		tok.Kind.String(),
		tok.Lexeme,
	)
}
