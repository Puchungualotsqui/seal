package diag

import (
	"strings"
	"testing"

	"seal/internal/source"
)

func TestReporterCollectsDiagnostics(t *testing.T) {
	file := source.NewFile("main.seal", "x := @")
	reporter := NewReporter()

	reporter.Add(source.NewSpan(file, 5, 6), "unexpected token")

	if !reporter.HasErrors() {
		t.Fatalf("expected reporter to have errors")
	}

	if len(reporter.Diagnostics()) != 1 {
		t.Fatalf("expected 1 diagnostic")
	}
}

func TestReporterString(t *testing.T) {
	file := source.NewFile("main.seal", "x := @")
	reporter := NewReporter()

	reporter.Add(source.NewSpan(file, 5, 6), "unexpected token")

	out := reporter.String()

	if !strings.Contains(out, "main.seal:1:6") {
		t.Fatalf("expected location in output, got %q", out)
	}

	if !strings.Contains(out, "unexpected token") {
		t.Fatalf("expected message in output, got %q", out)
	}
}
