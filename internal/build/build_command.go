package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

		if err := os.WriteFile(cPath, []byte(cCode), 0644); err != nil {
			return nil, err
		}

		resolverPackages[pkg.Config.Name] = resolver.ExportPackage(pkg.Config.Name, resolverScope)
		checkerPackages[pkg.Config.Name] = checker.ExportPackage(pkg.Config.Name, checkerScope)
		codegenPackages[pkg.Config.Name] = codegenInfo

		loaded = append(loaded, &LoadedPackage{
			Package: pkg,
			File:    file,
			CCode:   cCode,
			CPath:   cPath,
		})
	}

	output := options.Output
	if output == "" {
		output = filepath.Join(outDir, graph.Root.Config.Name)
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
	compiler := graph.Root.Config.Compiler
	if compiler == "" {
		compiler = "cc"
	}

	args := make([]string, 0, len(loaded)+2)

	for _, pkg := range loaded {
		args = append(args, pkg.CPath)
	}

	args = append(args, "-o", output)

	cmd := exec.Command(compiler, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("C compiler failed: %w", err)
	}

	return nil
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
