package frontend

import "seal/internal/checker"

/*
SourceInput is one immutable Seal source file supplied to an analysis.

Text may come from disk or from an unsaved editor buffer. The frontend does
not read the filesystem.
*/
type SourceInput struct {
	Path string
	Text string
}

/*
PackageInput contains all source files that belong to one Seal package.

Files should normally be supplied in deterministic path order. Declaration
order between files follows the order of this slice.
*/
type PackageInput struct {
	Name string

	Files []SourceInput

	CheckerOptions checker.Options
}
