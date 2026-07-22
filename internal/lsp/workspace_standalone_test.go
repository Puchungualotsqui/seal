package lsp

import (
	"context"
	"path/filepath"
	"testing"
)

func TestWorkspaceAnalyzesStandaloneFilesIndependently(
	t *testing.T,
) {
	t.Helper()

	root :=
		t.TempDir()

	workspace, err :=
		NewWorkspace(
			root,
		)

	if err != nil {
		t.Fatalf(
			"creating standalone workspace: %v",
			err,
		)
	}

	firstPath :=
		filepath.Join(
			root,
			"first.seal",
		)

	secondPath :=
		filepath.Join(
			root,
			"second.seal",
		)

	openStandaloneTestDocument(
		t,
		workspace,
		firstPath,
		`Main :: task() {
}
`,
	)

	/*
		The same declaration is legal because the second standalone file is
		analyzed as a different synthetic package.
	*/
	openStandaloneTestDocument(
		t,
		workspace,
		secondPath,
		`Main :: task() {
}
`,
	)

	snapshot, err :=
		workspace.Analyze(
			context.Background(),
		)

	if err != nil {
		t.Fatalf(
			"analyzing standalone documents: %v",
			err,
		)
	}

	firstPackage :=
		snapshot.PackageForPath(
			firstPath,
		)

	if firstPackage == nil {
		t.Fatalf(
			"no standalone package for %q",
			firstPath,
		)
	}

	secondPackage :=
		snapshot.PackageForPath(
			secondPath,
		)

	if secondPackage == nil {
		t.Fatalf(
			"no standalone package for %q",
			secondPath,
		)
	}

	if firstPackage ==
		secondPackage {
		t.Fatal(
			"standalone files unexpectedly share one package snapshot",
		)
	}

	if firstPackage.Package != nil {
		t.Fatal(
			"first standalone package unexpectedly has manifest metadata",
		)
	}

	if secondPackage.Package != nil {
		t.Fatal(
			"second standalone package unexpectedly has manifest metadata",
		)
	}

	if firstPackage.StandalonePath == "" {
		t.Fatal(
			"first standalone package has no standalone path",
		)
	}

	if secondPackage.StandalonePath == "" {
		t.Fatal(
			"second standalone package has no standalone path",
		)
	}

	if len(firstPackage.Result.Files) != 1 {
		t.Fatalf(
			"first standalone package contains %d files; expected 1",
			len(firstPackage.Result.Files),
		)
	}

	if len(secondPackage.Result.Files) != 1 {
		t.Fatalf(
			"second standalone package contains %d files; expected 1",
			len(secondPackage.Result.Files),
		)
	}
}

func openStandaloneTestDocument(
	t *testing.T,
	workspace *Workspace,
	path string,
	text string,
) {
	t.Helper()

	uri, err :=
		PathToFileURI(
			path,
		)

	if err != nil {
		t.Fatalf(
			"converting %q to URI: %v",
			path,
			err,
		)
	}

	err =
		workspace.OpenDocument(
			Document{
				URI: uri,

				Path: path,

				Version: 1,

				Text: text,
			},
		)

	if err != nil {
		t.Fatalf(
			"opening %q: %v",
			path,
			err,
		)
	}
}
