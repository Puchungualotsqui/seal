package source

import "fmt"

type File struct {
	Path       string
	Text       string
	lineStarts []int
}

func NewFile(path string, text string) *File {
	f := &File{
		Path: path,
		Text: text,
	}

	f.lineStarts = []int{0}

	for i, ch := range text {
		if ch == '\n' {
			f.lineStarts = append(f.lineStarts, i+1)
		}
	}

	return f
}

type Span struct {
	File  *File
	Start int
	End   int
}

func NewSpan(file *File, start int, end int) Span {
	return Span{
		File:  file,
		Start: start,
		End:   end,
	}
}

type Position struct {
	Line   int
	Column int
}

func (f *File) Position(offset int) Position {
	if offset < 0 {
		offset = 0
	}

	if offset > len(f.Text) {
		offset = len(f.Text)
	}

	line := 0

	low := 0
	high := len(f.lineStarts)

	for low < high {
		mid := (low + high) / 2

		if f.lineStarts[mid] <= offset {
			line = mid
			low = mid + 1
		} else {
			high = mid
		}
	}

	column := offset - f.lineStarts[line]

	return Position{
		Line:   line + 1,
		Column: column + 1,
	}
}

func (s Span) String() string {
	if s.File == nil {
		return "<unknown>"
	}

	pos := s.File.Position(s.Start)
	return fmt.Sprintf("%s:%d:%d", s.File.Path, pos.Line, pos.Column)
}

func (s Span) Text() string {
	if s.File == nil {
		return ""
	}

	start := s.Start
	end := s.End

	if start < 0 {
		start = 0
	}

	if end > len(s.File.Text) {
		end = len(s.File.Text)
	}

	if start > end {
		return ""
	}

	return s.File.Text[start:end]
}
