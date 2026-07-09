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

func TestBuildWorkspaceImportedGenericTaskSpecializationEmitOnly(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
version = "0.1.0"
kind = "executable"
dependencies = [
    { name = "types", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "app", "main.seal"), `
Main :: task() {
    b := types.MakeBox<int>(10)
    x := b.value
    assert(x == 10)
}
`)

	writeFile(t, filepath.Join(root, "types", "seal.toml"), `
name = "types"
version = "0.1.0"
kind = "library"
`)

	writeFile(t, filepath.Join(root, "types", "types.seal"), `
Box :: struct <T type> {
    value T
}

MakeBox :: task <T type>(value T) Box<T> {
    return Box<T>{value = value}
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

	appChecks := []string{
		"typedef struct types_Box_int {",
		"intptr_t value;",
		"} types_Box_int;",
		"types_Box_int types_MakeBox_int(intptr_t value);",
		"types_Box_int b = types_MakeBox_int(10);",
		"intptr_t x = (b).value;",
		"assert((x == 10));",
	}

	for _, want := range appChecks {
		if !strings.Contains(appC, want) {
			t.Fatalf("expected app.c to contain %q, got:\n%s", want, appC)
		}
	}

	typesBytes, err := os.ReadFile(filepath.Join(outDir, "types.c"))
	if err != nil {
		t.Fatal(err)
	}

	typesC := string(typesBytes)

	typesChecks := []string{
		"typedef struct types_Box_int {",
		"intptr_t value;",
		"} types_Box_int;",
		"types_Box_int types_MakeBox_int(intptr_t value);",
		"types_Box_int types_MakeBox_int(intptr_t value) {",
	}

	for _, want := range typesChecks {
		if !strings.Contains(typesC, want) {
			t.Fatalf("expected types.c to contain %q, got:\n%s", want, typesC)
		}
	}
}

func TestBuildWorkspaceImportedGenericStructValueParamEmitOnly(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
version = "0.1.0"
kind = "executable"
dependencies = [
    { name = "types", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "app", "main.seal"), `
Main :: task() {
    b: types.Buffer<int, 4>
    x := b.data[0]
    assert(x == 0)
}
`)

	writeFile(t, filepath.Join(root, "types", "seal.toml"), `
name = "types"
version = "0.1.0"
kind = "library"
`)

	writeFile(t, filepath.Join(root, "types", "types.seal"), `
Buffer :: struct <T type, N int> {
    data [N]T
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

	appChecks := []string{
		"typedef struct types_Buffer_int_4 {",
		"intptr_t data[4];",
		"} types_Buffer_int_4;",
		"types_Buffer_int_4 b;",
		"intptr_t x = (b).data[0];",
		"assert((x == 0));",
	}

	for _, want := range appChecks {
		if !strings.Contains(appC, want) {
			t.Fatalf("expected app.c to contain %q, got:\n%s", want, appC)
		}
	}

	typesBytes, err := os.ReadFile(filepath.Join(outDir, "types.c"))
	if err != nil {
		t.Fatal(err)
	}

	typesC := string(typesBytes)

	typesChecks := []string{
		"typedef struct types_Buffer_int_4 {",
		"intptr_t data[4];",
		"} types_Buffer_int_4;",
	}

	for _, want := range typesChecks {
		if !strings.Contains(typesC, want) {
			t.Fatalf("expected types.c to contain %q, got:\n%s", want, typesC)
		}
	}
}

func TestBuildWorkspaceGenericRequestFixedPointNestedStructsEmitOnly(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
version = "0.1.0"
kind = "executable"
dependencies = [
    { name = "types", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "app", "main.seal"), `
Main :: task() {
    b := types.MakeNested<int>()
    x := b.value.data[0]
    assert(x == 0)
}
`)

	writeFile(t, filepath.Join(root, "types", "seal.toml"), `
name = "types"
version = "0.1.0"
kind = "library"
`)

	writeFile(t, filepath.Join(root, "types", "types.seal"), `
Buffer :: struct <T type, N int> {
    data [N]T
}

Box :: struct <T type> {
    value T
}

MakeNested :: task <T type>() Box<Buffer<T, 4>> {
    buffer: Buffer<T, 4>
    return Box<Buffer<T, 4>>{value = buffer}
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

	appChecks := []string{
		"typedef struct types_Buffer_int_4 {",
		"intptr_t data[4];",
		"} types_Buffer_int_4;",
		"typedef struct types_Box_types_Buffer_int_4 {",
		"types_Buffer_int_4 value;",
		"} types_Box_types_Buffer_int_4;",
		"types_Box_types_Buffer_int_4 types_MakeNested_int(void);",
		"types_Box_types_Buffer_int_4 b = types_MakeNested_int();",
		"intptr_t x = ((b).value).data[0];",
	}

	for _, want := range appChecks {
		if !strings.Contains(appC, want) {
			t.Fatalf("expected app.c to contain %q, got:\n%s", want, appC)
		}
	}

	typesBytes, err := os.ReadFile(filepath.Join(outDir, "types.c"))
	if err != nil {
		t.Fatal(err)
	}

	typesC := string(typesBytes)

	typesChecks := []string{
		"typedef struct types_Buffer_int_4 {",
		"intptr_t data[4];",
		"} types_Buffer_int_4;",
		"typedef struct types_Box_types_Buffer_int_4 {",
		"types_Buffer_int_4 value;",
		"} types_Box_types_Buffer_int_4;",
		"types_Box_types_Buffer_int_4 types_MakeNested_int(void);",
		"types_Box_types_Buffer_int_4 types_MakeNested_int(void) {",
	}

	for _, want := range typesChecks {
		if !strings.Contains(typesC, want) {
			t.Fatalf("expected types.c to contain %q, got:\n%s", want, typesC)
		}
	}
}

func TestBuildWorkspaceGenericRequestsDeduplicateEmitOnly(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
version = "0.1.0"
kind = "executable"
dependencies = [
    { name = "types", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "app", "main.seal"), `
Main :: task() {
    a := types.MakeBox<int>(10)
    b := types.MakeBox<int>(20)
    assert(a.value == 10)
    assert(b.value == 20)
}
`)

	writeFile(t, filepath.Join(root, "types", "seal.toml"), `
name = "types"
version = "0.1.0"
kind = "library"
`)

	writeFile(t, filepath.Join(root, "types", "types.seal"), `
Box :: struct <T type> {
    value T
}

MakeBox :: task <T type>(value T) Box<T> {
    return Box<T>{value = value}
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

	typesBytes, err := os.ReadFile(filepath.Join(outDir, "types.c"))
	if err != nil {
		t.Fatal(err)
	}

	typesC := string(typesBytes)

	if count := strings.Count(typesC, "typedef struct types_Box_int {"); count != 1 {
		t.Fatalf("expected one types_Box_int typedef, got %d:\n%s", count, typesC)
	}

	if count := strings.Count(typesC, "types_Box_int types_MakeBox_int(intptr_t value) {"); count != 1 {
		t.Fatalf("expected one types_MakeBox_int definition, got %d:\n%s", count, typesC)
	}
}

func TestBuildWorkspaceImportedGenericTaskConstraintEmitOnly(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
version = "0.1.0"
kind = "executable"
dependencies = [
    { name = "types", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "app", "main.seal"), `
Apply :: task <T type, F task[(T) T]>(value T) T {
    return F(value)
}

Main :: task() {
    x := Apply<int, types.Identity<int>>(10)
    assert(x == 10)
}
`)

	writeFile(t, filepath.Join(root, "types", "seal.toml"), `
name = "types"
version = "0.1.0"
kind = "library"
`)

	writeFile(t, filepath.Join(root, "types", "types.seal"), `
Identity :: task <T type>(value T) T {
    return value
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

	appChecks := []string{
		"intptr_t types_Identity_int(intptr_t value);",
		"intptr_t app_Apply_int_types_Identity_int(intptr_t value);",
		"intptr_t x = app_Apply_int_types_Identity_int(10);",
		"types_Identity_int(value)",
	}

	for _, want := range appChecks {
		if !strings.Contains(appC, want) {
			t.Fatalf("expected app.c to contain %q, got:\n%s", want, appC)
		}
	}

	typesBytes, err := os.ReadFile(filepath.Join(outDir, "types.c"))
	if err != nil {
		t.Fatal(err)
	}

	typesC := string(typesBytes)

	typesChecks := []string{
		"intptr_t types_Identity_int(intptr_t value);",
		"intptr_t types_Identity_int(intptr_t value) {",
	}

	for _, want := range typesChecks {
		if !strings.Contains(typesC, want) {
			t.Fatalf("expected types.c to contain %q, got:\n%s", want, typesC)
		}
	}
}

func TestBuildWorkspaceImportedGenericFieldConstraintEmitOnly(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
version = "0.1.0"
kind = "executable"
dependencies = [
    { name = "types", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "app", "main.seal"), `
HealthOf :: task <T type[health int]>(target T) int {
    return target.health
}

Main :: task() {
    p := types.Player{health = 10}
    h := HealthOf<types.Player>(p)
    assert(h == 10)
}
`)

	writeFile(t, filepath.Join(root, "types", "seal.toml"), `
name = "types"
version = "0.1.0"
kind = "library"
`)

	writeFile(t, filepath.Join(root, "types", "types.seal"), `
Player :: struct {
    health int
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

	appChecks := []string{
		"typedef struct types_Player {",
		"intptr_t health;",
		"} types_Player;",
		"intptr_t app_HealthOf_types_Player(types_Player target);",
		"types_Player p = (types_Player){.health = 10};",
		"intptr_t h = app_HealthOf_types_Player(p);",
		"(target).health",
	}

	for _, want := range appChecks {
		if !strings.Contains(appC, want) {
			t.Fatalf("expected app.c to contain %q, got:\n%s", want, appC)
		}
	}

	typesBytes, err := os.ReadFile(filepath.Join(outDir, "types.c"))
	if err != nil {
		t.Fatal(err)
	}

	typesC := string(typesBytes)

	if !strings.Contains(typesC, "typedef struct types_Player {") {
		t.Fatalf("expected types.c to contain Player definition, got:\n%s", typesC)
	}
}

func TestBuildWorkspaceImportedGenericStructValueConstraintEmitOnly(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "app", "seal.toml"), `
name = "app"
version = "0.1.0"
kind = "executable"
dependencies = [
    { name = "types", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "app", "main.seal"), `
Main :: task() {
    b: types.Buffer<int, 4>
    x := b.data[0]
    assert(x == 0)
}
`)

	writeFile(t, filepath.Join(root, "types", "seal.toml"), `
name = "types"
version = "0.1.0"
kind = "library"
`)

	writeFile(t, filepath.Join(root, "types", "types.seal"), `
Buffer :: struct <T type, N int[N > 0]> {
    data [N]T
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

	appChecks := []string{
		"typedef struct types_Buffer_int_4 {",
		"intptr_t data[4];",
		"} types_Buffer_int_4;",
		"types_Buffer_int_4 b;",
		"intptr_t x = (b).data[0];",
	}

	for _, want := range appChecks {
		if !strings.Contains(appC, want) {
			t.Fatalf("expected app.c to contain %q, got:\n%s", want, appC)
		}
	}

	typesBytes, err := os.ReadFile(filepath.Join(outDir, "types.c"))
	if err != nil {
		t.Fatal(err)
	}

	typesC := string(typesBytes)

	typesChecks := []string{
		"typedef struct types_Buffer_int_4 {",
		"intptr_t data[4];",
		"} types_Buffer_int_4;",
	}

	for _, want := range typesChecks {
		if !strings.Contains(typesC, want) {
			t.Fatalf("expected types.c to contain %q, got:\n%s", want, typesC)
		}
	}
}
