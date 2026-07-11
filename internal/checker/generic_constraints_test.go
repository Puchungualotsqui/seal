package checker

import (
	"strings"
	"testing"

	"seal/internal/diag"
	"seal/internal/resolver"
)

func checkSourceWithOptions(t *testing.T, input string, options Options) *diag.Reporter {
	t.Helper()

	file, reporter := parseCheckerFile(t, "test", input)
	if reporter.HasErrors() {
		t.Fatalf("parser diagnostics:\n%s", reporter.String())
	}

	resolverReporter := diag.NewReporter()
	res := resolver.New(resolverReporter)
	res.ResolveFile(file)
	if resolverReporter.HasErrors() {
		t.Fatalf("resolver diagnostics:\n%s", resolverReporter.String())
	}

	checkerReporter := diag.NewReporter()
	c := NewWithPackagesAndOptions(checkerReporter, nil, options)
	c.CheckFile(file)

	return checkerReporter
}

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
    value T
}

Main :: task() {
    b: Buffer<int, 4>
    x := b.value
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
    value T
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
    value T
}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    b: types.Buffer<int, 4>
    x := b.value
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
    value T
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

func TestCheckGenericValueConstraintAllowsPureTaskPredicate(t *testing.T) {
	reporter := checkSource(t, `
Over :: pure task(n int) bool {
    return n > 18
}

UseAge :: task <Age int[Over(Age)]>() {}

Main :: task() {
    UseAge<21>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintRejectsPureTaskPredicateFalse(t *testing.T) {
	reporter := checkSource(t, `
Over :: pure task(n int) bool {
    return n > 18
}

UseAge :: task <Age int[Over(Age)]>() {}

Main :: task() {
    UseAge<18>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic constraint failed: Over(18)`) {
		t.Fatalf("expected failed pure task constraint diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintAllowsPureTaskPredicateInBooleanExpression(t *testing.T) {
	reporter := checkSource(t, `
Over :: pure task(n int) bool {
    return n > 18
}

UseAge :: task <Age int[Over(Age) && Age != 90]>() {}

Main :: task() {
    UseAge<21>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintRejectsPureTaskPredicateInBooleanExpression(t *testing.T) {
	reporter := checkSource(t, `
Over :: pure task(n int) bool {
    return n > 18
}

UseAge :: task <Age int[Over(Age) && Age != 90]>() {}

Main :: task() {
    UseAge<90>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic constraint failed: Over(90) && 90 != 90`) {
		t.Fatalf("expected failed composed pure task constraint diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintRejectsNonPureTaskPredicate(t *testing.T) {
	reporter := checkSource(t, `
Over :: task(n int) bool {
    return n > 18
}

UseAge :: task <Age int[Over(Age)]>() {}

Main :: task() {
    UseAge<21>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic constraint call "Over" must be pure`) {
		t.Fatalf("expected non-pure constraint diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintRejectsPureTaskPredicateWrongReturnType(t *testing.T) {
	reporter := checkSource(t, `
Bad :: pure task(n int) int {
    return n
}

UseAge :: task <Age int[Bad(Age)]>() {}

Main :: task() {
    UseAge<21>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic constraint must be bool, got int`) {
		t.Fatalf("expected bool constraint diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintAllowsPureTaskDeclaredAfterUse(t *testing.T) {
	reporter := checkSource(t, `
UseAge :: task <Age int[Over(Age)]>() {}

Over :: pure task(n int) bool {
    return n > 18
}

Main :: task() {
    UseAge<21>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintAllowsPureOperatorOverload(t *testing.T) {
	reporter := checkSource(t, `
Matrix :: struct {
    years int
}

Over :: pure task(age Matrix) bool {
    return age.years > 18
}

IsVampireAge :: pure task(age Matrix, tag string) bool {
    return age.years == 999 && tag == "vampire"
}

== :: overload {
    IsVampireAge
}

UseAge :: task <age Matrix[Over(age) || age == "vampire"]>() {}

Main :: task() {
    UseAge<Matrix{years = 999}>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintRejectsPureOperatorOverloadFalse(t *testing.T) {
	reporter := checkSource(t, `
Matrix :: struct {
    years int
}

Over :: pure task(age Matrix) bool {
    return age.years > 18
}

IsVampireAge :: pure task(age Matrix, tag string) bool {
    return age.years == 999 && tag == "vampire"
}

== :: overload {
    IsVampireAge
}

UseAge :: task <age Matrix[Over(age) || age == "vampire"]>() {}

Main :: task() {
    UseAge<Matrix{years = 10}>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic constraint failed`) {
		t.Fatalf("expected failed operator-overload constraint diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintRejectsNonPureOperatorOverload(t *testing.T) {
	reporter := checkSource(t, `
Matrix :: struct {
    years int
}

IsVampireAge :: task(age Matrix, tag string) bool {
    return age.years == 999 && tag == "vampire"
}

== :: overload {
    IsVampireAge
}

UseAge :: task <age Matrix[age == "vampire"]>() {}

Main :: task() {
    UseAge<Matrix{years = 999}>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic constraint operator "==" candidate "IsVampireAge" must be pure`) {
		t.Fatalf("expected non-pure operator-overload diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintAllowsDerivedNotEqualOperatorOverload(t *testing.T) {
	reporter := checkSource(t, `
Matrix :: struct {
    years int
}

IsVampireAge :: pure task(age Matrix, tag string) bool {
    return age.years == 999 && tag == "vampire"
}

== :: overload {
    IsVampireAge
}

UseMortalAge :: task <age Matrix[age != "vampire"]>() {}

Main :: task() {
    UseMortalAge<Matrix{years = 10}>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintRejectsDerivedNotEqualOperatorOverloadFalse(t *testing.T) {
	reporter := checkSource(t, `
Matrix :: struct {
    years int
}

IsVampireAge :: pure task(age Matrix, tag string) bool {
    return age.years == 999 && tag == "vampire"
}

== :: overload {
    IsVampireAge
}

UseMortalAge :: task <age Matrix[age != "vampire"]>() {}

Main :: task() {
    UseMortalAge<Matrix{years = 999}>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic constraint failed`) {
		t.Fatalf("expected failed derived != overload constraint diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintAllowsOperatorOverloadDeclaredAfterUse(t *testing.T) {
	reporter := checkSource(t, `
Matrix :: struct {
    years int
}

UseAge :: task <age Matrix[age == "vampire"]>() {}

IsVampireAge :: pure task(age Matrix, tag string) bool {
    return age.years == 999 && tag == "vampire"
}

== :: overload {
    IsVampireAge
}

Main :: task() {
    UseAge<Matrix{years = 999}>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintRejectsRecursivePureTaskWhenDepthGuardEnabled(t *testing.T) {
	reporter := checkSourceWithOptions(t, `
Loop :: pure task(n int) bool {
    return Loop(n)
}

UseAge :: task <Age int[Loop(Age)]>() {}

Main :: task() {
    UseAge<21>()
}
`, Options{
		GenericConstraintMaxDepth: 8,
	})

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `recursive generic constraint evaluation through "Loop"`) {
		t.Fatalf("expected recursive generic constraint diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintRejectsMutualRecursivePureTasksWhenDepthGuardEnabled(t *testing.T) {
	reporter := checkSourceWithOptions(t, `
A :: pure task(n int) bool {
    return B(n)
}

B :: pure task(n int) bool {
    return A(n)
}

UseAge :: task <Age int[A(Age)]>() {}

Main :: task() {
    UseAge<21>()
}
`, Options{
		GenericConstraintMaxDepth: 8,
	})

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `recursive generic constraint evaluation through "A"`) {
		t.Fatalf("expected mutual recursion diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckGenericValueConstraintRejectsPureTaskEvaluationPastMaxDepth(t *testing.T) {
	reporter := checkSourceWithOptions(t, `
F1 :: pure task(n int) bool {
    return F2(n)
}

F2 :: pure task(n int) bool {
    return F3(n)
}

F3 :: pure task(n int) bool {
    return F4(n)
}

F4 :: pure task(n int) bool {
    return n > 18
}

UseAge :: task <Age int[F1(Age)]>() {}

Main :: task() {
    UseAge<21>()
}
`, Options{
		GenericConstraintMaxDepth: 3,
	})

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic constraint evaluation exceeded max depth 3`) {
		t.Fatalf("expected generic constraint max depth diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericValueConstraintAllowsPureTaskPredicate(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "rules", `
Over :: pure task(n int) bool {
    return n > 18
}
`)

	reporter := checkWithPackages(t, `
UseAge :: task <Age int[rules.Over(Age)]>() {}

Main :: task() {
    UseAge<21>()
}
`, map[string]*resolver.PackageInfo{
		"rules": resolverPkg,
	}, map[string]*PackageInfo{
		"rules": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericValueConstraintRejectsPureTaskPredicateFalse(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "rules", `
Over :: pure task(n int) bool {
    return n > 18
}
`)

	reporter := checkWithPackages(t, `
UseAge :: task <Age int[rules.Over(Age)]>() {}

Main :: task() {
    UseAge<18>()
}
`, map[string]*resolver.PackageInfo{
		"rules": resolverPkg,
	}, map[string]*PackageInfo{
		"rules": checkerPkg,
	})

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic constraint failed: rules.Over(18)`) {
		t.Fatalf("expected imported pure task constraint failure, got:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericValueConstraintAllowsPureTaskCallingPureTask(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "rules", `
OverLimit :: pure task(n int, limit int) bool {
    return n > limit
}

Over :: pure task(n int) bool {
    return OverLimit(n, 18)
}
`)

	reporter := checkWithPackages(t, `
UseAge :: task <Age int[rules.Over(Age)]>() {}

Main :: task() {
    UseAge<21>()
}
`, map[string]*resolver.PackageInfo{
		"rules": resolverPkg,
	}, map[string]*PackageInfo{
		"rules": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericValueConstraintAllowsPureOperatorOverload(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "rules", `
Matrix :: struct {
    years int
}

IsVampireAge :: pure task(age Matrix, tag string) bool {
    return age.years == 999 && tag == "vampire"
}

== :: overload {
    IsVampireAge
}
`)

	reporter := checkWithPackages(t, `
UseAge :: task <age rules.Matrix[age == "vampire"]>() {}

Main :: task() {
    UseAge<rules.Matrix{years = 999}>()
}
`, map[string]*resolver.PackageInfo{
		"rules": resolverPkg,
	}, map[string]*PackageInfo{
		"rules": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericValueConstraintRejectsPureOperatorOverloadFalse(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "rules", `
Matrix :: struct {
    years int
}

IsVampireAge :: pure task(age Matrix, tag string) bool {
    return age.years == 999 && tag == "vampire"
}

== :: overload {
    IsVampireAge
}
`)

	reporter := checkWithPackages(t, `
UseAge :: task <age rules.Matrix[age == "vampire"]>() {}

Main :: task() {
    UseAge<rules.Matrix{years = 10}>()
}
`, map[string]*resolver.PackageInfo{
		"rules": resolverPkg,
	}, map[string]*PackageInfo{
		"rules": checkerPkg,
	})

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic constraint failed`) {
		t.Fatalf("expected imported pure operator constraint failure, got:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericValueConstraintAllowsDerivedNotEqualPureOperatorOverload(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "rules", `
Matrix :: struct {
    years int
}

IsVampireAge :: pure task(age Matrix, tag string) bool {
    return age.years == 999 && tag == "vampire"
}

== :: overload {
    IsVampireAge
}
`)

	reporter := checkWithPackages(t, `
UseMortalAge :: task <age rules.Matrix[age != "vampire"]>() {}

Main :: task() {
    UseMortalAge<rules.Matrix{years = 10}>()
}
`, map[string]*resolver.PackageInfo{
		"rules": resolverPkg,
	}, map[string]*PackageInfo{
		"rules": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}
