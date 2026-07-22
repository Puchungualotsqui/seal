package lsp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"seal/internal/build"
	"seal/internal/checker"
	"seal/internal/diag"
	"seal/internal/frontend"
	"seal/internal/resolver"
)

var ErrStaleAnalysis = errors.New(
	"workspace analysis became stale",
)

/*
PackageSnapshot is one immutable package analysis.

ExportsStale is true when the current package contains errors and dependency
analysis must continue using the most recent successful exported package
information.
*/
type PackageSnapshot struct {
	Package *build.Package
	Result  frontend.Result

	ResolverPackage *resolver.PackageInfo
	CheckerPackage  *checker.PackageInfo

	ExportsStale bool
}

/*
WorkspaceSnapshot is an immutable analysis of every discovered package using
one specific document overlay revision.
*/
type WorkspaceSnapshot struct {
	Generation uint64

	WorkspaceRevision uint64
	DocumentRevision  uint64

	Root string

	Packages map[string]*PackageSnapshot
	Order    []string
}

func (s *WorkspaceSnapshot) Package(
	name string,
) *PackageSnapshot {
	if s == nil {
		return nil
	}

	return s.Packages[name]
}

func (s *WorkspaceSnapshot) PackageForPath(
	path string,
) *PackageSnapshot {
	if s == nil {
		return nil
	}

	var best *PackageSnapshot
	bestLength := -1

	for _, snapshot := range s.Packages {
		if snapshot == nil ||
			snapshot.Package == nil {
			continue
		}

		root :=
			snapshot.Package.Config.RootDir

		if root == "" {
			continue
		}

		inside, err :=
			pathInside(
				root,
				path,
			)

		if err != nil ||
			!inside {
			continue
		}

		absoluteRoot, err :=
			filepath.Abs(
				root,
			)

		if err != nil {
			continue
		}

		if len(absoluteRoot) >
			bestLength {
			best =
				snapshot

			bestLength =
				len(absoluteRoot)
		}
	}

	return best
}

func (s *WorkspaceSnapshot) DiagnosticsForPath(
	path string,
) []diag.Diagnostic {
	if s == nil {
		return nil
	}

	packageSnapshot :=
		s.PackageForPath(
			path,
		)

	if packageSnapshot == nil {
		return nil
	}

	return packageSnapshot.Result.DiagnosticsForFile(
		path,
	)
}

/*
Workspace owns persistent editor documents, discovered package metadata, and
the latest complete analysis snapshot.
*/
type Workspace struct {
	mu sync.RWMutex

	root string

	packages map[string]*build.Package
	order    []*build.Package

	reverseDependencies map[string][]string

	workspaceRevision uint64
	generation        uint64

	documents *DocumentStore

	snapshot *WorkspaceSnapshot
}

func NewWorkspace(
	startPath string,
) (
	*Workspace,
	error,
) {
	workspace :=
		&Workspace{
			packages: map[string]*build.Package{},

			reverseDependencies: map[string][]string{},

			documents: NewDocumentStore(),
		}

	if err :=
		workspace.refreshPackages(
			startPath,
		); err != nil {
		return nil,
			err
	}

	return workspace,
		nil
}

func (w *Workspace) Root() string {
	if w == nil {
		return ""
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.root
}

func (w *Workspace) Documents() *DocumentStore {
	if w == nil {
		return nil
	}

	return w.documents
}

func (w *Workspace) OpenDocument(
	document Document,
) error {
	if w == nil {
		return fmt.Errorf(
			"missing workspace",
		)
	}

	return w.documents.Open(
		document,
	)
}

func (w *Workspace) ChangeDocument(
	uri string,
	version int,
	text string,
) error {
	if w == nil {
		return fmt.Errorf(
			"missing workspace",
		)
	}

	return w.documents.Change(
		uri,
		version,
		text,
	)
}

func (w *Workspace) CloseDocument(
	uri string,
) (
	Document,
	bool,
) {
	if w == nil {
		return Document{},
			false
	}

	return w.documents.Close(
		uri,
	)
}

func (w *Workspace) Snapshot() *WorkspaceSnapshot {
	if w == nil {
		return nil
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.snapshot
}

/*
RefreshPackages rediscovers seal.toml files and standard-library packages.

Call it after seal.toml or seal.workspace changes.
*/
func (w *Workspace) RefreshPackages(
	startPath string,
) error {
	if w == nil {
		return fmt.Errorf(
			"missing workspace",
		)
	}

	return w.refreshPackages(
		startPath,
	)
}

func (w *Workspace) refreshPackages(
	startPath string,
) error {
	root, err :=
		build.FindWorkspaceRoot(
			startPath,
		)

	if err != nil {
		return err
	}

	packages, err :=
		build.DiscoverPackages(
			root,
		)

	if err != nil {
		return err
	}

	if err :=
		build.DiscoverStdPackagesInto(
			packages,
			root,
		); err != nil {
		return err
	}

	order, err :=
		allPackageBuildOrder(
			packages,
		)

	if err != nil {
		return err
	}

	reverseDependencies :=
		buildReverseDependencies(
			packages,
		)

	w.mu.Lock()
	defer w.mu.Unlock()

	w.root =
		root

	w.packages =
		packages

	w.order =
		order

	w.reverseDependencies =
		reverseDependencies

	w.workspaceRevision++

	return nil
}

/*
Analyze creates and atomically publishes a new workspace snapshot.

The complete document overlay is copied before analysis begins. If a document
or package manifest changes before analysis finishes, ErrStaleAnalysis is
returned and the old snapshot remains active.
*/
func (w *Workspace) Analyze(
	ctx context.Context,
) (
	*WorkspaceSnapshot,
	error,
) {
	if w == nil {
		return nil,
			fmt.Errorf(
				"missing workspace",
			)
	}

	documents :=
		w.documents.Snapshot()

	w.mu.RLock()

	root :=
		w.root

	workspaceRevision :=
		w.workspaceRevision

	packages :=
		copyPackages(
			w.packages,
		)

	order :=
		append(
			[]*build.Package(nil),
			w.order...,
		)

	previous :=
		w.snapshot

	nextGeneration :=
		w.generation + 1

	w.mu.RUnlock()

	snapshot :=
		&WorkspaceSnapshot{
			Generation: nextGeneration,

			WorkspaceRevision: workspaceRevision,

			DocumentRevision: documents.Revision,

			Root: root,

			Packages: make(
				map[string]*PackageSnapshot,
				len(packages),
			),

			Order: make(
				[]string,
				0,
				len(order),
			),
		}

	currentResolverExports :=
		map[string]*resolver.PackageInfo{}

	currentCheckerExports :=
		map[string]*checker.PackageInfo{}

	for _, pkg := range order {
		if err :=
			ctx.Err(); err != nil {
			return nil,
				err
		}

		if pkg == nil {
			continue
		}

		sources, err :=
			packageSourceInputs(
				pkg,
				packages,
				documents,
			)

		if err != nil {
			return nil,
				fmt.Errorf(
					"loading package %q: %w",
					pkg.Config.Name,
					err,
				)
		}

		resolverDependencies :=
			resolverDependenciesForPackage(
				pkg,
				currentResolverExports,
				previous,
			)

		checkerDependencies :=
			checkerDependenciesForPackage(
				pkg,
				currentCheckerExports,
				previous,
			)

		result :=
			frontend.AnalyzePackage(
				frontend.PackageInput{
					Name: pkg.Config.Name,

					Files: sources,

					CheckerOptions: checker.Options{
						GenericConstraintMaxDepth: pkg.Config.GenericConstraintMaxDepth,
					},
				},
				resolverDependencies,
				checkerDependencies,
			)

		packageSnapshot :=
			&PackageSnapshot{
				Package: pkg,

				Result: result,
			}

		if result.ResolverPackage != nil {
			packageSnapshot.ResolverPackage =
				result.ResolverPackage

			currentResolverExports[pkg.Config.Name] = result.ResolverPackage
		} else if previousExport :=
			previousResolverExport(
				previous,
				pkg.Config.Name,
			); previousExport != nil {
			packageSnapshot.ResolverPackage =
				previousExport

			packageSnapshot.ExportsStale =
				true

			currentResolverExports[pkg.Config.Name] = previousExport
		}

		if result.CheckerPackage != nil {
			packageSnapshot.CheckerPackage =
				result.CheckerPackage

			currentCheckerExports[pkg.Config.Name] = result.CheckerPackage
		} else if previousExport :=
			previousCheckerExport(
				previous,
				pkg.Config.Name,
			); previousExport != nil {
			packageSnapshot.CheckerPackage =
				previousExport

			packageSnapshot.ExportsStale =
				true

			currentCheckerExports[pkg.Config.Name] = previousExport
		}

		snapshot.Packages[pkg.Config.Name] = packageSnapshot

		snapshot.Order =
			append(
				snapshot.Order,
				pkg.Config.Name,
			)
	}

	/*
		Do not publish results produced from an old editor overlay.
	*/
	if w.documents.Revision() !=
		documents.Revision {
		return nil,
			ErrStaleAnalysis
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.workspaceRevision !=
		workspaceRevision {
		return nil,
			ErrStaleAnalysis
	}

	w.generation =
		nextGeneration

	w.snapshot =
		snapshot

	return snapshot,
		nil
}

func packageSourceInputs(
	pkg *build.Package,
	packages map[string]*build.Package,
	documents DocumentSnapshot,
) (
	[]frontend.SourceInput,
	error,
) {
	if pkg == nil {
		return nil,
			fmt.Errorf(
				"missing package",
			)
	}

	diskFiles, err :=
		build.SealFiles(
			pkg.Config.RootDir,
		)

	if err != nil {
		return nil,
			err
	}

	sourcesByPath :=
		map[string]frontend.SourceInput{}

	for _, path := range diskFiles {
		key, err :=
			canonicalPath(
				path,
			)

		if err != nil {
			return nil,
				err
		}

		if document, found :=
			documents.DocumentByPath(
				path,
			); found {
			sourcesByPath[key] =
				frontend.SourceInput{
					Path: document.Path,

					Text: document.Text,
				}

			continue
		}

		content, err :=
			os.ReadFile(
				path,
			)

		if err != nil {
			return nil,
				fmt.Errorf(
					"reading %q: %w",
					path,
					err,
				)
		}

		absolutePath, err :=
			filepath.Abs(
				path,
			)

		if err != nil {
			return nil,
				err
		}

		sourcesByPath[key] =
			frontend.SourceInput{
				Path: filepath.Clean(
					absolutePath,
				),

				Text: string(content),
			}
	}

	/*
		Add open unsaved files that are not present on disk.
	*/
	for _, document := range documents.Documents {
		if !strings.EqualFold(
			filepath.Ext(
				document.Path,
			),
			".seal",
		) {
			continue
		}

		owner, err :=
			build.FindPackageContainingPath(
				document.Path,
				packages,
			)

		if err != nil ||
			owner == nil ||
			owner.Config.Name !=
				pkg.Config.Name {
			continue
		}

		key, err :=
			canonicalPath(
				document.Path,
			)

		if err != nil {
			return nil,
				err
		}

		sourcesByPath[key] =
			frontend.SourceInput{
				Path: document.Path,

				Text: document.Text,
			}
	}

	sources :=
		make(
			[]frontend.SourceInput,
			0,
			len(sourcesByPath),
		)

	for _, sourceInput := range sourcesByPath {
		sources =
			append(
				sources,
				sourceInput,
			)
	}

	sort.Slice(
		sources,
		func(
			left int,
			right int,
		) bool {
			return sources[left].Path <
				sources[right].Path
		},
	)

	return sources,
		nil
}

func resolverDependenciesForPackage(
	pkg *build.Package,
	current map[string]*resolver.PackageInfo,
	previous *WorkspaceSnapshot,
) map[string]*resolver.PackageInfo {
	if pkg == nil ||
		len(
			pkg.Config.Dependencies,
		) == 0 {
		return nil
	}

	dependencies :=
		map[string]*resolver.PackageInfo{}

	for _, dependency := range pkg.Config.Dependencies {
		if exported :=
			current[dependency.Name]; exported != nil {
			dependencies[dependency.Name] = exported

			continue
		}

		if exported :=
			previousResolverExport(
				previous,
				dependency.Name,
			); exported != nil {
			dependencies[dependency.Name] = exported
		}
	}

	return dependencies
}

func checkerDependenciesForPackage(
	pkg *build.Package,
	current map[string]*checker.PackageInfo,
	previous *WorkspaceSnapshot,
) map[string]*checker.PackageInfo {
	if pkg == nil ||
		len(
			pkg.Config.Dependencies,
		) == 0 {
		return nil
	}

	dependencies :=
		map[string]*checker.PackageInfo{}

	for _, dependency := range pkg.Config.Dependencies {
		if exported :=
			current[dependency.Name]; exported != nil {
			dependencies[dependency.Name] = exported

			continue
		}

		if exported :=
			previousCheckerExport(
				previous,
				dependency.Name,
			); exported != nil {
			dependencies[dependency.Name] = exported
		}
	}

	return dependencies
}

func previousResolverExport(
	snapshot *WorkspaceSnapshot,
	name string,
) *resolver.PackageInfo {
	if snapshot == nil {
		return nil
	}

	packageSnapshot :=
		snapshot.Packages[name]

	if packageSnapshot == nil {
		return nil
	}

	return packageSnapshot.ResolverPackage
}

func previousCheckerExport(
	snapshot *WorkspaceSnapshot,
	name string,
) *checker.PackageInfo {
	if snapshot == nil {
		return nil
	}

	packageSnapshot :=
		snapshot.Packages[name]

	if packageSnapshot == nil {
		return nil
	}

	return packageSnapshot.CheckerPackage
}

func allPackageBuildOrder(
	packages map[string]*build.Package,
) (
	[]*build.Package,
	error,
) {
	names :=
		make(
			[]string,
			0,
			len(packages),
		)

	for name := range packages {
		names =
			append(
				names,
				name,
			)
	}

	sort.Strings(
		names,
	)

	visiting :=
		map[string]bool{}

	visited :=
		map[string]bool{}

	var order []*build.Package

	var visit func(
		name string,
	) error

	visit =
		func(
			name string,
		) error {
			if visited[name] {
				return nil
			}

			if visiting[name] {
				return fmt.Errorf(
					"dependency cycle involving package %q",
					name,
				)
			}

			pkg :=
				packages[name]

			if pkg == nil {
				return fmt.Errorf(
					"missing package %q",
					name,
				)
			}

			visiting[name] =
				true

			dependencyNames :=
				make(
					[]string,
					0,
					len(
						pkg.Config.Dependencies,
					),
				)

			for _, dependency := range pkg.Config.Dependencies {
				dependencyNames =
					append(
						dependencyNames,
						dependency.Name,
					)
			}

			sort.Strings(
				dependencyNames,
			)

			for _, dependencyName := range dependencyNames {
				if packages[dependencyName] == nil {
					return fmt.Errorf(
						"package %q depends on missing package %q",
						name,
						dependencyName,
					)
				}

				if err :=
					visit(
						dependencyName,
					); err != nil {
					return err
				}
			}

			visiting[name] =
				false

			visited[name] =
				true

			order =
				append(
					order,
					pkg,
				)

			return nil
		}

	for _, name := range names {
		if err :=
			visit(
				name,
			); err != nil {
			return nil,
				err
		}
	}

	return order,
		nil
}

func buildReverseDependencies(
	packages map[string]*build.Package,
) map[string][]string {
	reverse :=
		map[string][]string{}

	for name := range packages {
		reverse[name] =
			nil
	}

	for packageName, pkg := range packages {
		if pkg == nil {
			continue
		}

		for _, dependency := range pkg.Config.Dependencies {
			reverse[dependency.Name] = append(
				reverse[dependency.Name],
				packageName,
			)
		}
	}

	for name := range reverse {
		sort.Strings(
			reverse[name],
		)
	}

	return reverse
}

func copyPackages(
	packages map[string]*build.Package,
) map[string]*build.Package {
	copied :=
		make(
			map[string]*build.Package,
			len(packages),
		)

	for name, pkg := range packages {
		copied[name] =
			pkg
	}

	return copied
}

func pathInside(
	parent string,
	child string,
) (
	bool,
	error,
) {
	parentAbsolute, err :=
		filepath.Abs(
			parent,
		)

	if err != nil {
		return false,
			err
	}

	childAbsolute, err :=
		filepath.Abs(
			child,
		)

	if err != nil {
		return false,
			err
	}

	relative, err :=
		filepath.Rel(
			parentAbsolute,
			childAbsolute,
		)

	if err != nil {
		return false,
			err
	}

	return relative == "." ||
			(!strings.HasPrefix(
				relative,
				"..",
			) &&
				!filepath.IsAbs(
					relative,
				)),
		nil
}
