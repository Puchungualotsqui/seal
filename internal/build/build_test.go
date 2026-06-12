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
