package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildWorkspaceEmitOnly(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "game", "seal.toml"), `
name = "game"
version = "0.1.0"
kind = "executable"
`)

	writeFile(t, filepath.Join(root, "game", "main.seal"), `
Main :: task() {
    x := 10
    Print(x)
}
`)

	outDir := filepath.Join(root, "out")

	result, err := BuildWorkspace(filepath.Join(root, "game"), BuildOptions{
		EmitOnly: true,
		OutDir:   outDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Graph.Root.Config.Name != "game" {
		t.Fatalf("expected root game, got %q", result.Graph.Root.Config.Name)
	}

	cPath := filepath.Join(outDir, "game.c")
	bytes, err := os.ReadFile(cPath)
	if err != nil {
		t.Fatal(err)
	}

	text := string(bytes)

	if !strings.Contains(text, "int main(void)") {
		t.Fatalf("expected generated main, got:\n%s", text)
	}

	if !strings.Contains(text, `printf("%d", x);`) {
		t.Fatalf("expected generated Print call, got:\n%s", text)
	}
}

func TestBuildWorkspaceMultiplePackageEmitOnly(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "game", "seal.toml"), `
name = "game"
version = "0.1.0"
kind = "executable"
dependencies = [
    { name = "helper", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "game", "main.seal"), `
Main :: task() {
    x := 10
    Print(x)
}
`)

	writeFile(t, filepath.Join(root, "helper", "seal.toml"), `
name = "helper"
version = "0.1.0"
kind = "library"
`)

	writeFile(t, filepath.Join(root, "helper", "helper.seal"), `
Double :: task(x int) int {
    return x * 2
}
`)

	outDir := filepath.Join(root, "out")

	result, err := BuildWorkspace(filepath.Join(root, "game"), BuildOptions{
		EmitOnly: true,
		OutDir:   outDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Packages) != 2 {
		t.Fatalf("expected 2 loaded packages, got %d", len(result.Packages))
	}

	if _, err := os.Stat(filepath.Join(outDir, "helper.c")); err != nil {
		t.Fatalf("expected helper.c: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "game.c")); err != nil {
		t.Fatalf("expected game.c: %v", err)
	}
}

func TestBuildWorkspacePackageQualifiedCallEmitOnly(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "game", "seal.toml"), `
name = "game"
version = "0.1.0"
kind = "executable"
dependencies = [
    { name = "helper", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "game", "main.seal"), `
Main :: task() {
    x := helper.Double(10)
    Print(x)
}
`)

	writeFile(t, filepath.Join(root, "helper", "seal.toml"), `
name = "helper"
version = "0.1.0"
kind = "library"
`)

	writeFile(t, filepath.Join(root, "helper", "helper.seal"), `
Double :: task(x int) int {
    return x * 2
}
`)

	outDir := filepath.Join(root, "out")

	_, err := BuildWorkspace(filepath.Join(root, "game"), BuildOptions{
		EmitOnly: true,
		OutDir:   outDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	gameBytes, err := os.ReadFile(filepath.Join(outDir, "game.c"))
	if err != nil {
		t.Fatal(err)
	}

	gameC := string(gameBytes)

	if !strings.Contains(gameC, "int helper_Double(int arg0);") {
		t.Fatalf("expected imported helper prototype, got:\n%s", gameC)
	}

	if !strings.Contains(gameC, "int x = helper_Double(10);") {
		t.Fatalf("expected helper_Double call, got:\n%s", gameC)
	}

	helperBytes, err := os.ReadFile(filepath.Join(outDir, "helper.c"))
	if err != nil {
		t.Fatal(err)
	}

	helperC := string(helperBytes)

	if !strings.Contains(helperC, "int helper_Double(int x)") {
		t.Fatalf("expected prefixed helper function definition, got:\n%s", helperC)
	}
}
