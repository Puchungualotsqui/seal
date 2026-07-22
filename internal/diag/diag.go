package diag

import (
	"fmt"
	"strings"

	"seal/internal/source"
)

type Severity uint8

const (
	SeverityInvalid Severity = iota
	SeverityError
	SeverityWarning
	SeverityInformation
	SeverityHint
)

func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"

	case SeverityWarning:
		return "warning"

	case SeverityInformation:
		return "information"

	case SeverityHint:
		return "hint"

	default:
		return "error"
	}
}

type Diagnostic struct {
	Span     source.Span
	Message  string
	Severity Severity

	/*
		Code is an optional stable diagnostic identifier.

		Examples:

		    undefined-symbol
		    type-mismatch
		    invalid-return-count

		It is intended for LSP clients and future code actions. Existing
		compiler diagnostics may leave it empty.
	*/
	Code string

	/*
		Source identifies the component that produced the diagnostic.

		The default value used by Reporter is "seal".
	*/
	Source string
}

type Reporter struct {
	diagnostics []Diagnostic
}

func NewReporter() *Reporter {
	return &Reporter{
		diagnostics: nil,
	}
}

/*
Add records an ordinary compiler error.

This preserves the existing Reporter API used by the lexer, parser, resolver,
checker, and code generator.
*/
func (r *Reporter) Add(
	span source.Span,
	message string,
) {
	r.AddDiagnostic(
		Diagnostic{
			Span:     span,
			Message:  message,
			Severity: SeverityError,
			Source:   "seal",
		},
	)
}

func (r *Reporter) AddError(
	span source.Span,
	code string,
	message string,
) {
	r.AddDiagnostic(
		Diagnostic{
			Span:     span,
			Message:  message,
			Severity: SeverityError,
			Code:     code,
			Source:   "seal",
		},
	)
}

func (r *Reporter) AddWarning(
	span source.Span,
	code string,
	message string,
) {
	r.AddDiagnostic(
		Diagnostic{
			Span:     span,
			Message:  message,
			Severity: SeverityWarning,
			Code:     code,
			Source:   "seal",
		},
	)
}

func (r *Reporter) AddInformation(
	span source.Span,
	code string,
	message string,
) {
	r.AddDiagnostic(
		Diagnostic{
			Span:     span,
			Message:  message,
			Severity: SeverityInformation,
			Code:     code,
			Source:   "seal",
		},
	)
}

func (r *Reporter) AddHint(
	span source.Span,
	code string,
	message string,
) {
	r.AddDiagnostic(
		Diagnostic{
			Span:     span,
			Message:  message,
			Severity: SeverityHint,
			Code:     code,
			Source:   "seal",
		},
	)
}

/*
AddDiagnostic records a fully structured diagnostic.

Invalid or omitted severity is normalized to Error. An omitted source is
normalized to "seal".
*/
func (r *Reporter) AddDiagnostic(
	diagnostic Diagnostic,
) {
	if r == nil {
		return
	}

	if diagnostic.Severity ==
		SeverityInvalid {
		diagnostic.Severity =
			SeverityError
	}

	if diagnostic.Source == "" {
		diagnostic.Source = "seal"
	}

	r.diagnostics = append(
		r.diagnostics,
		diagnostic,
	)
}

/*
AddDiagnostics copies diagnostics into the reporter.

The input slice is not retained.
*/
func (r *Reporter) AddDiagnostics(
	diagnostics []Diagnostic,
) {
	if r == nil {
		return
	}

	for _, diagnostic := range diagnostics {
		r.AddDiagnostic(
			diagnostic,
		)
	}
}

/*
HasErrors reports whether at least one error-severity diagnostic exists.

Warnings, information messages, and hints do not cause compilation to fail.
*/
func (r *Reporter) HasErrors() bool {
	if r == nil {
		return false
	}

	for _, diagnostic := range r.diagnostics {
		severity :=
			diagnostic.Severity

		if severity ==
			SeverityInvalid {
			severity =
				SeverityError
		}

		if severity ==
			SeverityError {
			return true
		}
	}

	return false
}

func (r *Reporter) Count() int {
	if r == nil {
		return 0
	}

	return len(
		r.diagnostics,
	)
}

func (r *Reporter) ErrorCount() int {
	if r == nil {
		return 0
	}

	count := 0

	for _, diagnostic := range r.diagnostics {
		severity :=
			diagnostic.Severity

		if severity ==
			SeverityInvalid {
			severity =
				SeverityError
		}

		if severity ==
			SeverityError {
			count++
		}
	}

	return count
}

/*
Diagnostics returns an independent copy.

Callers may sort, group, or filter the returned slice without mutating the
reporter's internal state.
*/
func (r *Reporter) Diagnostics() []Diagnostic {
	if r == nil {
		return nil
	}

	return append(
		[]Diagnostic(nil),
		r.diagnostics...,
	)
}

func (r *Reporter) String() string {
	if r == nil {
		return ""
	}

	var builder strings.Builder

	for _, diagnostic := range r.diagnostics {
		severity :=
			diagnostic.Severity

		if severity ==
			SeverityInvalid {
			severity =
				SeverityError
		}

		builder.WriteString(
			fmt.Sprintf(
				"%s: %s: %s\n",
				diagnostic.Span.String(),
				severity.String(),
				diagnostic.Message,
			),
		)
	}

	return builder.String()
}
