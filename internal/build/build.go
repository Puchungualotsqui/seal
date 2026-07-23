package build

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type Kind string

const (
	KindExecutable    Kind = "executable"
	KindStaticLibrary Kind = "static_library"
	KindSharedLibrary Kind = "shared_library"

	// KindLibrary is retained as a source-level compatibility alias.
	KindLibrary Kind = KindStaticLibrary
)

func normalizePackageKind(
	kind Kind,
) (Kind, error) {
	normalized :=
		strings.ToLower(
			strings.TrimSpace(
				string(kind),
			),
		)

	switch normalized {
	case "",
		"library",
		"static_library":
		return KindStaticLibrary, nil

	case "executable":
		return KindExecutable, nil

	case "shared_library":
		return KindSharedLibrary, nil

	default:
		return "",
			fmt.Errorf(
				"unknown package kind %q; expected executable, static_library, shared_library, or legacy library",
				kind,
			)
	}
}

func (kind Kind) IsLibrary() bool {
	return kind == KindStaticLibrary ||
		kind == KindSharedLibrary
}

type Dependency struct {
	Name    string
	Version string
}

type CompilerProfile struct {
	CompilerPath *string
	CompilerArgs *[]string

	CFlags      *[]string
	LinkFlags   *[]string
	IncludeDirs *[]string
	LibraryDirs *[]string
	Libraries   *[]string
	Defines     *[]string

	Target   *string
	Standard *string
	Linkage  *string
}

type NativeConfig struct {
	Sources     []string
	IncludeDirs []string
	LibraryDirs []string
	Libraries   []string
	Defines     []string
	CFlags      []string
	LinkFlags   []string
}

type Config struct {
	Name    string
	Version string
	Kind    Kind
	RootDir string

	Dependencies []Dependency

	Compiler         string
	CompilerPath     string
	CompilerArgs     []string
	CompilerProfiles map[string]CompilerProfile

	CFlags      []string
	LinkFlags   []string
	IncludeDirs []string
	LibraryDirs []string
	Libraries   []string
	Defines     []string
	Target      string
	Standard    string
	Linkage     string

	Native          NativeConfig
	NativePlatforms map[string]NativeConfig

	AutoInitializeVariables        bool
	AllowUninitializedVariables    bool
	AllowPartialInitializedStructs bool
	AllowPartialSwitches           bool

	IntegerOverflow string
	BoundsChecking  string

	FailBadStyle          bool
	AllowUnusedVariables  bool
	AllowUnusedParameters bool
	AllowRunDirectives    bool

	GenericConstraintMaxDepth int
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

func StdPackageRoots(
	workspaceRoot string,
) []string {
	var roots []string
	seen := map[string]bool{}

	add := func(path string) {
		if strings.TrimSpace(path) == "" {
			return
		}

		absolutePath, err :=
			filepath.Abs(
				path,
			)

		if err != nil {
			return
		}

		absolutePath =
			filepath.Clean(
				absolutePath,
			)

		key :=
			absolutePath

		if runtime.GOOS == "windows" {
			key =
				strings.ToLower(
					key,
				)
		}

		if seen[key] {
			return
		}

		seen[key] = true

		roots =
			append(
				roots,
				absolutePath,
			)
	}

	/*
		Explicit override. Multiple roots are permitted using the operating
		system's PATH separator.
	*/
	if explicit :=
		os.Getenv(
			"SEAL_STD_PATH",
		); explicit != "" {
		for _, part := range filepath.SplitList(
			explicit,
		) {
			add(part)
		}

		return roots
	}

	/*
		A workspace-local standard library overrides the installed copy. This
		is useful while developing the compiler and standard library together.
	*/
	if strings.TrimSpace(
		workspaceRoot,
	) != "" {
		workspaceStd :=
			filepath.Join(
				workspaceRoot,
				"std",
			)

		if dirExists(
			workspaceStd,
		) {
			add(workspaceStd)

			return roots
		}
	}

	/*
		Standard installed location:

			Windows: C:\Users\name\.seal\std
			Unix:   /home/name/.seal/std

		SEAL_HOME may override the .seal directory.
	*/
	if sealHome, err :=
		SealHome(); err == nil {
		add(
			filepath.Join(
				sealHome,
				"std",
			),
		)
	}

	/*
		Portable installed layouts:

			bin/sealc
			std/base
			std/core

		or:

			sealc
			std/base
			std/core
	*/
	if executable, err :=
		os.Executable(); err == nil {
		executableDirectory :=
			filepath.Dir(
				executable,
			)

		add(
			filepath.Join(
				executableDirectory,
				"std",
			),
		)

		add(
			filepath.Join(
				executableDirectory,
				"..",
				"std",
			),
		)
	}

	return roots
}

func SealHome() (
	string,
	error,
) {
	if explicit :=
		strings.TrimSpace(
			os.Getenv(
				"SEAL_HOME",
			),
		); explicit != "" {
		absolutePath, err :=
			filepath.Abs(
				explicit,
			)

		if err != nil {
			return "",
				err
		}

		return filepath.Clean(
			absolutePath,
		), nil
	}

	homeDirectory, err :=
		os.UserHomeDir()

	if err != nil {
		return "",
			fmt.Errorf(
				"determining Seal home directory: %w",
				err,
			)
	}

	return filepath.Join(
		homeDirectory,
		".seal",
	), nil
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

func FindPackageContaining(
	path string,
	packages map[string]*Package,
) (*Package, error) {
	abs, err :=
		filepath.Abs(
			path,
		)

	if err != nil {
		return nil, err
	}

	info, err :=
		os.Stat(
			abs,
		)

	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		abs =
			filepath.Dir(
				abs,
			)
	}

	return FindPackageContainingPath(
		abs,
		packages,
	)
}

/*
FindPackageContainingPath finds the package whose root most specifically
contains path.

Unlike FindPackageContaining, this function does not call os.Stat and therefore
works for:

  - unsaved editor documents;
  - newly created files;
  - deleted files that remain open in an editor;
  - prospective paths that do not exist yet.

The function treats a path ending in a separator as a directory. Otherwise, a
path with a .seal suffix is treated as a source file and its parent directory is
used for containment checks.

For other nonexistent paths, the path itself is checked first. This allows both
directory-like and file-like paths to work when their intended form can be
determined from package containment.
*/
func FindPackageContainingPath(
	path string,
	packages map[string]*Package,
) (*Package, error) {
	if strings.TrimSpace(path) == "" {
		return nil,
			fmt.Errorf(
				"package lookup path cannot be empty",
			)
	}

	abs, err :=
		filepath.Abs(
			path,
		)

	if err != nil {
		return nil, err
	}

	abs =
		filepath.Clean(
			abs,
		)

	candidates :=
		packageContainmentCandidates(
			abs,
		)

	var best *Package
	bestLength := -1

	for _, candidate := range candidates {
		for _, pkg := range packages {
			if pkg == nil {
				continue
			}

			root :=
				pkg.Config.RootDir

			if strings.TrimSpace(root) == "" {
				if pkg.Path == "" {
					continue
				}

				root =
					filepath.Dir(
						pkg.Path,
					)
			}

			rootAbs, err :=
				filepath.Abs(
					root,
				)

			if err != nil {
				return nil, err
			}

			rootAbs =
				filepath.Clean(
					rootAbs,
				)

			inside, err :=
				sameOrInside(
					rootAbs,
					candidate,
				)

			if err != nil {
				continue
			}

			if !inside {
				continue
			}

			if len(rootAbs) >
				bestLength {
				best = pkg
				bestLength =
					len(rootAbs)
			}
		}

		/*
			The first candidate is the most literal interpretation of the path.
			Only try its parent when no package contains that path.
		*/
		if best != nil {
			break
		}
	}

	if best == nil {
		return nil,
			fmt.Errorf(
				"no package contains %s",
				path,
			)
	}

	return best, nil
}

/*
packageContainmentCandidates returns possible directories to use for package
containment.

A Seal source path is unambiguously a file path, even when the file does not
exist. Other paths are first treated literally and then as possible file paths.
*/
func packageContainmentCandidates(
	path string,
) []string {
	path =
		filepath.Clean(
			path,
		)

	if strings.EqualFold(
		filepath.Ext(path),
		".seal",
	) {
		return []string{
			filepath.Dir(
				path,
			),
		}
	}

	parent :=
		filepath.Dir(
			path,
		)

	if parent == path {
		return []string{
			path,
		}
	}

	return []string{
		path,
		parent,
	}
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
