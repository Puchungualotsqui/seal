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
    y := x + 20
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

	if !strings.Contains(text, "intptr_t x = 10;") {
		t.Fatalf("expected generated x variable, got:\n%s", text)
	}

	if !strings.Contains(text, "intptr_t y = (x + 20);") {
		t.Fatalf("expected generated y variable, got:\n%s", text)
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
    y := x + 20
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
    y := x + 1
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

	if !strings.Contains(gameC, "intptr_t helper_Double(intptr_t arg0);") {
		t.Fatalf("expected imported helper prototype, got:\n%s", gameC)
	}

	if !strings.Contains(gameC, "intptr_t x = helper_Double(10);") {
		t.Fatalf("expected helper_Double call, got:\n%s", gameC)
	}

	helperBytes, err := os.ReadFile(filepath.Join(outDir, "helper.c"))
	if err != nil {
		t.Fatal(err)
	}

	helperC := string(helperBytes)

	if !strings.Contains(helperC, "intptr_t helper_Double(intptr_t x)") {
		t.Fatalf("expected prefixed helper function definition, got:\n%s", helperC)
	}
}

func TestBuildWorkspacePackageQualifiedVariadicSpreadEmitOnly(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
version = "0.1.0"
kind = "executable"
dependencies = [
    { name = "fmtlike", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "app", "main.seal"), `
Main :: task() {
    PrintForward("x = %", 10)
}
`)

	writeFile(t, filepath.Join(root, "app", "forward.seal"), `
PrintForward :: task(format string, args ...any) {
    fmtlike.Print(format, args...)
}
`)

	writeFile(t, filepath.Join(root, "fmtlike", "seal.toml"), `
name = "fmtlike"
version = "0.1.0"
kind = "library"
`)

	writeFile(t, filepath.Join(root, "fmtlike", "fmtlike.seal"), `
Print :: task(format string, args ...any) {
}
`)

	outDir := filepath.Join(root, "out")

	_, err := BuildWorkspace(filepath.Join(root, "app"), BuildOptions{
		EmitOnly: true,
		OutDir:   outDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	appBytes, err := os.ReadFile(filepath.Join(outDir, "app.c"))
	if err != nil {
		t.Fatal(err)
	}

	appC := string(appBytes)

	if !strings.Contains(appC, "fmtlike_Print(format, args)") {
		t.Fatalf("expected package-qualified variadic forwarding, got:\n%s", appC)
	}
}

func TestCompilerCommandPresets(t *testing.T) {
	path, args, err := compilerCommand(Config{
		Compiler: "gcc",
	})
	if err != nil {
		t.Fatal(err)
	}

	if path != "gcc" {
		t.Fatalf("expected gcc, got %q", path)
	}

	if len(args) != 0 {
		t.Fatalf("expected no args, got %#v", args)
	}

	path, args, err = compilerCommand(Config{
		Compiler: "zigcc",
	})
	if err != nil {
		t.Fatal(err)
	}

	if path != "zig" {
		t.Fatalf("expected zig, got %q", path)
	}

	if strings.Join(args, " ") != "cc" {
		t.Fatalf("expected cc arg, got %#v", args)
	}

	path, args, err = compilerCommand(Config{
		Compiler:     "zigcc",
		CompilerPath: "C:/tools/zig/zig.exe",
		CompilerArgs: []string{"cc"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if path != "C:/tools/zig/zig.exe" {
		t.Fatalf("expected explicit zig path, got %q", path)
	}

	if strings.Join(args, " ") != "cc" {
		t.Fatalf("expected cc arg, got %#v", args)
	}
}

func TestCompilerConfigArgs(t *testing.T) {
	args := compilerConfigArgs(Config{
		Compiler:    "gcc",
		Standard:    "c11",
		CFlags:      []string{"-Wall"},
		LinkFlags:   []string{"-static"},
		IncludeDirs: []string{"include"},
		LibraryDirs: []string{"lib"},
		Libraries:   []string{"m"},
		Defines:     []string{"SEAL_DEBUG=1"},
	})

	got := strings.Join(args, " ")
	wantParts := []string{
		"-std=c11",
		"-Iinclude",
		"-DSEAL_DEBUG=1",
		"-Wall",
		"-Llib",
		"-lm",
		"-static",
	}

	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in args %q", want, got)
		}
	}
}
