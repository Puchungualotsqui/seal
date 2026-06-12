package diag

import (
	"fmt"
	"strings"

	"seal/internal/source"
)

type Diagnostic struct {
	Span    source.Span
	Message string
}

type Reporter struct {
	diagnostics []Diagnostic
}

func NewReporter() *Reporter {
	return &Reporter{}
}

func (r *Reporter) Add(span source.Span, message string) {
	r.diagnostics = append(r.diagnostics, Diagnostic{
		Span:    span,
		Message: message,
	})
}

func (r *Reporter) HasErrors() bool {
	return len(r.diagnostics) > 0
}

func (r *Reporter) Diagnostics() []Diagnostic {
	return r.diagnostics
}

func (r *Reporter) String() string {
	var b strings.Builder

	for _, d := range r.diagnostics {
		b.WriteString(fmt.Sprintf("%s: error: %s\n", d.Span.String(), d.Message))
	}

	return b.String()
}
