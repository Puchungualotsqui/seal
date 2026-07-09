package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBuildTestFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildWorkspaceImportedPureGenericValueConstraint(t *testing.T) {
	root := t.TempDir()

	writeBuildTestFile(t, filepath.Join(root, "seal.workspace"), `
`)

	writeBuildTestFile(t, filepath.Join(root, "rules", "seal.toml"), `
name = "rules"
kind = "library"
`)

	writeBuildTestFile(t, filepath.Join(root, "rules", "rules.seal"), `
Over :: pure task(n int) bool {
    return n > 18
}
`)

	writeBuildTestFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
kind = "executable"

dependencies = [
    { name = "rules" }
]
`)

	writeBuildTestFile(t, filepath.Join(root, "app", "main.seal"), `
UseAge :: task <Age int[rules.Over(Age)]>() {}

Main :: task() {
    UseAge<21>()
}
`)

	result, err := BuildWorkspace(filepath.Join(root, "app"), BuildOptions{
		EmitOnly: true,
	})
	if err != nil {
		t.Fatalf("BuildWorkspace failed:\n%v", err)
	}

	if result == nil {
		t.Fatalf("expected build result")
	}

	if len(result.Packages) != 2 {
		t.Fatalf("expected 2 loaded packages, got %d", len(result.Packages))
	}
}

func TestBuildWorkspaceImportedPureGenericValueConstraintRejectsFalse(t *testing.T) {
	root := t.TempDir()

	writeBuildTestFile(t, filepath.Join(root, "seal.workspace"), `
`)

	writeBuildTestFile(t, filepath.Join(root, "rules", "seal.toml"), `
name = "rules"
kind = "library"
`)

	writeBuildTestFile(t, filepath.Join(root, "rules", "rules.seal"), `
Over :: pure task(n int) bool {
    return n > 18
}
`)

	writeBuildTestFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
kind = "executable"

dependencies = [
    { name = "rules" }
]
`)

	writeBuildTestFile(t, filepath.Join(root, "app", "main.seal"), `
UseAge :: task <Age int[rules.Over(Age)]>() {}

Main :: task() {
    UseAge<18>()
}
`)

	_, err := BuildWorkspace(filepath.Join(root, "app"), BuildOptions{
		EmitOnly: true,
	})
	if err == nil {
		t.Fatalf("expected BuildWorkspace to fail")
	}

	if !strings.Contains(err.Error(), `generic constraint failed: rules.Over(18)`) {
		t.Fatalf("expected imported pure constraint failure, got:\n%v", err)
	}
}

func TestBuildWorkspaceGenericConstraintMaxDepthFromConfig(t *testing.T) {
	root := t.TempDir()

	writeBuildTestFile(t, filepath.Join(root, "seal.workspace"), `
`)

	writeBuildTestFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
kind = "executable"

[checker]
generic_constraint_max_depth = 8
`)

	writeBuildTestFile(t, filepath.Join(root, "app", "main.seal"), `
Loop :: pure task(n int) bool {
    return Loop(n)
}

UseAge :: task <Age int[Loop(Age)]>() {}

Main :: task() {
    UseAge<21>()
}
`)

	_, err := BuildWorkspace(filepath.Join(root, "app"), BuildOptions{
		EmitOnly: true,
	})
	if err == nil {
		t.Fatalf("expected BuildWorkspace to fail")
	}

	if !strings.Contains(err.Error(), `recursive generic constraint evaluation through "Loop"`) {
		t.Fatalf("expected recursive generic constraint diagnostic, got:\n%v", err)
	}
}

func TestBuildWorkspaceImportedPureOperatorGenericValueConstraint(t *testing.T) {
	root := t.TempDir()

	writeBuildTestFile(t, filepath.Join(root, "seal.workspace"), `
`)

	writeBuildTestFile(t, filepath.Join(root, "rules", "seal.toml"), `
name = "rules"
kind = "library"
`)

	writeBuildTestFile(t, filepath.Join(root, "rules", "rules.seal"), `
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

	writeBuildTestFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
kind = "executable"

dependencies = [
    { name = "rules" }
]
`)

	writeBuildTestFile(t, filepath.Join(root, "app", "main.seal"), `
Vampire :: rules.Matrix{years = 999}

UseAge :: task <age rules.Matrix[age == "vampire"]>() {}

Main :: task() {
    UseAge<Vampire>()
}
`)

	result, err := BuildWorkspace(filepath.Join(root, "app"), BuildOptions{
		EmitOnly: true,
	})
	if err != nil {
		t.Fatalf("BuildWorkspace failed:\n%v", err)
	}

	if result == nil {
		t.Fatalf("expected build result")
	}
}

func TestBuildWorkspaceImportedPureOperatorGenericValueConstraintRejectsFalse(t *testing.T) {
	root := t.TempDir()

	writeBuildTestFile(t, filepath.Join(root, "seal.workspace"), `
`)

	writeBuildTestFile(t, filepath.Join(root, "rules", "seal.toml"), `
name = "rules"
kind = "library"
`)

	writeBuildTestFile(t, filepath.Join(root, "rules", "rules.seal"), `
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

	writeBuildTestFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
kind = "executable"

dependencies = [
    { name = "rules" }
]
`)

	writeBuildTestFile(t, filepath.Join(root, "app", "main.seal"), `
Child :: rules.Matrix{years = 10}

UseAge :: task <age rules.Matrix[age == "vampire"]>() {}

Main :: task() {
    UseAge<Child>()
}
`)

	_, err := BuildWorkspace(filepath.Join(root, "app"), BuildOptions{
		EmitOnly: true,
	})
	if err == nil {
		t.Fatalf("expected BuildWorkspace to fail")
	}

	if !strings.Contains(err.Error(), `generic constraint failed`) {
		t.Fatalf("expected imported pure operator constraint failure, got:\n%v", err)
	}
}
