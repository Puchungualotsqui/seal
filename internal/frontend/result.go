package frontend

import (
	"seal/internal/ast"
	"seal/internal/checker"
	"seal/internal/diag"
	"seal/internal/resolver"
	"seal/internal/source"
	"seal/internal/token"
)

/*
ParsedFile retains the frontend artifacts belonging to one physical source
file.

AST may be non-nil even when Parsed is false because the parser performs error
recovery and may produce a partial file.
*/
type ParsedFile struct {
	Source *source.File
	Tokens []token.Token
	AST    *ast.File

	Lexed  bool
	Parsed bool
}

/*
Result is an immutable snapshot of one package analysis.

Phase flags indicate successful completion:

	Parsed   all files lexed and parsed without errors
	Resolved resolution completed without errors
	Checked  type checking completed without errors

Partial lexer tokens and parser ASTs remain available after syntax failures.
Resolver and checker results remain available when their respective phases ran,
even when that phase produced errors.
*/
type Result struct {
	PackageName string

	Files    []*ParsedFile
	Combined *ast.File

	ResolverScope *resolver.Scope
	CheckerScope  *checker.Scope

	ResolverPackage *resolver.PackageInfo
	CheckerPackage  *checker.PackageInfo

	SemanticInfo checker.SemanticInfo

	Diagnostics []diag.Diagnostic

	Parsed   bool
	Resolved bool
	Checked  bool
}

func (r *Result) HasErrors() bool {
	if r == nil {
		return false
	}

	for _, diagnostic := range r.Diagnostics {
		severity :=
			diagnostic.Severity

		if severity ==
			diag.SeverityInvalid {
			severity =
				diag.SeverityError
		}

		if severity ==
			diag.SeverityError {
			return true
		}
	}

	return false
}

func (r *Result) DiagnosticsForFile(
	path string,
) []diag.Diagnostic {
	if r == nil {
		return nil
	}

	var diagnostics []diag.Diagnostic

	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Span.File == nil ||
			diagnostic.Span.File.Path != path {
			continue
		}

		diagnostics = append(
			diagnostics,
			diagnostic,
		)
	}

	return diagnostics
}

func (r *Result) FileByPath(
	path string,
) *ParsedFile {
	if r == nil {
		return nil
	}

	for _, file := range r.Files {
		if file == nil ||
			file.Source == nil {
			continue
		}

		if file.Source.Path ==
			path {
			return file
		}
	}

	return nil
}
