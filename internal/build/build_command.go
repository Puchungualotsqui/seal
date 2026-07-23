package build

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"seal/internal/checker"
	cgen "seal/internal/codegen"
	"seal/internal/diag"
	"seal/internal/resolver"
)

const maxGenericRequestIterations = 64

type BuildOptions struct {
	EmitOnly bool
	OutDir   string
	Output   string

	// Compiler temporarily overrides [build].compiler.
	// It does not modify seal.toml.
	Compiler string
}

type BuildResult struct {
	Graph    *Graph
	Packages []*LoadedPackage
	OutDir   string
	Output   string
}

func BuildWorkspace(
	startPath string,
	options BuildOptions,
) (*BuildResult, error) {
	graph, err :=
		DiscoverAndBuildGraph(
			startPath,
		)

	if err != nil {
		return nil, err
	}

	outDir :=
		options.OutDir

	if outDir == "" {
		outDir =
			filepath.Join(
				graph.Root.Config.RootDir,
				".seal",
				"build",
			)
	}

	if err :=
		os.MkdirAll(
			outDir,
			0755,
		); err != nil {
		return nil, err
	}

	var loaded []*LoadedPackage

	resolverPackages :=
		map[string]*resolver.PackageInfo{}

	checkerPackages :=
		map[string]*checker.PackageInfo{}

	codegenPackages :=
		map[string]*cgen.PackageInfo{}

	// Phase 1: load, resolve, check, and export package signatures.
	for _, pkg := range graph.Order {
		reporter :=
			diag.NewReporter()

		file,
			resolverScope,
			checkerScope,
			semantic,
			err :=
			LoadAndCheckPackageWithSemanticInfo(
				pkg,
				reporter,
				resolverPackages,
				checkerPackages,
			)

		if err != nil {
			return nil,
				withDiagnostics(
					err,
					reporter,
				)
		}

		resolverInfo :=
			resolver.ExportPackage(
				pkg.Config.Name,
				resolverScope,
			)

		checkerInfo :=
			checker.ExportPackage(
				pkg.Config.Name,
				checkerScope,
			)

		dependencyCodegenPackages,
			err :=
			codegenPackagesForPackage(
				pkg,
				codegenPackages,
			)

		if err != nil {
			return nil, err
		}

		codegenInfo :=
			emptyCodegenPackageInfo(
				pkg.Config.Name,
			)

		if file != nil &&
			len(file.Decls) > 0 {
			codegenInfo =
				cgen.ExportPackageInfoWithSemanticInfo(
					pkg.Config.Name,
					file,
					reporter,
					dependencyCodegenPackages,
					semantic,
				)

			if reporter.HasErrors() {
				return nil,
					withDiagnostics(
						fmt.Errorf(
							"C package export failed for package %q",
							pkg.Config.Name,
						),
						reporter,
					)
			}
		}

		nativeCFiles, err :=
			CFiles(
				pkg.Config.RootDir,
			)

		if err != nil {
			return nil, err
		}

		cPath :=
			filepath.Join(
				outDir,
				sanitizeFileName(
					pkg.Config.Name,
				)+".c",
			)

		resolverPackages[pkg.Config.Name] =
			resolverInfo

		checkerPackages[pkg.Config.Name] =
			checkerInfo

		codegenPackages[pkg.Config.Name] =
			codegenInfo

		loaded = append(
			loaded,
			&LoadedPackage{
				Package:      pkg,
				File:         file,
				CPath:        cPath,
				NativeCFiles: nativeCFiles,
				CodegenInfo:  codegenInfo,
				SemanticInfo: semantic,
			},
		)
	}

	// Phase 2: generate C with cross-package generic instance requests.
	if err :=
		generateWorkspaceCWithGenericRequests(
			graph,
			loaded,
			codegenPackages,
		); err != nil {
		return nil, err
	}

	// Phase 3: write final fixed-point C output.
	for _, pkg := range loaded {
		if err :=
			os.WriteFile(
				pkg.CPath,
				[]byte(pkg.CCode),
				0644,
			); err != nil {
			return nil, err
		}
	}

	compilerConfig :=
		resolveCompilerConfig(
			graph.Root.Config,
			options.Compiler,
		)

	platform :=
		buildPlatform(
			compilerConfig.Target,
		)

	output :=
		options.Output

	if output == "" {
		output =
			defaultArtifactPath(
				outDir,
				graph.Root.Config.Name,
				graph.Root.Config.Kind,
				platform,
				compilerConfig,
			)
	} else if graph.Root.Config.Kind ==
		KindExecutable &&
		platform == "windows" &&
		filepath.Ext(output) == "" {
		output += ".exe"
	}

	if !options.EmitOnly {
		outputDirectory :=
			filepath.Dir(
				output,
			)

		if err :=
			os.MkdirAll(
				outputDirectory,
				0755,
			); err != nil {
			return nil,
				fmt.Errorf(
					"creating output directory %q: %w",
					outputDirectory,
					err,
				)
		}

		if err :=
			compileArtifact(
				graph,
				loaded,
				output,
				options.Compiler,
				outDir,
			); err != nil {
			return nil, err
		}
	}

	return &BuildResult{
		Graph:    graph,
		Packages: loaded,
		OutDir:   outDir,
		Output:   output,
	}, nil
}

func RunWorkspace(
	startPath string,
	options BuildOptions,
	programArgs []string,
) (*BuildResult, error) {
	if options.EmitOnly {
		return nil, fmt.Errorf(
			"run cannot be used with emit-only mode",
		)
	}

	// Running always requires a compiled executable.
	options.EmitOnly = false

	result, err := BuildWorkspace(
		startPath,
		options,
	)
	if err != nil {
		return nil, err
	}

	if result == nil {
		return nil, fmt.Errorf(
			"build completed without a result",
		)
	}

	if result.Graph == nil ||
		result.Graph.Root == nil {
		return nil, fmt.Errorf(
			"build completed without a root package",
		)
	}

	if result.Graph.Root.Config.Kind != KindExecutable {
		return nil, fmt.Errorf(
			"cannot run package %q because it is a %s package",
			result.Graph.Root.Config.Name,
			result.Graph.Root.Config.Kind,
		)
	}

	executablePath, err := filepath.Abs(
		result.Output,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"resolving executable path %q: %w",
			result.Output,
			err,
		)
	}

	cmd := exec.Command(
		executablePath,
		programArgs...,
	)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError

		if errors.As(err, &exitErr) {
			// Return the original ExitError so the CLI can preserve the
			// program's exit status.
			return result, exitErr
		}

		return result, fmt.Errorf(
			"failed to run %q: %w",
			executablePath,
			err,
		)
	}

	return result, nil
}

func emptyCodegenPackageInfo(name string) *cgen.PackageInfo {
	return &cgen.PackageInfo{
		Name:      name,
		Tasks:     map[string]cgen.TaskInfo{},
		Overloads: map[string][]string{},
	}
}

func generateWorkspaceCWithGenericRequests(
	graph *Graph,
	loaded []*LoadedPackage,
	codegenPackages map[string]*cgen.PackageInfo,
) error {
	requestsByPackage :=
		map[string]*cgen.GenericInstanceRequestSet{}

	for _, loadedPkg := range loaded {
		if loadedPkg == nil ||
			loadedPkg.Package == nil {
			continue
		}

		requestsByPackage[loadedPkg.Package.Config.Name] = cgen.NewGenericInstanceRequestSet()
	}

	for iteration := 0; iteration < maxGenericRequestIterations; iteration++ {
		changed := false

		for _, loadedPkg := range loaded {
			if loadedPkg == nil ||
				loadedPkg.Package == nil {
				continue
			}

			pkg := loadedPkg.Package
			pkgName := pkg.Config.Name

			if loadedPkg.File == nil ||
				len(loadedPkg.File.Decls) == 0 {
				loadedPkg.CCode = fmt.Sprintf(
					"/* empty package %s */\n",
					pkgName,
				)

				continue
			}

			depPackages, err :=
				codegenPackagesForPackage(
					pkg,
					codegenPackages,
				)

			if err != nil {
				return err
			}

			reporter := diag.NewReporter()

			g :=
				cgen.NewWithPackagesAndSemanticInfo(
					reporter,
					pkgName,
					depPackages,
					loadedPkg.SemanticInfo,
				)

			// The ordinary package map above intentionally contains only source-level
			// dependencies. Generic requests may additionally carry a concrete type
			// owned by a dependant package, so CGen also receives read-only metadata for
			// the complete workspace.
			g.SetWorkspacePackages(
				codegenPackages,
			)

			if set :=
				requestsByPackage[pkgName]; set != nil {
				g.AddRequestedInstances(
					set.List(),
				)
			}

			cCode :=
				g.Generate(loadedPkg.File)

			if reporter.HasErrors() {
				return withDiagnostics(
					fmt.Errorf(
						"C generation failed for package %q",
						pkgName,
					),
					reporter,
				)
			}

			loadedPkg.CCode = cCode

			for _, req := range g.RequestedGenericInstances() {
				if req.PackageName == "" {
					continue
				}

				if codegenPackages[req.PackageName] == nil {
					return fmt.Errorf(
						"package %q generated generic instance request for missing package %q",
						pkgName,
						req.PackageName,
					)
				}

				set :=
					requestsByPackage[req.PackageName]

				if set == nil {
					set =
						cgen.NewGenericInstanceRequestSet()

					requestsByPackage[req.PackageName] = set
				}

				if set.Add(req) {
					changed = true
				}
			}
		}

		if !changed {
			return nil
		}
	}

	return fmt.Errorf(
		"generic instance request fixed point did not converge after %d iterations",
		maxGenericRequestIterations,
	)
}

func codegenPackagesForPackage(
	pkg *Package,
	codegenPackages map[string]*cgen.PackageInfo,
) (map[string]*cgen.PackageInfo, error) {
	if pkg == nil {
		return nil, fmt.Errorf("missing package")
	}

	if len(pkg.Config.Dependencies) == 0 {
		return nil, nil
	}

	out := map[string]*cgen.PackageInfo{}

	for _, dep := range pkg.Config.Dependencies {
		info := codegenPackages[dep.Name]
		if info == nil {
			return nil, fmt.Errorf(
				"package %q depends on %q, but no codegen package info was exported",
				pkg.Config.Name,
				dep.Name,
			)
		}

		out[dep.Name] = info
	}

	return out, nil
}

func compileArtifact(
	graph *Graph,
	loaded []*LoadedPackage,
	output string,
	compilerOverride string,
	outDir string,
) error {
	if graph == nil ||
		graph.Root == nil {
		return fmt.Errorf(
			"missing root package",
		)
	}

	compilerConfig :=
		resolveCompilerConfig(
			graph.Root.Config,
			compilerOverride,
		)

	platform :=
		buildPlatform(
			compilerConfig.Target,
		)

	nativeConfig, err :=
		resolveWorkspaceNativeConfig(
			graph,
			platform,
		)

	if err != nil {
		return err
	}

	applyNativeConfig(
		&compilerConfig,
		nativeConfig,
	)

	compilerPath,
		compilerArgs,
		err :=
		compilerCommand(
			compilerConfig,
		)

	if err != nil {
		return err
	}

	sources :=
		collectBuildSources(
			loaded,
			nativeConfig.Sources,
		)

	if len(sources) == 0 {
		return fmt.Errorf(
			"package %q has no C sources to build",
			graph.Root.Config.Name,
		)
	}

	objectDirectory :=
		filepath.Join(
			outDir,
			"obj",
		)

	if err :=
		os.RemoveAll(
			objectDirectory,
		); err != nil {
		return fmt.Errorf(
			"cleaning object directory %q: %w",
			objectDirectory,
			err,
		)
	}

	if err :=
		os.MkdirAll(
			objectDirectory,
			0755,
		); err != nil {
		return fmt.Errorf(
			"creating object directory %q: %w",
			objectDirectory,
			err,
		)
	}

	shared :=
		graph.Root.Config.Kind ==
			KindSharedLibrary

	objects := make(
		[]string,
		0,
		len(sources),
	)

	for index, source := range sources {
		object :=
			objectPath(
				objectDirectory,
				source,
				index,
				compilerConfig,
			)

		if err :=
			compileSource(
				compilerConfig,
				compilerPath,
				compilerArgs,
				source,
				object,
				platform,
				shared,
			); err != nil {
			return err
		}

		objects = append(
			objects,
			object,
		)
	}

	switch graph.Root.Config.Kind {
	case KindExecutable:
		return linkExecutable(
			compilerConfig,
			compilerPath,
			compilerArgs,
			objects,
			output,
		)

	case KindStaticLibrary:
		return archiveStaticLibrary(
			compilerConfig,
			compilerPath,
			compilerArgs,
			objects,
			output,
		)

	case KindSharedLibrary:
		return linkSharedLibrary(
			compilerConfig,
			compilerPath,
			compilerArgs,
			objects,
			output,
			platform,
		)

	default:
		return fmt.Errorf(
			"unsupported package kind %q",
			graph.Root.Config.Kind,
		)
	}
}

func resolveWorkspaceNativeConfig(
	graph *Graph,
	platform string,
) (NativeConfig, error) {
	if graph == nil {
		return NativeConfig{},
			fmt.Errorf(
				"missing package graph",
			)
	}

	packageConfigs := make(
		[]NativeConfig,
		0,
		len(graph.Order),
	)

	var resolved NativeConfig

	for _, pkg := range graph.Order {
		if pkg == nil {
			continue
		}

		contributions :=
			[]NativeConfig{
				pkg.Config.Native,
			}

		if platformConfig, ok :=
			pkg.Config.NativePlatforms[platform]; ok {
			contributions = append(
				contributions,
				platformConfig,
			)
		}

		var packageConfig NativeConfig

		for _, contribution := range contributions {
			resolvedContribution,
				err :=
				resolvePackageNativeConfig(
					pkg,
					contribution,
				)

			if err != nil {
				return NativeConfig{}, err
			}

			mergeNativeConfig(
				&packageConfig,
				resolvedContribution,
			)
		}

		packageConfigs = append(
			packageConfigs,
			packageConfig,
		)

		resolved.Sources =
			appendUniqueStrings(
				resolved.Sources,
				packageConfig.Sources...,
			)

		resolved.IncludeDirs =
			appendUniqueStrings(
				resolved.IncludeDirs,
				packageConfig.IncludeDirs...,
			)

		resolved.LibraryDirs =
			appendUniqueStrings(
				resolved.LibraryDirs,
				packageConfig.LibraryDirs...,
			)

		resolved.Defines =
			appendUniqueStrings(
				resolved.Defines,
				packageConfig.Defines...,
			)

		resolved.CFlags = append(
			resolved.CFlags,
			packageConfig.CFlags...,
		)
	}

	/*
		Link requirements are emitted from dependants to dependencies.

		For a one-pass native linker, a library that uses another library
		generally needs to appear before the library it uses.
	*/
	for index :=
		len(packageConfigs) - 1; index >= 0; index-- {
		packageConfig :=
			packageConfigs[index]

		resolved.Libraries = append(
			resolved.Libraries,
			packageConfig.Libraries...,
		)

		resolved.LinkFlags = append(
			resolved.LinkFlags,
			packageConfig.LinkFlags...,
		)
	}

	return resolved, nil
}

func resolvePackageNativeConfig(
	pkg *Package,
	config NativeConfig,
) (NativeConfig, error) {
	if pkg == nil {
		return NativeConfig{},
			fmt.Errorf(
				"missing package while resolving native configuration",
			)
	}

	root :=
		strings.TrimSpace(
			pkg.Config.RootDir,
		)

	if root == "" &&
		pkg.Path != "" {
		root =
			filepath.Dir(
				pkg.Path,
			)
	}

	if root == "" {
		return NativeConfig{},
			fmt.Errorf(
				"package %q has no root directory",
				pkg.Config.Name,
			)
	}

	sources, err :=
		resolveNativeSources(
			pkg.Config.Name,
			root,
			config.Sources,
		)

	if err != nil {
		return NativeConfig{}, err
	}

	return NativeConfig{
		Sources: sources,

		IncludeDirs: resolveNativePaths(
			root,
			config.IncludeDirs,
		),

		LibraryDirs: resolveNativePaths(
			root,
			config.LibraryDirs,
		),

		Libraries: append(
			[]string(nil),
			config.Libraries...,
		),

		Defines: append(
			[]string(nil),
			config.Defines...,
		),

		CFlags: append(
			[]string(nil),
			config.CFlags...,
		),

		LinkFlags: append(
			[]string(nil),
			config.LinkFlags...,
		),
	}, nil
}

func resolveNativeSources(
	packageName string,
	root string,
	patterns []string,
) ([]string, error) {
	var sources []string

	for _, pattern := range patterns {
		pattern =
			strings.TrimSpace(
				pattern,
			)

		if pattern == "" {
			return nil,
				fmt.Errorf(
					"package %q has an empty native source path",
					packageName,
				)
		}

		resolvedPattern :=
			resolveNativePath(
				root,
				pattern,
			)

		matches :=
			[]string{
				resolvedPattern,
			}

		if strings.ContainsAny(
			resolvedPattern,
			"*?[",
		) {
			expanded, err :=
				filepath.Glob(
					resolvedPattern,
				)

			if err != nil {
				return nil,
					fmt.Errorf(
						"package %q has invalid native source pattern %q: %w",
						packageName,
						pattern,
						err,
					)
			}

			matches = expanded
		}

		if len(matches) == 0 {
			return nil,
				fmt.Errorf(
					"package %q native source pattern %q matched no files",
					packageName,
					pattern,
				)
		}

		sort.Strings(
			matches,
		)

		matchedFiles := 0

		for _, match := range matches {
			info, err :=
				os.Stat(
					match,
				)

			if err != nil {
				return nil,
					fmt.Errorf(
						"package %q native source %q: %w",
						packageName,
						match,
						err,
					)
			}

			if info.IsDir() {
				continue
			}

			absolute, err :=
				filepath.Abs(
					match,
				)

			if err != nil {
				return nil,
					fmt.Errorf(
						"resolving native source %q: %w",
						match,
						err,
					)
			}

			sources = append(
				sources,
				filepath.Clean(
					absolute,
				),
			)

			matchedFiles++
		}

		if matchedFiles == 0 {
			return nil,
				fmt.Errorf(
					"package %q native source pattern %q matched no files",
					packageName,
					pattern,
				)
		}
	}

	return appendUniqueStrings(
		nil,
		sources...,
	), nil
}

func resolveNativePaths(
	root string,
	paths []string,
) []string {
	resolved := make(
		[]string,
		0,
		len(paths),
	)

	for _, path := range paths {
		path =
			strings.TrimSpace(
				path,
			)

		if path == "" {
			continue
		}

		resolved = append(
			resolved,
			resolveNativePath(
				root,
				path,
			),
		)
	}

	return appendUniqueStrings(
		nil,
		resolved...,
	)
}

func resolveNativePath(
	root string,
	path string,
) string {
	if filepath.IsAbs(
		path,
	) {
		return filepath.Clean(
			path,
		)
	}

	return filepath.Clean(
		filepath.Join(
			root,
			path,
		),
	)
}

func mergeNativeConfig(
	target *NativeConfig,
	contribution NativeConfig,
) {
	if target == nil {
		return
	}

	target.Sources =
		appendUniqueStrings(
			target.Sources,
			contribution.Sources...,
		)

	target.IncludeDirs =
		appendUniqueStrings(
			target.IncludeDirs,
			contribution.IncludeDirs...,
		)

	target.LibraryDirs =
		appendUniqueStrings(
			target.LibraryDirs,
			contribution.LibraryDirs...,
		)

	target.Defines =
		appendUniqueStrings(
			target.Defines,
			contribution.Defines...,
		)

	target.Libraries = append(
		target.Libraries,
		contribution.Libraries...,
	)

	target.CFlags = append(
		target.CFlags,
		contribution.CFlags...,
	)

	target.LinkFlags = append(
		target.LinkFlags,
		contribution.LinkFlags...,
	)
}

func applyNativeConfig(
	config *Config,
	native NativeConfig,
) {
	if config == nil {
		return
	}

	config.IncludeDirs =
		appendUniqueStrings(
			config.IncludeDirs,
			native.IncludeDirs...,
		)

	config.LibraryDirs =
		appendUniqueStrings(
			config.LibraryDirs,
			native.LibraryDirs...,
		)

	config.Defines =
		appendUniqueStrings(
			config.Defines,
			native.Defines...,
		)

	config.Libraries = append(
		config.Libraries,
		native.Libraries...,
	)

	config.CFlags = append(
		config.CFlags,
		native.CFlags...,
	)

	config.LinkFlags = append(
		config.LinkFlags,
		native.LinkFlags...,
	)
}

func appendUniqueStrings(
	existing []string,
	values ...string,
) []string {
	result :=
		append(
			[]string(nil),
			existing...,
		)

	seen := make(
		map[string]bool,
		len(result)+len(values),
	)

	for _, value := range result {
		seen[value] = true
	}

	for _, value := range values {
		if seen[value] {
			continue
		}

		seen[value] = true

		result = append(
			result,
			value,
		)
	}

	return result
}

func collectBuildSources(
	loaded []*LoadedPackage,
	explicitNativeSources []string,
) []string {
	var sources []string

	add := func(
		path string,
	) {
		path =
			strings.TrimSpace(
				path,
			)

		if path == "" {
			return
		}

		absolute, err :=
			filepath.Abs(
				path,
			)

		if err == nil {
			path = absolute
		}

		sources = append(
			sources,
			filepath.Clean(
				path,
			),
		)
	}

	for _, loadedPackage := range loaded {
		if loadedPackage == nil {
			continue
		}

		add(
			loadedPackage.CPath,
		)

		for _, native := range loadedPackage.NativeCFiles {
			add(
				native,
			)
		}
	}

	for _, native := range explicitNativeSources {
		add(
			native,
		)
	}

	return appendUniqueStrings(
		nil,
		sources...,
	)
}

func objectPath(
	objectDirectory string,
	source string,
	index int,
	config Config,
) string {
	extension := ".o"

	if normalizedCompilerName(
		config,
	) == "msvc" {
		extension = ".obj"
	}

	base :=
		strings.TrimSuffix(
			filepath.Base(
				source,
			),
			filepath.Ext(
				source,
			),
		)

	return filepath.Join(
		objectDirectory,
		fmt.Sprintf(
			"%04d_%s%s",
			index,
			sanitizeFileName(
				base,
			),
			extension,
		),
	)
}

func compileSource(
	config Config,
	compilerPath string,
	compilerArgs []string,
	source string,
	object string,
	platform string,
	shared bool,
) error {
	args :=
		append(
			[]string(nil),
			compilerArgs...,
		)

	if normalizedCompilerName(
		config,
	) == "msvc" {
		args = append(
			args,
			"/nologo",
		)

		args = append(
			args,
			compilerCompileArgs(
				config,
				platform,
				shared,
			)...,
		)

		args = append(
			args,
			"/c",
			source,
			"/Fo"+object,
		)
	} else {
		args = append(
			args,
			compilerCompileArgs(
				config,
				platform,
				shared,
			)...,
		)

		args = append(
			args,
			"-c",
			source,
			"-o",
			object,
		)
	}

	return runBuildCommand(
		"C compilation",
		compilerPath,
		args,
	)
}

func linkExecutable(
	config Config,
	compilerPath string,
	compilerArgs []string,
	objects []string,
	output string,
) error {
	args :=
		append(
			[]string(nil),
			compilerArgs...,
		)

	if normalizedCompilerName(
		config,
	) == "msvc" {
		args = append(
			args,
			"/nologo",
		)

		args = append(
			args,
			objects...,
		)

		args = append(
			args,
			"/Fe:"+output,
		)

		args = append(
			args,
			compilerLinkArgs(
				config,
			)...,
		)
	} else {
		args = append(
			args,
			objects...,
		)

		args = append(
			args,
			compilerLinkArgs(
				config,
			)...,
		)

		args = append(
			args,
			"-o",
			output,
		)
	}

	return runBuildCommand(
		"C linker",
		compilerPath,
		args,
	)
}

func linkSharedLibrary(
	config Config,
	compilerPath string,
	compilerArgs []string,
	objects []string,
	output string,
	platform string,
) error {
	args :=
		append(
			[]string(nil),
			compilerArgs...,
		)

	if normalizedCompilerName(
		config,
	) == "msvc" {
		args = append(
			args,
			"/nologo",
			"/LD",
		)

		args = append(
			args,
			objects...,
		)

		args = append(
			args,
			"/Fe:"+output,
		)

		args = append(
			args,
			compilerLinkArgs(
				config,
			)...,
		)
	} else {
		switch platform {
		case "macos":
			args = append(
				args,
				"-dynamiclib",
			)

		default:
			args = append(
				args,
				"-shared",
			)
		}

		args = append(
			args,
			objects...,
		)

		args = append(
			args,
			compilerLinkArgs(
				config,
			)...,
		)

		args = append(
			args,
			"-o",
			output,
		)
	}

	return runBuildCommand(
		"shared-library linker",
		compilerPath,
		args,
	)
}

func archiveStaticLibrary(
	config Config,
	compilerPath string,
	compilerArgs []string,
	objects []string,
	output string,
) error {
	compilerName :=
		normalizedCompilerName(
			config,
		)

	switch compilerName {
	case "msvc":
		args :=
			[]string{
				"/nologo",
				"/OUT:" + output,
			}

		args = append(
			args,
			objects...,
		)

		return runBuildCommand(
			"static-library archiver",
			"lib",
			args,
		)

	case "tcc":
		args :=
			append(
				[]string(nil),
				compilerArgs...,
			)

		args = append(
			args,
			"-ar",
			"rcs",
			output,
		)

		args = append(
			args,
			objects...,
		)

		return runBuildCommand(
			"static-library archiver",
			compilerPath,
			args,
		)

	case "zigcc":
		args :=
			[]string{
				"ar",
				"rcs",
				output,
			}

		args = append(
			args,
			objects...,
		)

		return runBuildCommand(
			"static-library archiver",
			compilerPath,
			args,
		)

	default:
		archiverPath :=
			inferArchiverPath(
				compilerPath,
			)

		args :=
			[]string{
				"rcs",
				output,
			}

		args = append(
			args,
			objects...,
		)

		return runBuildCommand(
			"static-library archiver",
			archiverPath,
			args,
		)
	}
}

func inferArchiverPath(
	compilerPath string,
) string {
	directory :=
		filepath.Dir(
			compilerPath,
		)

	base :=
		filepath.Base(
			compilerPath,
		)

	extension :=
		filepath.Ext(
			base,
		)

	name :=
		strings.TrimSuffix(
			base,
			extension,
		)

	candidateName := ""

	switch {
	case strings.HasSuffix(
		name,
		"gcc",
	):
		candidateName =
			strings.TrimSuffix(
				name,
				"gcc",
			) +
				"ar" +
				extension

	case strings.HasSuffix(
		name,
		"clang",
	):
		candidateName =
			strings.TrimSuffix(
				name,
				"clang",
			) +
				"llvm-ar" +
				extension
	}

	if candidateName != "" {
		candidate :=
			candidateName

		if directory != "." &&
			directory != "" {
			candidate =
				filepath.Join(
					directory,
					candidateName,
				)
		}

		if found, err :=
			exec.LookPath(
				candidate,
			); err == nil {
			return found
		}
	}

	return "ar"
}

func runBuildCommand(
	operation string,
	commandPath string,
	args []string,
) error {
	cmd :=
		exec.Command(
			commandPath,
			args...,
		)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf(
			"%s failed: %w\ncommand: %s %s",
			operation,
			err,
			commandPath,
			strings.Join(
				args,
				" ",
			),
		)
	}

	return nil
}

func compilerCompileArgs(
	config Config,
	platform string,
	shared bool,
) []string {
	compilerName :=
		normalizedCompilerName(
			config,
		)

	var args []string

	if config.Standard != "" {
		switch compilerName {
		case "msvc":
			args = append(
				args,
				"/std:"+config.Standard,
			)

		default:
			args = append(
				args,
				"-std="+config.Standard,
			)
		}
	}

	if config.Target != "" &&
		compilerName == "zigcc" {
		args = append(
			args,
			"-target",
			config.Target,
		)
	}

	switch compilerName {
	case "msvc":
		for _, directory := range config.IncludeDirs {
			args = append(
				args,
				"/I"+directory,
			)
		}

		for _, define := range config.Defines {
			args = append(
				args,
				"/D"+define,
			)
		}

	default:
		for _, directory := range config.IncludeDirs {
			args = append(
				args,
				"-I"+directory,
			)
		}

		for _, define := range config.Defines {
			args = append(
				args,
				"-D"+define,
			)
		}

		if shared &&
			platform != "windows" &&
			!containsArgument(
				config.CFlags,
				"-fPIC",
			) {
			args = append(
				args,
				"-fPIC",
			)
		}
	}

	args = append(
		args,
		config.CFlags...,
	)

	return args
}

func compilerLinkArgs(
	config Config,
) []string {
	compilerName :=
		normalizedCompilerName(
			config,
		)

	if compilerName == "msvc" {
		var linkerArgs []string

		for _, directory := range config.LibraryDirs {
			linkerArgs = append(
				linkerArgs,
				"/LIBPATH:"+directory,
			)
		}

		for _, library := range config.Libraries {
			linkerArgs = append(
				linkerArgs,
				msvcLibraryArgument(
					library,
				),
			)
		}

		linkerArgs = append(
			linkerArgs,
			config.LinkFlags...,
		)

		if len(linkerArgs) == 0 {
			return nil
		}

		return append(
			[]string{
				"/link",
			},
			linkerArgs...,
		)
	}

	var args []string

	for _, directory := range config.LibraryDirs {
		args = append(
			args,
			"-L"+directory,
		)
	}

	for _, library := range config.Libraries {
		args = append(
			args,
			gccLibraryArgument(
				library,
			),
		)
	}

	args = append(
		args,
		config.LinkFlags...,
	)

	return args
}

func compilerConfigArgs(
	config Config,
) []string {
	args :=
		compilerCompileArgs(
			config,
			buildPlatform(
				config.Target,
			),
			false,
		)

	args = append(
		args,
		compilerLinkArgs(
			config,
		)...,
	)

	return args
}

func gccLibraryArgument(
	library string,
) string {
	if strings.HasPrefix(
		library,
		"-",
	) ||
		filepath.IsAbs(
			library,
		) ||
		strings.ContainsAny(
			library,
			`/\`,
		) ||
		filepath.Ext(
			library,
		) != "" {
		return library
	}

	return "-l" + library
}

func msvcLibraryArgument(
	library string,
) string {
	if filepath.Ext(
		library,
	) != "" {
		return library
	}

	return library + ".lib"
}

func containsArgument(
	args []string,
	expected string,
) bool {
	for _, arg := range args {
		if arg == expected {
			return true
		}
	}

	return false
}

func buildPlatform(
	target string,
) string {
	normalizedTarget :=
		strings.ToLower(
			strings.TrimSpace(
				target,
			),
		)

	if normalizedTarget != "" {
		switch {
		case strings.Contains(
			normalizedTarget,
			"windows",
		),
			strings.Contains(
				normalizedTarget,
				"mingw",
			),
			strings.Contains(
				normalizedTarget,
				"w64",
			):
			return "windows"

		case strings.Contains(
			normalizedTarget,
			"linux",
		):
			return "linux"

		case strings.Contains(
			normalizedTarget,
			"darwin",
		),
			strings.Contains(
				normalizedTarget,
				"macos",
			),
			strings.Contains(
				normalizedTarget,
				"apple",
			):
			return "macos"

		case strings.Contains(
			normalizedTarget,
			"freebsd",
		):
			return "freebsd"
		}
	}

	switch runtime.GOOS {
	case "darwin":
		return "macos"

	default:
		return runtime.GOOS
	}
}

func defaultArtifactPath(
	outDirectory string,
	packageName string,
	kind Kind,
	platform string,
	config Config,
) string {
	packageName =
		sanitizeFileName(
			packageName,
		)

	switch kind {
	case KindExecutable:
		if platform == "windows" {
			packageName += ".exe"
		}

	case KindStaticLibrary:
		if platform == "windows" &&
			normalizedCompilerName(
				config,
			) == "msvc" {
			packageName += ".lib"
		} else {
			packageName =
				"lib" +
					packageName +
					".a"
		}

	case KindSharedLibrary:
		switch platform {
		case "windows":
			packageName += ".dll"

		case "macos":
			packageName =
				"lib" +
					packageName +
					".dylib"

		default:
			packageName =
				"lib" +
					packageName +
					".so"
		}
	}

	return filepath.Join(
		outDirectory,
		packageName,
	)
}

func resolveCompilerConfig(cfg Config, compilerOverride string) Config {
	out := cfg
	selected := strings.TrimSpace(compilerOverride)
	if selected == "" {
		selected = strings.TrimSpace(cfg.Compiler)
	}
	if selected == "" {
		selected = "cc"
	}
	selected = normalizeCompilerProfileName(selected)
	out.Compiler = selected
	if profile, ok := cfg.CompilerProfiles["default"]; ok {
		applyCompilerProfile(&out, profile)
	}
	if profile, ok := cfg.CompilerProfiles[selected]; ok {
		applyCompilerProfile(&out, profile)
	}
	return out
}

func applyCompilerProfile(cfg *Config, profile CompilerProfile) {
	if cfg == nil {
		return
	}
	if profile.CompilerPath != nil {
		cfg.CompilerPath = *profile.CompilerPath
	}
	if profile.CompilerArgs != nil {
		cfg.CompilerArgs = append([]string(nil), (*profile.CompilerArgs)...)
	}
	if profile.CFlags != nil {
		cfg.CFlags = append([]string(nil), (*profile.CFlags)...)
	}
	if profile.LinkFlags != nil {
		cfg.LinkFlags = append([]string(nil), (*profile.LinkFlags)...)
	}
	if profile.IncludeDirs != nil {
		cfg.IncludeDirs = append([]string(nil), (*profile.IncludeDirs)...)
	}
	if profile.LibraryDirs != nil {
		cfg.LibraryDirs = append([]string(nil), (*profile.LibraryDirs)...)
	}
	if profile.Libraries != nil {
		cfg.Libraries = append([]string(nil), (*profile.Libraries)...)
	}
	if profile.Defines != nil {
		cfg.Defines = append([]string(nil), (*profile.Defines)...)
	}
	if profile.Target != nil {
		cfg.Target = *profile.Target
	}
	if profile.Standard != nil {
		cfg.Standard = *profile.Standard
	}
	if profile.Linkage != nil {
		cfg.Linkage = *profile.Linkage
	}
}

func compilerCommand(cfg Config) (string, []string, error) {
	compiler := normalizeCompilerProfileName(cfg.Compiler)
	compilerPath := strings.TrimSpace(cfg.CompilerPath)
	args := append([]string(nil), cfg.CompilerArgs...)
	if compiler == "" && compilerPath == "" {
		compiler = "cc"
	}
	if compilerPath != "" {
		return compilerPath, args, nil
	}
	switch compiler {
	case "", "cc":
		return "cc", args, nil
	case "gcc":
		return "gcc", args, nil
	case "clang":
		return "clang", args, nil
	case "zigcc":
		if len(args) == 0 {
			args = append(args, "cc")
		}
		return "zig", args, nil
	case "msvc":
		return "cl", args, nil
	default:
		// Custom compiler executable:
		// compiler = "tcc"
		// compiler = "x86_64-w64-mingw32-gcc"
		return compiler, args, nil
	}
}

func normalizedCompilerName(
	cfg Config,
) string {
	if cfg.Compiler != "" {
		return normalizeCompilerProfileName(
			cfg.Compiler,
		)
	}

	base := strings.ToLower(
		filepath.Base(cfg.CompilerPath),
	)

	base = strings.TrimSuffix(
		base,
		".exe",
	)

	switch base {
	case "zig":
		return "zigcc"

	case "cl":
		return "msvc"

	default:
		return base
	}
}

func withDiagnostics(err error, reporter *diag.Reporter) error {
	if reporter != nil && reporter.HasErrors() {
		return fmt.Errorf("%w\n%s", err, reporter.String())
	}

	return err
}

func sanitizeFileName(name string) string {
	var b strings.Builder

	for _, ch := range name {
		if ch >= 'a' && ch <= 'z' ||
			ch >= 'A' && ch <= 'Z' ||
			ch >= '0' && ch <= '9' ||
			ch == '_' ||
			ch == '-' {
			b.WriteRune(ch)
			continue
		}

		b.WriteByte('_')
	}

	if b.Len() == 0 {
		return "package"
	}

	return b.String()
}
