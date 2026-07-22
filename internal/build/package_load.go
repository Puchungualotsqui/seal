package build

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"seal/internal/ast"
	"seal/internal/checker"
	cgen "seal/internal/codegen"
	"seal/internal/diag"
	"seal/internal/frontend"
	"seal/internal/resolver"
)

type LoadedPackage struct {
	Package      *Package
	File         *ast.File
	CCode        string
	CPath        string
	NativeCFiles []string

	CodegenInfo  *cgen.PackageInfo
	SemanticInfo checker.SemanticInfo
}

func checkerOptionsFromConfig(
	cfg Config,
) checker.Options {
	return checker.Options{
		GenericConstraintMaxDepth: cfg.GenericConstraintMaxDepth,
	}
}

func LoadAndCheckPackage(
	pkg *Package,
	reporter *diag.Reporter,
	resolverPackages map[string]*resolver.PackageInfo,
	checkerPackages map[string]*checker.PackageInfo,
) (
	*ast.File,
	*resolver.Scope,
	*checker.Scope,
	error,
) {
	file,
		resolverScope,
		checkerScope,
		_,
		err :=
		LoadAndCheckPackageWithSemanticInfo(
			pkg,
			reporter,
			resolverPackages,
			checkerPackages,
		)

	return file,
		resolverScope,
		checkerScope,
		err
}

func LoadAndCheckPackageWithSemanticInfo(
	pkg *Package,
	reporter *diag.Reporter,
	resolverPackages map[string]*resolver.PackageInfo,
	checkerPackages map[string]*checker.PackageInfo,
) (
	*ast.File,
	*resolver.Scope,
	*checker.Scope,
	checker.SemanticInfo,
	error,
) {
	if pkg == nil {
		return nil,
			nil,
			nil,
			checker.SemanticInfo{},
			fmt.Errorf(
				"missing package",
			)
	}

	files, err :=
		SealFiles(
			pkg.Config.RootDir,
		)

	if err != nil {
		return nil,
			nil,
			nil,
			checker.SemanticInfo{},
			err
	}

	if len(files) == 0 &&
		pkg.Config.Kind != KindLibrary {
		return nil,
			nil,
			nil,
			checker.SemanticInfo{},
			fmt.Errorf(
				"executable package %q has no .seal files",
				pkg.Config.Name,
			)
	}

	sources := make(
		[]frontend.SourceInput,
		0,
		len(files),
	)

	for _, path := range files {
		sourceBytes, err :=
			os.ReadFile(
				path,
			)

		if err != nil {
			return nil,
				nil,
				nil,
				checker.SemanticInfo{},
				fmt.Errorf(
					"reading Seal source %q: %w",
					path,
					err,
				)
		}

		sources = append(
			sources,
			frontend.SourceInput{
				Path: path,
				Text: string(
					sourceBytes,
				),
			},
		)
	}

	result :=
		frontend.AnalyzePackage(
			frontend.PackageInput{
				Name: pkg.Config.Name,

				Files: sources,

				CheckerOptions: checkerOptionsFromConfig(
					pkg.Config,
				),
			},
			resolverPackages,
			checkerPackages,
		)

	if reporter != nil {
		reporter.AddDiagnostics(
			result.Diagnostics,
		)
	}

	if !result.Parsed {
		phase :=
			frontendSyntaxFailurePhase(
				&result,
			)

		return nil,
			nil,
			nil,
			checker.SemanticInfo{},
			fmt.Errorf(
				"%s failed for package %q",
				phase,
				pkg.Config.Name,
			)
	}

	if !result.Resolved {
		return nil,
			nil,
			nil,
			checker.SemanticInfo{},
			fmt.Errorf(
				"resolving failed for package %q",
				pkg.Config.Name,
			)
	}

	if !result.Checked {
		return nil,
			nil,
			nil,
			checker.SemanticInfo{},
			fmt.Errorf(
				"checking failed for package %q",
				pkg.Config.Name,
			)
	}

	return result.Combined,
		result.ResolverScope,
		result.CheckerScope,
		result.SemanticInfo,
		nil
}

/*
frontendSyntaxFailurePhase distinguishes lexical errors from parser errors.

The frontend does not run the parser for a file that contains lexical errors,
so a nil per-file AST means lexing failed. A non-nil partial AST with Parsed
set to false means parsing failed.
*/
func frontendSyntaxFailurePhase(
	result *frontend.Result,
) string {
	if result == nil {
		return "parsing"
	}

	for _, file := range result.Files {
		if file == nil ||
			file.AST == nil {
			return "lexing"
		}
	}

	return "parsing"
}

func shouldSkipPackageDirectory(
	root string,
	path string,
	entry os.DirEntry,
) (bool, error) {
	if entry == nil ||
		!entry.IsDir() ||
		path == root {
		return false, nil
	}

	switch entry.Name() {
	case ".git",
		".seal",
		"build",
		"vendor":
		return true, nil
	}

	manifestPath :=
		filepath.Join(
			path,
			"seal.toml",
		)

	_, err :=
		os.Stat(
			manifestPath,
		)

	switch {
	case err == nil:
		/*
			This directory is the root of another Seal package.
		*/
		return true, nil

	case os.IsNotExist(
		err,
	):
		return false, nil

	default:
		return false, err
	}
}

func CFiles(
	root string,
) ([]string, error) {
	root =
		filepath.Clean(
			root,
		)

	var files []string

	err :=
		filepath.WalkDir(
			root,
			func(
				path string,
				entry os.DirEntry,
				walkErr error,
			) error {
				if walkErr != nil {
					return walkErr
				}

				if entry.IsDir() {
					skip, err :=
						shouldSkipPackageDirectory(
							root,
							path,
							entry,
						)

					if err != nil {
						return err
					}

					if skip {
						return fs.SkipDir
					}

					return nil
				}

				if strings.HasSuffix(
					entry.Name(),
					".c",
				) {
					files = append(
						files,
						path,
					)
				}

				return nil
			},
		)

	if err != nil {
		return nil, err
	}

	sort.Strings(
		files,
	)

	return files, nil
}

func GeneratePackageC(
	pkg *Package,
	file *ast.File,
	reporter *diag.Reporter,
	codegenPackages map[string]*cgen.PackageInfo,
) (
	string,
	*cgen.PackageInfo,
	error,
) {
	return GeneratePackageCWithSemanticInfo(
		pkg,
		file,
		reporter,
		codegenPackages,
		checker.SemanticInfo{},
	)
}

func GeneratePackageCWithSemanticInfo(
	pkg *Package,
	file *ast.File,
	reporter *diag.Reporter,
	codegenPackages map[string]*cgen.PackageInfo,
	semantic checker.SemanticInfo,
) (
	string,
	*cgen.PackageInfo,
	error,
) {
	if file == nil ||
		len(file.Decls) == 0 {
		info :=
			&cgen.PackageInfo{
				Name: pkg.Config.Name,

				Tasks: map[string]cgen.TaskInfo{},

				Overloads: map[string][]string{},
			}

		return fmt.Sprintf(
				"/* empty package %s */\n",
				pkg.Config.Name,
			),
			info,
			nil
	}

	info :=
		cgen.ExportPackageInfoWithSemanticInfo(
			pkg.Config.Name,
			file,
			reporter,
			codegenPackages,
			semantic,
		)

	if reporter.HasErrors() {
		return "",
			nil,
			fmt.Errorf(
				"C package export failed for package %q",
				pkg.Config.Name,
			)
	}

	g :=
		cgen.NewWithPackagesAndSemanticInfo(
			reporter,
			pkg.Config.Name,
			codegenPackages,
			semantic,
		)

	g.SetWorkspacePackages(
		codegenPackages,
	)

	out :=
		g.Generate(
			file,
		)

	if reporter.HasErrors() {
		return "",
			nil,
			fmt.Errorf(
				"C generation failed for package %q",
				pkg.Config.Name,
			)
	}

	return out,
		info,
		nil
}

func SealFiles(
	root string,
) ([]string, error) {
	root =
		filepath.Clean(
			root,
		)

	var files []string

	err :=
		filepath.WalkDir(
			root,
			func(
				path string,
				entry os.DirEntry,
				walkErr error,
			) error {
				if walkErr != nil {
					return walkErr
				}

				if entry.IsDir() {
					skip, err :=
						shouldSkipPackageDirectory(
							root,
							path,
							entry,
						)

					if err != nil {
						return err
					}

					if skip {
						return fs.SkipDir
					}

					return nil
				}

				if strings.HasSuffix(
					entry.Name(),
					".seal",
				) {
					files = append(
						files,
						path,
					)
				}

				return nil
			},
		)

	if err != nil {
		return nil, err
	}

	sort.Strings(
		files,
	)

	return files, nil
}
