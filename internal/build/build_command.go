package build

import (
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

type BuildOptions struct {
	EmitOnly bool
	OutDir   string
	Output   string
}

type BuildResult struct {
	Graph    *Graph
	Packages []*LoadedPackage
	OutDir   string
	Output   string
}

func BuildWorkspace(startPath string, options BuildOptions) (*BuildResult, error) {
	graph, err := DiscoverAndBuildGraph(startPath)
	if err != nil {
		return nil, err
	}

	outDir := options.OutDir
	if outDir == "" {
		outDir = filepath.Join(graph.Root.Config.RootDir, ".seal", "build")
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, err
	}

	var loaded []*LoadedPackage

	resolverPackages := map[string]*resolver.PackageInfo{}
	checkerPackages := map[string]*checker.PackageInfo{}
	codegenPackages := map[string]*cgen.PackageInfo{}

	for _, pkg := range graph.Order {
		reporter := diag.NewReporter()

		file, resolverScope, checkerScope, err := LoadAndCheckPackage(
			pkg,
			reporter,
			resolverPackages,
			checkerPackages,
		)
		if err != nil {
			return nil, withDiagnostics(err, reporter)
		}

		cCode, codegenInfo, err := GeneratePackageC(
			pkg,
			file,
			reporter,
			codegenPackages,
		)
		if err != nil {
			return nil, withDiagnostics(err, reporter)
		}

		cPath := filepath.Join(outDir, sanitizeFileName(pkg.Config.Name)+".c")

		nativeCFiles, err := CFiles(pkg.Config.RootDir)
		if err != nil {
			return nil, err
		}

		if err := os.WriteFile(cPath, []byte(cCode), 0644); err != nil {
			return nil, err
		}

		resolverPackages[pkg.Config.Name] = resolver.ExportPackage(pkg.Config.Name, resolverScope)
		checkerPackages[pkg.Config.Name] = checker.ExportPackage(pkg.Config.Name, checkerScope)
		codegenPackages[pkg.Config.Name] = codegenInfo

		loaded = append(loaded, &LoadedPackage{
			Package:      pkg,
			File:         file,
			CCode:        cCode,
			CPath:        cPath,
			NativeCFiles: nativeCFiles,
		})
	}

	output := options.Output
	if output == "" {
		output = filepath.Join(outDir, graph.Root.Config.Name)
	}

	if runtime.GOOS == "windows" && filepath.Ext(output) == "" {
		output += ".exe"
	}

	if !options.EmitOnly && graph.Root.Config.Kind == KindExecutable {
		if err := compileExecutable(graph, loaded, output); err != nil {
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

func compileExecutable(graph *Graph, loaded []*LoadedPackage, output string) error {
	root := graph.Root
	if root == nil {
		return fmt.Errorf("missing root package")
	}

	compilerPath, compilerArgs, err := compilerCommand(root.Config)
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

	args = append(args, compilerConfigArgs(root.Config)...)
	args = append(args, "-o", output)

	cmd := exec.Command(compilerPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("C compiler failed: %w\ncommand: %s %s", err, compilerPath, strings.Join(args, " "))
	}

	return nil
}

func compilerCommand(cfg Config) (string, []string, error) {
	compiler := strings.TrimSpace(cfg.Compiler)
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

	case "zigcc", "zig-cc", "zig cc":
		if len(args) == 0 {
			args = append(args, "cc")
		}
		return "zig", args, nil

	case "msvc", "cl":
		return "cl", args, nil

	default:
		// Allow custom compiler names directly:
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
		case "msvc", "cl":
			// MSVC does not use -std=c11.
			// Add MSVC-specific flags later if needed.
		default:
			args = append(args, "-std="+cfg.Standard)
		}
	}

	if cfg.Target != "" {
		switch normalizedCompilerName(cfg) {
		case "zigcc", "zig-cc", "zig cc":
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

func normalizedCompilerName(cfg Config) string {
	if cfg.Compiler != "" {
		return strings.ToLower(strings.TrimSpace(cfg.Compiler))
	}

	base := strings.ToLower(filepath.Base(cfg.CompilerPath))
	base = strings.TrimSuffix(base, ".exe")

	if base == "zig" {
		return "zigcc"
	}

	return base
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
