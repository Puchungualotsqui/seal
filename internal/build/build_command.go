package build

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		DiscoverAndBuildGraph(startPath)
	if err != nil {
		return nil, err
	}

	outDir := options.OutDir

	if outDir == "" {
		outDir = filepath.Join(
			graph.Root.Config.RootDir,
			".seal",
			"build",
		)
	}

	if err := os.MkdirAll(
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
	//
	// Semantic side tables are retained with the exact checked AST. CGen
	// consumes those tables later and must not repeat index or len overload
	// resolution.
	for _, pkg := range graph.Order {
		reporter := diag.NewReporter()

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
			CFiles(pkg.Config.RootDir)

		if err != nil {
			return nil, err
		}

		cPath := filepath.Join(
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
		if err := os.WriteFile(
			pkg.CPath,
			[]byte(pkg.CCode),
			0644,
		); err != nil {
			return nil, err
		}
	}

	output := options.Output

	if output == "" {
		output = filepath.Join(
			outDir,
			graph.Root.Config.Name,
		)
	}

	if runtime.GOOS == "windows" &&
		filepath.Ext(output) == "" {
		output += ".exe"
	}

	if !options.EmitOnly &&
		graph.Root.Config.Kind ==
			KindExecutable {
		if err := compileExecutable(
			graph,
			loaded,
			output,
			options.Compiler,
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

func compileExecutable(
	graph *Graph,
	loaded []*LoadedPackage,
	output string,
	compilerOverride string,
) error {
	root := graph.Root
	if root == nil {
		return fmt.Errorf("missing root package")
	}

	compilerConfig := resolveCompilerConfig(
		root.Config,
		compilerOverride,
	)

	compilerPath, compilerArgs, err :=
		compilerCommand(compilerConfig)
	if err != nil {
		return err
	}

	args := make([]string, 0, len(compilerArgs)+len(loaded)*2+16)
	args = append(args, compilerArgs...)

	seen := map[string]bool{}

	addFile := func(path string) {
		if path == "" {
			return
		}

		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}

		if seen[abs] {
			return
		}

		seen[abs] = true
		args = append(args, path)
	}

	for _, pkg := range loaded {
		addFile(pkg.CPath)

		for _, native := range pkg.NativeCFiles {
			addFile(native)
		}
	}

	args = append(
		args,
		compilerConfigArgs(compilerConfig)...,
	)
	args = append(args, "-o", output)

	cmd := exec.Command(compilerPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("C compiler failed: %w\ncommand: %s %s", err, compilerPath, strings.Join(args, " "))
	}

	return nil
}

func resolveCompilerConfig(
	cfg Config,
	compilerOverride string,
) Config {
	out := cfg

	selected := strings.TrimSpace(
		compilerOverride,
	)

	if selected == "" {
		selected = strings.TrimSpace(
			cfg.Compiler,
		)
	}

	if selected == "" {
		selected = "cc"
	}

	selected =
		normalizeCompilerProfileName(
			selected,
		)

	out.Compiler = selected

	if profile, ok :=
		cfg.CompilerProfiles["default"]; ok {
		applyCompilerProfile(
			&out,
			profile,
		)
	}

	if profile, ok :=
		cfg.CompilerProfiles[selected]; ok {
		applyCompilerProfile(
			&out,
			profile,
		)
	}

	return out
}

func applyCompilerProfile(
	cfg *Config,
	profile CompilerProfile,
) {
	if cfg == nil {
		return
	}

	if profile.CompilerPath != nil {
		cfg.CompilerPath =
			*profile.CompilerPath
	}

	if profile.CompilerArgs != nil {
		cfg.CompilerArgs = append(
			[]string(nil),
			(*profile.CompilerArgs)...,
		)
	}

	if profile.CFlags != nil {
		cfg.CFlags = append(
			[]string(nil),
			(*profile.CFlags)...,
		)
	}

	if profile.LinkFlags != nil {
		cfg.LinkFlags = append(
			[]string(nil),
			(*profile.LinkFlags)...,
		)
	}

	if profile.IncludeDirs != nil {
		cfg.IncludeDirs = append(
			[]string(nil),
			(*profile.IncludeDirs)...,
		)
	}

	if profile.LibraryDirs != nil {
		cfg.LibraryDirs = append(
			[]string(nil),
			(*profile.LibraryDirs)...,
		)
	}

	if profile.Libraries != nil {
		cfg.Libraries = append(
			[]string(nil),
			(*profile.Libraries)...,
		)
	}

	if profile.Defines != nil {
		cfg.Defines = append(
			[]string(nil),
			(*profile.Defines)...,
		)
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

func compilerCommand(
	cfg Config,
) (string, []string, error) {
	compiler :=
		normalizeCompilerProfileName(
			cfg.Compiler,
		)

	compilerPath :=
		strings.TrimSpace(
			cfg.CompilerPath,
		)

	args := append(
		[]string(nil),
		cfg.CompilerArgs...,
	)

	if compiler == "" &&
		compilerPath == "" {
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
		//
		//     compiler = "tcc"
		//     compiler = "x86_64-w64-mingw32-gcc"
		return compiler, args, nil
	}
}

func compilerConfigArgs(cfg Config) []string {
	var args []string

	if cfg.Standard != "" {
		switch normalizedCompilerName(cfg) {
		case "msvc":
			// MSVC does not use -std=c11.
			// Add MSVC-specific flags later if needed.
		default:
			args = append(args, "-std="+cfg.Standard)
		}
	}

	if cfg.Target != "" {
		switch normalizedCompilerName(cfg) {
		case "zigcc":
			args = append(args, "-target", cfg.Target)
		default:
			// GCC/Clang target handling is platform-specific.
			// Let users pass it explicitly through c_flags if needed.
		}
	}

	for _, dir := range cfg.IncludeDirs {
		args = append(args, "-I"+dir)
	}

	for _, define := range cfg.Defines {
		args = append(args, "-D"+define)
	}

	args = append(args, cfg.CFlags...)

	for _, dir := range cfg.LibraryDirs {
		args = append(args, "-L"+dir)
	}

	for _, lib := range cfg.Libraries {
		args = append(args, "-l"+lib)
	}

	args = append(args, cfg.LinkFlags...)

	return args
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
