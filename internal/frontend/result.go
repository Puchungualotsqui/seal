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

ResolverSemantic contains lexical declarations, resolved symbol uses,
definition locations, and source regions associated with lexical scopes.

SemanticInfo contains checker-produced semantic information.
*/
type Result struct {
	PackageName string

	Files    []*ParsedFile
	Combined *ast.File

	ResolverScope *resolver.Scope
	CheckerScope  *checker.Scope

	ResolverPackage *resolver.PackageInfo
	CheckerPackage  *checker.PackageInfo

	ResolverSemantic resolver.SemanticInfo
	SemanticInfo     checker.SemanticInfo

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

/*
ScopeAt returns the innermost lexical scope associated with a source position.

The offset is a UTF-8 byte offset into the file contents.
*/
func (r *Result) ScopeAt(
	path string,
	offset int,
) *resolver.Scope {
	if r == nil {
		return nil
	}

	file :=
		r.FileByPath(
			path,
		)

	if file == nil ||
		file.Source == nil {
		return nil
	}

	return r.ResolverSemantic.ScopeAt(
		file.Source,
		offset,
	)
}

/*
ResolvedUseAt returns the smallest resolved symbol use containing the supplied
UTF-8 byte offset.
*/
func (r *Result) ResolvedUseAt(
	path string,
	offset int,
) *resolver.ResolvedUse {
	if r == nil {
		return nil
	}

	file :=
		r.FileByPath(
			path,
		)

	if file == nil ||
		file.Source == nil {
		return nil
	}

	return r.ResolverSemantic.UseAt(
		file.Source,
		offset,
	)
}

/*
DefinitionAt returns the declaration whose name contains the supplied UTF-8
byte offset.
*/
func (r *Result) DefinitionAt(
	path string,
	offset int,
) *resolver.Definition {
	if r == nil {
		return nil
	}

	file :=
		r.FileByPath(
			path,
		)

	if file == nil ||
		file.Source == nil {
		return nil
	}

	return r.ResolverSemantic.DefinitionAt(
		file.Source,
		offset,
	)
}
