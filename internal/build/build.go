package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Kind string

const (
	KindExecutable Kind = "executable"
	KindLibrary    Kind = "library"
)

type Dependency struct {
	Name    string
	Version string
}

type Config struct {
	Name    string
	Version string
	Kind    Kind
	RootDir string

	Dependencies []Dependency

	Compiler     string
	CompilerPath string
	CompilerArgs []string
	CFlags       []string
	LinkFlags    []string
	IncludeDirs  []string
	LibraryDirs  []string
	Libraries    []string
	Defines      []string
	Target       string
	Standard     string
	Linkage      string

	AutoInitializeVariables        bool
	AllowUninitializedVariables    bool
	AllowPartialInitializedStructs bool
	AllowPartialInitializedArrays  bool
	AllowPartialSwitches           bool

	IntegerOverflow string
	BoundsChecking  string

	FailBadStyle          bool
	AllowUnusedVariables  bool
	AllowUnusedParameters bool
	AllowRunDirectives    bool
}

type Package struct {
	Config Config
	Path   string
}

type Graph struct {
	Root     *Package
	Packages map[string]*Package
	Order    []*Package
}

func DiscoverAndBuildGraph(startPath string) (*Graph, error) {
	workspaceRoot, err := FindWorkspaceRoot(startPath)
	if err != nil {
		return nil, err
	}

	packages, err := DiscoverPackages(workspaceRoot)
	if err != nil {
		return nil, err
	}

	if err := DiscoverStdPackagesInto(packages, workspaceRoot); err != nil {
		return nil, err
	}

	rootPkg, err := FindPackageContaining(startPath, packages)
	if err != nil {
		return nil, err
	}

	order, err := BuildOrder(rootPkg, packages)
	if err != nil {
		return nil, err
	}

	return &Graph{
		Root:     rootPkg,
		Packages: packages,
		Order:    order,
	}, nil
}

func FindWorkspaceRoot(startPath string) (string, error) {
	abs, err := filepath.Abs(startPath)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}

	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}

	current := abs

	var nearestPackageRoot string
	var workspaceRoot string

	for {
		if fileExists(filepath.Join(current, "seal.workspace")) {
			workspaceRoot = current
		}

		if nearestPackageRoot == "" && fileExists(filepath.Join(current, "seal.toml")) {
			nearestPackageRoot = current
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}

		current = parent
	}

	if workspaceRoot != "" {
		return workspaceRoot, nil
	}

	if nearestPackageRoot != "" {
		return nearestPackageRoot, nil
	}

	return "", fmt.Errorf("no seal.workspace or seal.toml found from %s", startPath)
}

func DiscoverPackages(workspaceRoot string) (map[string]*Package, error) {
	packages := map[string]*Package{}

	if err := discoverPackagesInto(packages, workspaceRoot); err != nil {
		return nil, err
	}

	return packages, nil
}

func DiscoverStdPackagesInto(packages map[string]*Package, workspaceRoot string) error {
	seenPaths := map[string]bool{}

	for _, pkg := range packages {
		if pkg == nil {
			continue
		}

		abs, err := filepath.Abs(pkg.Path)
		if err != nil {
			return err
		}

		seenPaths[abs] = true
	}

	for _, root := range StdPackageRoots(workspaceRoot) {
		if root == "" {
			continue
		}

		absRoot, err := filepath.Abs(root)
		if err != nil {
			return err
		}

		if !dirExists(absRoot) {
			continue
		}

		if err := discoverPackagesIntoWithSeenPaths(packages, absRoot, seenPaths); err != nil {
			return err
		}
	}

	return nil
}

func discoverPackagesIntoWithSeenPaths(packages map[string]*Package, root string, seenPaths map[string]bool) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !entry.IsDir() {
			return nil
		}

		base := filepath.Base(path)
		if base == ".git" || base == "build" || base == ".seal" {
			return filepath.SkipDir
		}

		configPath := filepath.Join(path, "seal.toml")
		if !fileExists(configPath) {
			return nil
		}

		absConfigPath, err := filepath.Abs(configPath)
		if err != nil {
			return err
		}

		if seenPaths[absConfigPath] {
			return nil
		}

		cfg, err := ReadConfig(configPath)
		if err != nil {
			return err
		}

		cfg.RootDir = path

		if cfg.Name == "" {
			return fmt.Errorf("%s: package name is required", configPath)
		}

		if existing := packages[cfg.Name]; existing != nil {
			return fmt.Errorf(
				"duplicate package name %q:\n  %s\n  %s",
				cfg.Name,
				existing.Path,
				configPath,
			)
		}

		seenPaths[absConfigPath] = true

		packages[cfg.Name] = &Package{
			Config: cfg,
			Path:   configPath,
		}

		return nil
	})
}

func StdPackageRoots(workspaceRoot string) []string {
	var roots []string
	seen := map[string]bool{}

	add := func(path string) {
		if path == "" {
			return
		}

		abs, err := filepath.Abs(path)
		if err != nil {
			return
		}

		if seen[abs] {
			return
		}

		seen[abs] = true
		roots = append(roots, abs)
	}

	if explicit := os.Getenv("SEAL_STD_PATH"); explicit != "" {
		for _, part := range filepath.SplitList(explicit) {
			add(part)
		}

		return roots
	}

	// If the workspace has its own std directory, prefer it.
	workspaceStd := filepath.Join(workspaceRoot, "std")
	if dirExists(workspaceStd) {
		add(workspaceStd)
		return roots
	}

	// Installed layout:
	//   bin/sealc
	//   std/mem
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		add(filepath.Join(exeDir, "std"))
		add(filepath.Join(exeDir, "..", "std"))
	}

	// Development convenience when running from the compiler repository.
	if cwd, err := os.Getwd(); err == nil {
		for current := cwd; ; current = filepath.Dir(current) {
			add(filepath.Join(current, "std"))

			parent := filepath.Dir(current)
			if parent == current {
				break
			}
		}
	}

	return roots
}

func discoverPackagesInto(packages map[string]*Package, root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !entry.IsDir() {
			return nil
		}

		base := filepath.Base(path)
		if base == ".git" || base == "build" || base == ".seal" {
			return filepath.SkipDir
		}

		configPath := filepath.Join(path, "seal.toml")
		if !fileExists(configPath) {
			return nil
		}

		cfg, err := ReadConfig(configPath)
		if err != nil {
			return err
		}

		cfg.RootDir = path

		if cfg.Name == "" {
			return fmt.Errorf("%s: package name is required", configPath)
		}

		if existing := packages[cfg.Name]; existing != nil {
			return fmt.Errorf(
				"duplicate package name %q:\n  %s\n  %s",
				cfg.Name,
				existing.Path,
				configPath,
			)
		}

		packages[cfg.Name] = &Package{
			Config: cfg,
			Path:   configPath,
		}

		return nil
	})
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func sameOrInside(parent string, child string) (bool, error) {
	parentAbs, err := filepath.Abs(parent)
	if err != nil {
		return false, err
	}

	childAbs, err := filepath.Abs(child)
	if err != nil {
		return false, err
	}

	rel, err := filepath.Rel(parentAbs, childAbs)
	if err != nil {
		return false, err
	}

	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)), nil
}

func FindPackageContaining(path string, packages map[string]*Package) (*Package, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}

	var best *Package
	bestLen := -1

	for _, pkg := range packages {
		root, err := filepath.Abs(pkg.Config.RootDir)
		if err != nil {
			return nil, err
		}

		rel, err := filepath.Rel(root, abs)
		if err != nil {
			continue
		}

		if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
			if len(root) > bestLen {
				best = pkg
				bestLen = len(root)
			}
		}
	}

	if best == nil {
		return nil, fmt.Errorf("no package contains %s", path)
	}

	return best, nil
}

func BuildOrder(root *Package, packages map[string]*Package) ([]*Package, error) {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var order []*Package

	var visit func(pkg *Package) error

	visit = func(pkg *Package) error {
		name := pkg.Config.Name

		if visiting[name] {
			return fmt.Errorf("dependency cycle involving package %q", name)
		}

		if visited[name] {
			return nil
		}

		visiting[name] = true

		for _, dep := range pkg.Config.Dependencies {
			depPkg := packages[dep.Name]
			if depPkg == nil {
				return fmt.Errorf("package %q depends on missing package %q", name, dep.Name)
			}

			if dep.Version != "" && depPkg.Config.Version != "" && dep.Version != depPkg.Config.Version {
				return fmt.Errorf(
					"package %q requires %s@%s, but found %s@%s",
					name,
					dep.Name,
					dep.Version,
					dep.Name,
					depPkg.Config.Version,
				)
			}

			if err := visit(depPkg); err != nil {
				return err
			}
		}

		visiting[name] = false
		visited[name] = true
		order = append(order, pkg)

		return nil
	}

	if err := visit(root); err != nil {
		return nil, err
	}

	return order, nil
}

func DebugGraph(g *Graph) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("root=%s\n", g.Root.Config.Name))
	b.WriteString("order=\n")

	for _, pkg := range g.Order {
		b.WriteString(fmt.Sprintf("  %s@%s %s\n", pkg.Config.Name, pkg.Config.Version, pkg.Config.RootDir))
	}

	names := make([]string, 0, len(g.Packages))
	for name := range g.Packages {
		names = append(names, name)
	}
	sort.Strings(names)

	b.WriteString("discovered=\n")
	for _, name := range names {
		pkg := g.Packages[name]
		b.WriteString(fmt.Sprintf("  %s@%s %s\n", pkg.Config.Name, pkg.Config.Version, pkg.Config.RootDir))
	}

	return b.String()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
