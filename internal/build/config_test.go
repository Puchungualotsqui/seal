package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadConfigCheckerGenericConstraintMaxDepth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seal.toml")

	err := os.WriteFile(path, []byte(`
name = "app"

[checker]
generic_constraint_max_depth = 32
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.GenericConstraintMaxDepth != 32 {
		t.Fatalf("expected generic constraint max depth 32, got %d", cfg.GenericConstraintMaxDepth)
	}
}

func TestReadConfigCheckerGenericConstraintMaxDepthAllowsNegative(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seal.toml")

	err := os.WriteFile(path, []byte(`
name = "app"

[checker]
generic_constraint_max_depth = -1
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.GenericConstraintMaxDepth != -1 {
		t.Fatalf("expected generic constraint max depth -1, got %d", cfg.GenericConstraintMaxDepth)
	}
}
