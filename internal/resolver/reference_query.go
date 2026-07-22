package resolver

import (
	"path/filepath"
	"runtime"
	"strings"

	"seal/internal/source"
)

func sameReferenceSpan(left, right source.Span) bool {
	return sameReferenceFile(left.File, right.File) &&
		left.Start == right.Start &&
		left.End == right.End
}

func sameReferenceFile(left, right *source.File) bool {
	if left == right {
		return left != nil
	}
	if left == nil || right == nil || left.Path == "" || right.Path == "" {
		return false
	}

	leftPath := filepath.Clean(left.Path)
	rightPath := filepath.Clean(right.Path)

	if runtime.GOOS == "windows" {
		return strings.EqualFold(leftPath, rightPath)
	}
	return leftPath == rightPath
}

func normalizedReferencePath(file *source.File) string {
	if file == nil {
		return ""
	}
	path := filepath.Clean(file.Path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
