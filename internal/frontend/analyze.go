package frontend

import (
	"seal/internal/ast"
	"seal/internal/checker"
	"seal/internal/diag"
	"seal/internal/lexer"
	"seal/internal/parser"
	"seal/internal/resolver"
	"seal/internal/source"
)

/*
AnalyzePackage runs the Seal frontend over an in-memory package snapshot.

The frontend performs no filesystem access and no C generation.

Analysis phases:

 1. Lex every source file.
 2. Parse files that lexed successfully.
 3. Resolve the combined package when all files parsed successfully.
 4. Type-check the package when resolution completed successfully.

A syntax error prevents resolution because missing or malformed declarations
can otherwise cause misleading semantic diagnostics. A resolution error
prevents type checking for the same reason.

The returned Result always contains every source.File and every token stream
that could be produced.
*/
func AnalyzePackage(
	input PackageInput,
	resolverPackages map[string]*resolver.PackageInfo,
	checkerPackages map[string]*checker.PackageInfo,
) Result {
	result := Result{
		PackageName: input.Name,
		Files:       nil,
		Combined:    &ast.File{},
		Parsed:      true,
		Resolved:    false,
		Checked:     false,
	}

	allDiagnostics :=
		diag.NewReporter()

	for _, sourceInput := range input.Files {
		fileResult,
			fileDiagnostics :=
			analyzeSourceFile(
				sourceInput,
			)

		result.Files = append(
			result.Files,
			fileResult,
		)

		allDiagnostics.AddDiagnostics(
			fileDiagnostics,
		)

		if fileResult == nil ||
			!fileResult.Parsed {
			result.Parsed = false
		}

		if fileResult == nil ||
			fileResult.AST == nil {
			continue
		}

		result.Combined.Decls =
			append(
				result.Combined.Decls,
				fileResult.AST.Decls...,
			)
	}

	if !result.Parsed {
		result.Diagnostics =
			allDiagnostics.Diagnostics()

		return result
	}

	resolutionReporter :=
		diag.NewReporter()

	resolverInstance :=
		resolver.NewWithPackages(
			resolutionReporter,
			resolverPackages,
		)

	result.ResolverScope =
		resolverInstance.ResolveFile(
			result.Combined,
		)

	/*
		Resolver semantic information remains useful even when resolution reports an
		error. Retain every declaration, successful symbol use, and scope region that
		was discovered before the failure.
	*/
	result.ResolverSemantic =
		resolverInstance.SemanticInfo()

	allDiagnostics.AddDiagnostics(
		resolutionReporter.Diagnostics(),
	)

	if resolutionReporter.HasErrors() {
		result.Diagnostics =
			allDiagnostics.Diagnostics()

		return result
	}

	result.Resolved = true

	result.ResolverPackage =
		resolver.ExportPackage(
			input.Name,
			result.ResolverScope,
		)

	checkerReporter :=
		diag.NewReporter()

	checkerInstance :=
		checker.NewWithPackagesAndOptions(
			checkerReporter,
			checkerPackages,
			input.CheckerOptions,
		)

	result.CheckerScope =
		checkerInstance.CheckFile(
			result.Combined,
		)

	/*
		Semantic information may contain useful partial data even when checking
		reports errors, so retain it before inspecting the reporter.
	*/
	result.SemanticInfo =
		checkerInstance.SemanticInfo()

	allDiagnostics.AddDiagnostics(
		checkerReporter.Diagnostics(),
	)

	if checkerReporter.HasErrors() {
		result.Diagnostics =
			allDiagnostics.Diagnostics()

		return result
	}

	result.Checked = true

	result.CheckerPackage =
		checker.ExportPackage(
			input.Name,
			result.CheckerScope,
		)

	result.Diagnostics =
		allDiagnostics.Diagnostics()

	return result
}

func analyzeSourceFile(
	input SourceInput,
) (
	*ParsedFile,
	[]diag.Diagnostic,
) {
	sourceFile :=
		source.NewFile(
			input.Path,
			input.Text,
		)

	result := &ParsedFile{
		Source: sourceFile,
		Tokens: nil,
		AST:    nil,
		Lexed:  false,
		Parsed: false,
	}

	lexReporter :=
		diag.NewReporter()

	lexerInstance :=
		lexer.New(
			sourceFile,
			lexReporter,
		)

	result.Tokens =
		lexerInstance.LexAll()

	result.Lexed = true

	if lexReporter.HasErrors() {
		return result,
			lexReporter.Diagnostics()
	}

	parseReporter :=
		diag.NewReporter()

	parserInstance :=
		parser.New(
			result.Tokens,
			parseReporter,
		)

	result.AST =
		parserInstance.ParseFile()

	result.Parsed =
		!parseReporter.HasErrors()

	diagnostics :=
		lexReporter.Diagnostics()

	diagnostics = append(
		diagnostics,
		parseReporter.Diagnostics()...,
	)

	return result,
		diagnostics
}
