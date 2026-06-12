package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, text string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seal.toml")

	writeFile(t, path, `
name = "game"
version = "0.1.0"
kind = "executable"

dependencies = [
    { name = "fmt", version = "0.1.0" },
    { name = "mem", version = "0.1.0" },
]

compiler = "clang"
linkage = "static"

auto_initialize_variables = true
allow_uninitialized_variables = false
allow_partial_initialized_structs = false
allow_partial_initialized_arrays = true
allow_partial_switches = false

integer_overflow = "trap"
bounds_checking = "trap"

fail_bad_style = false
allow_unused_variables = true
allow_unused_parameters = true
allow_run_directives = true
`)

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Name != "game" {
		t.Fatalf("expected game, got %q", cfg.Name)
	}

	if cfg.Kind != KindExecutable {
		t.Fatalf("expected executable, got %q", cfg.Kind)
	}

	if len(cfg.Dependencies) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(cfg.Dependencies))
	}

	if cfg.Dependencies[0].Name != "fmt" || cfg.Dependencies[0].Version != "0.1.0" {
		t.Fatalf("bad dependency: %+v", cfg.Dependencies[0])
	}
}

func TestDiscoverPackagesAndBuildOrder(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "game", "seal.toml"), `
name = "game"
version = "0.1.0"
kind = "executable"
dependencies = [
    { name = "fmt", version = "0.1.0" },
    { name = "mem", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "std", "fmt", "seal.toml"), `
name = "fmt"
version = "0.1.0"
kind = "library"
dependencies = [
    { name = "mem", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "std", "mem", "seal.toml"), `
name = "mem"
version = "0.1.0"
kind = "library"
`)

	graph, err := DiscoverAndBuildGraph(filepath.Join(root, "game"))
	if err != nil {
		t.Fatal(err)
	}

	if graph.Root.Config.Name != "game" {
		t.Fatalf("expected root game, got %q", graph.Root.Config.Name)
	}

	got := []string{}
	for _, pkg := range graph.Order {
		got = append(got, pkg.Config.Name)
	}

	want := strings.Join([]string{"mem", "fmt", "game"}, ",")
	if strings.Join(got, ",") != want {
		t.Fatalf("expected order %s, got %v", want, got)
	}
}

func TestDuplicatePackageName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "a", "seal.toml"), `
name = "fmt"
`)

	writeFile(t, filepath.Join(root, "b", "seal.toml"), `
name = "fmt"
`)

	_, err := DiscoverPackages(root)
	if err == nil {
		t.Fatalf("expected duplicate package error")
	}

	if !strings.Contains(err.Error(), `duplicate package name "fmt"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMissingDependency(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "game", "seal.toml"), `
name = "game"
kind = "executable"
dependencies = [
    { name = "fmt", version = "0.1.0" },
]
`)

	_, err := DiscoverAndBuildGraph(filepath.Join(root, "game"))
	if err == nil {
		t.Fatalf("expected missing dependency error")
	}

	if !strings.Contains(err.Error(), `depends on missing package "fmt"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVersionMismatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "game", "seal.toml"), `
name = "game"
dependencies = [
    { name = "fmt", version = "1.0.0" },
]
`)

	writeFile(t, filepath.Join(root, "fmt", "seal.toml"), `
name = "fmt"
version = "0.1.0"
`)

	_, err := DiscoverAndBuildGraph(filepath.Join(root, "game"))
	if err == nil {
		t.Fatalf("expected version mismatch")
	}

	if !strings.Contains(err.Error(), `requires fmt@1.0.0`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDependencyCycle(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "seal.workspace"), "")

	writeFile(t, filepath.Join(root, "a", "seal.toml"), `
name = "a"
dependencies = [
    { name = "b", version = "0.1.0" },
]
`)

	writeFile(t, filepath.Join(root, "b", "seal.toml"), `
name = "b"
dependencies = [
    { name = "a", version = "0.1.0" },
]
`)

	_, err := DiscoverAndBuildGraph(filepath.Join(root, "a"))
	if err == nil {
		t.Fatalf("expected dependency cycle")
	}

	if !strings.Contains(err.Error(), `dependency cycle`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverGraphFindsStdPackage(t *testing.T) {
	tmp := t.TempDir()

	workspace := filepath.Join(tmp, "workspace")
	app := filepath.Join(workspace, "app")
	std := filepath.Join(tmp, "std")
	mem := filepath.Join(std, "mem")

	mustMkdir(t, app)
	mustMkdir(t, mem)

	mustWrite(t, filepath.Join(workspace, "seal.workspace"), "")

	mustWrite(t, filepath.Join(app, "seal.toml"), `
name = "app"
version = "0.1.0"
kind = "executable"
dependencies = [
    "mem",
]
`)

	mustWrite(t, filepath.Join(app, "main.seal"), `
Main :: task() {
}
`)

	mustWrite(t, filepath.Join(mem, "seal.toml"), `
name = "mem"
version = "0.1.0"
kind = "library"
`)

	mustWrite(t, filepath.Join(mem, "mem.seal"), `
c :: @c_import {
    include "stdlib.h"
}

Alloc :: extern("malloc") task(size usize) rawptr
Free :: extern("free") task(ptr rawptr)
`)

	t.Setenv("SEAL_STD_PATH", std)

	graph, err := DiscoverAndBuildGraph(app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if graph.Packages["mem"] == nil {
		t.Fatalf("expected std package mem to be discovered")
	}

	if len(graph.Order) != 2 {
		t.Fatalf("expected build order app + mem, got %d", len(graph.Order))
	}

	if graph.Order[0].Config.Name != "mem" {
		t.Fatalf("expected mem to build first, got %s", graph.Order[0].Config.Name)
	}

	if graph.Order[1].Config.Name != "app" {
		t.Fatalf("expected app to build last, got %s", graph.Order[1].Config.Name)
	}
}

func TestStdDuplicatePackageNameRejected(t *testing.T) {
	tmp := t.TempDir()

	workspace := filepath.Join(tmp, "workspace")
	app := filepath.Join(workspace, "app")
	workspaceMem := filepath.Join(workspace, "mem")
	std := filepath.Join(tmp, "std")
	stdMem := filepath.Join(std, "mem")

	mustMkdir(t, app)
	mustMkdir(t, workspaceMem)
	mustMkdir(t, stdMem)

	mustWrite(t, filepath.Join(workspace, "seal.workspace"), "")

	mustWrite(t, filepath.Join(app, "seal.toml"), `
name = "app"
version = "0.1.0"
kind = "executable"
dependencies = [
    "mem",
]
`)

	mustWrite(t, filepath.Join(workspaceMem, "seal.toml"), `
name = "mem"
version = "0.1.0"
kind = "library"
`)

	mustWrite(t, filepath.Join(stdMem, "seal.toml"), `
name = "mem"
version = "0.1.0"
kind = "library"
`)

	t.Setenv("SEAL_STD_PATH", std)

	_, err := DiscoverAndBuildGraph(app)
	if err == nil {
		t.Fatalf("expected duplicate package name error")
	}

	if !strings.Contains(err.Error(), `duplicate package name "mem"`) {
		t.Fatalf("expected duplicate mem error, got: %v", err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestBuildWorkspaceUsesStdMemExternNames(t *testing.T) {
	tmp := t.TempDir()

	workspace := filepath.Join(tmp, "workspace")
	app := filepath.Join(workspace, "app")
	std := filepath.Join(tmp, "std")
	mem := filepath.Join(std, "mem")

	mustMkdir(t, app)
	mustMkdir(t, mem)

	mustWrite(t, filepath.Join(workspace, "seal.workspace"), "")

	mustWrite(t, filepath.Join(app, "seal.toml"), `
name = "app"
version = "0.1.0"
kind = "executable"
dependencies = [
    "mem",
]
`)

	mustWrite(t, filepath.Join(app, "main.seal"), `
Main :: task() {
    ptr := mem.Alloc(64)
    mem.Free(ptr)
}
`)

	mustWrite(t, filepath.Join(mem, "seal.toml"), `
name = "mem"
version = "0.1.0"
kind = "library"
`)

	mustWrite(t, filepath.Join(mem, "mem.seal"), `
c :: @c_import {
    include "stdlib.h"
}

Alloc :: extern("malloc") task(size usize) rawptr
Free :: extern("free") task(ptr rawptr)
`)

	t.Setenv("SEAL_STD_PATH", std)

	result, err := BuildWorkspace(app, BuildOptions{
		EmitOnly: true,
	})
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}

	var appCode string
	for _, pkg := range result.Packages {
		if pkg.Package.Config.Name == "app" {
			appCode = pkg.CCode
			break
		}
	}

	if appCode == "" {
		t.Fatalf("expected app C code")
	}

	if !strings.Contains(appCode, "malloc(64)") {
		t.Fatalf("expected mem.Alloc to lower to malloc, got:\n%s", appCode)
	}

	if !strings.Contains(appCode, "free(ptr)") {
		t.Fatalf("expected mem.Free to lower to free, got:\n%s", appCode)
	}

	if strings.Contains(appCode, "mem_Alloc") {
		t.Fatalf("did not expect mem_Alloc wrapper call, got:\n%s", appCode)
	}

	if strings.Contains(appCode, "mem_Free") {
		t.Fatalf("did not expect mem_Free wrapper call, got:\n%s", appCode)
	}
}
