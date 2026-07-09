package checker

import (
	"strings"
	"testing"

	"seal/internal/resolver"
)

func TestCheckGenericTypeFieldConstraint(t *testing.T) {
	reporter := checkSource(t, `
Player :: struct {
    health int
}

HealthOf :: task <T type[health int]>(target T) int {
    return target.health
}

Main :: task() {
    p := Player{health = 10}
    h := HealthOf<Player>(p)
    assert(h == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckGenericTypeFieldConstraintRejectsMissingField(t *testing.T) {
	reporter := checkSource(t, `
Rock :: struct {
    weight int
}

HealthOf :: task <T type[health int]>(target T) int {
    return target.health
}

Main :: task() {
    r := Rock{weight = 10}
    h := HealthOf<Rock>(r)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "health") {
		t.Fatalf("expected diagnostic to mention missing health field, got:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraint(t *testing.T) {
	reporter := checkSource(t, `
Buffer :: struct <T type, N int[N > 0]> {
    data [N]T
}

Main :: task() {
    b: Buffer<int, 4>
    x := b.data[0]
    assert(x == 0)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintRejectsInvalidValue(t *testing.T) {
	reporter := checkSource(t, `
Buffer :: struct <T type, N int[N > 0]> {
    data [N]T
}

Main :: task() {
    b: Buffer<int, 0>
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "N") && !strings.Contains(reporter.String(), "constraint") {
		t.Fatalf("expected generic value constraint diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckGenericTaskSignatureConstraint(t *testing.T) {
	reporter := checkSource(t, `
Identity :: task <T type>(value T) T {
    return value
}

Apply :: task <T type, F task[(T) T]>(value T) T {
    return F(value)
}

Main :: task() {
    x := Apply<int, Identity<int>>(10)
    assert(x == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckGenericTaskSignatureConstraintRejectsMismatch(t *testing.T) {
	reporter := checkSource(t, `
Identity :: task <T type>(value T) T {
    return value
}

Apply :: task <T type, F task[(T) T]>(value T) T {
    return F(value)
}

Main :: task() {
    x := Apply<int, Identity<string>>(10)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "task") {
		t.Fatalf("expected task signature constraint diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckGenericEnumVariantConstraint(t *testing.T) {
	reporter := checkSource(t, `
State :: enum {
    Ready
    Busy
}

NeedsReady :: task <E enum[Ready]>() {
}

Main :: task() {
    NeedsReady<State>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckGenericEnumVariantConstraintRejectsMissingVariant(t *testing.T) {
	reporter := checkSource(t, `
State :: enum {
    Busy
}

NeedsReady :: task <E enum[Ready]>() {
}

Main :: task() {
    NeedsReady<State>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "Ready") {
		t.Fatalf("expected missing Ready variant diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckGenericUnionMemberConstraint(t *testing.T) {
	reporter := checkSource(t, `
Failure :: struct {
    code int
}

Result :: union {
    Failure
    int
}

NeedsFailure :: task <U union[Failure]>() {
}

Main :: task() {
    NeedsFailure<Result>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckGenericUnionMemberConstraintRejectsMissingMember(t *testing.T) {
	reporter := checkSource(t, `
Failure :: struct {
    code int
}

Result :: union {
    int
}

NeedsFailure :: task <U union[Failure]>() {
}

Main :: task() {
    NeedsFailure<Result>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "Failure") {
		t.Fatalf("expected missing Failure member diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericValueConstraint(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Buffer :: struct <T type, N int[N > 0]> {
    data [N]T
}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    b: types.Buffer<int, 4>
    x := b.data[0]
    assert(x == 0)
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericValueConstraintRejectsInvalidValue(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Buffer :: struct <T type, N int[N > 0]> {
    data [N]T
}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    b: types.Buffer<int, 0>
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "N") && !strings.Contains(reporter.String(), "constraint") {
		t.Fatalf("expected imported generic value constraint diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericTaskSignatureConstraint(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Identity :: task <T type>(value T) T {
    return value
}
`)

	reporter := checkWithPackages(t, `
Apply :: task <T type, F task[(T) T]>(value T) T {
    return F(value)
}

Main :: task() {
    x := Apply<int, types.Identity<int>>(10)
    assert(x == 10)
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericTaskSignatureConstraintRejectsMismatch(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Identity :: task <T type>(value T) T {
    return value
}
`)

	reporter := checkWithPackages(t, `
Apply :: task <T type, F task[(T) T]>(value T) T {
    return F(value)
}

Main :: task() {
    x := Apply<int, types.Identity<string>>(10)
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "task") {
		t.Fatalf("expected imported task signature constraint diagnostic, got:\n%s", reporter.String())
	}
}
