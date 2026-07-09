package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"seal/internal/ast"
	"seal/internal/checker"
	cgen "seal/internal/codegen"
	"seal/internal/diag"
	"seal/internal/lexer"
	"seal/internal/parser"
	"seal/internal/resolver"
	"seal/internal/source"
)

type LoadedPackage struct {
	Package      *Package
	File         *ast.File
	CCode        string
	CPath        string
	NativeCFiles []string

	CodegenInfo *cgen.PackageInfo
}

func checkerOptionsFromConfig(cfg Config) checker.Options {
	return checker.Options{
		GenericConstraintMaxDepth: cfg.GenericConstraintMaxDepth,
	}
}

func LoadAndCheckPackage(
	pkg *Package,
	reporter *diag.Reporter,
	resolverPackages map[string]*resolver.PackageInfo,
	checkerPackages map[string]*checker.PackageInfo,
) (*ast.File, *resolver.Scope, *checker.Scope, error) {
	files, err := SealFiles(pkg.Config.RootDir)
	if err != nil {
		return nil, nil, nil, err
	}

	checkerOptions := checkerOptionsFromConfig(pkg.Config)

	if len(files) == 0 {
		if pkg.Config.Kind == KindLibrary {
			empty := &ast.File{}
			r := resolver.NewWithPackages(reporter, resolverPackages)
			resolverScope := r.ResolveFile(empty)

			c := checker.NewWithPackagesAndOptions(reporter, checkerPackages, checkerOptions)
			checkerScope := c.CheckFile(empty)

			return empty, resolverScope, checkerScope, nil
		}

		return nil, nil, nil, fmt.Errorf("executable package %q has no .seal files", pkg.Config.Name)
	}

	combined := &ast.File{}

	for _, path := range files {
		srcBytes, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, nil, err
		}

		src := source.NewFile(path, string(srcBytes))

		lex := lexer.New(src, reporter)
		tokens := lex.LexAll()

		if reporter.HasErrors() {
			return nil, nil, nil, fmt.Errorf("lexing failed for package %q", pkg.Config.Name)
		}

		p := parser.New(tokens, reporter)
		parsed := p.ParseFile()

		if reporter.HasErrors() {
			return nil, nil, nil, fmt.Errorf("parsing failed for package %q", pkg.Config.Name)
		}

		combined.Decls = append(combined.Decls, parsed.Decls...)
	}

	r := resolver.NewWithPackages(reporter, resolverPackages)
	resolverScope := r.ResolveFile(combined)

	if reporter.HasErrors() {
		return nil, nil, nil, fmt.Errorf("resolving failed for package %q", pkg.Config.Name)
	}

	c := checker.NewWithPackagesAndOptions(reporter, checkerPackages, checkerOptions)
	checkerScope := c.CheckFile(combined)

	if reporter.HasErrors() {
		return nil, nil, nil, fmt.Errorf("checking failed for package %q", pkg.Config.Name)
	}

	return combined, resolverScope, checkerScope, nil
}

func CFiles(root string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			switch filepath.Base(path) {
			case ".git", ".seal", "build", "vendor":
				return filepath.SkipDir
			}
			return nil
		}

		if strings.HasSuffix(entry.Name(), ".c") {
			files = append(files, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func GeneratePackageC(
	pkg *Package,
	file *ast.File,
	reporter *diag.Reporter,
	codegenPackages map[string]*cgen.PackageInfo,
) (string, *cgen.PackageInfo, error) {
	if file == nil || len(file.Decls) == 0 {
		info := &cgen.PackageInfo{
			Name:      pkg.Config.Name,
			Tasks:     map[string]cgen.TaskInfo{},
			Overloads: map[string][]string{},
		}

		return fmt.Sprintf("/* empty package %s */\n", pkg.Config.Name), info, nil
	}

	info := cgen.ExportPackageInfo(pkg.Config.Name, file, reporter)

	g := cgen.NewWithPackages(reporter, pkg.Config.Name, codegenPackages)
	out := g.Generate(file)

	if reporter.HasErrors() {
		return "", nil, fmt.Errorf("C generation failed for package %q", pkg.Config.Name)
	}

	return out, info, nil
}

func SealFiles(root string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			base := filepath.Base(path)

			switch base {
			case ".git", ".seal", "build", "vendor":
				return filepath.SkipDir
			}

			return nil
		}

		if strings.HasSuffix(entry.Name(), ".seal") {
			files = append(files, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}
