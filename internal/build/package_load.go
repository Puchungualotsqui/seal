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
	Package *Package
	File    *ast.File
	CCode   string
	CPath   string
}

func LoadAndCheckPackage(pkg *Package, reporter *diag.Reporter) (*ast.File, error) {
	files, err := SealFiles(pkg.Config.RootDir)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		if pkg.Config.Kind == KindLibrary {
			return &ast.File{}, nil
		}

		return nil, fmt.Errorf("executable package %q has no .seal files", pkg.Config.Name)
	}

	combined := &ast.File{}

	for _, path := range files {
		srcBytes, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		src := source.NewFile(path, string(srcBytes))

		lex := lexer.New(src, reporter)
		tokens := lex.LexAll()

		if reporter.HasErrors() {
			return nil, fmt.Errorf("lexing failed for package %q", pkg.Config.Name)
		}

		p := parser.New(tokens, reporter)
		parsed := p.ParseFile()

		if reporter.HasErrors() {
			return nil, fmt.Errorf("parsing failed for package %q", pkg.Config.Name)
		}

		combined.Decls = append(combined.Decls, parsed.Decls...)
	}

	r := resolver.New(reporter)
	r.ResolveFile(combined)

	if reporter.HasErrors() {
		return nil, fmt.Errorf("resolving failed for package %q", pkg.Config.Name)
	}

	c := checker.New(reporter)
	c.CheckFile(combined)

	if reporter.HasErrors() {
		return nil, fmt.Errorf("checking failed for package %q", pkg.Config.Name)
	}

	return combined, nil
}

func GeneratePackageC(pkg *Package, file *ast.File, reporter *diag.Reporter) (string, error) {
	if file == nil || len(file.Decls) == 0 {
		return fmt.Sprintf("/* empty package %s */\n", pkg.Config.Name), nil
	}

	g := cgen.New(reporter)
	out := g.Generate(file)

	if reporter.HasErrors() {
		return "", fmt.Errorf("C generation failed for package %q", pkg.Config.Name)
	}

	return out, nil
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
