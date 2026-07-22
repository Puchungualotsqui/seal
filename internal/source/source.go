package source

import (
	"fmt"
	"unicode/utf8"
)

type File struct {
	Path       string
	Text       string
	lineStarts []int
}

func NewFile(path string, text string) *File {
	f := &File{
		Path:       path,
		Text:       text,
		lineStarts: []int{0},
	}

	for index := 0; index < len(text); index++ {
		if text[index] == '\n' {
			f.lineStarts = append(
				f.lineStarts,
				index+1,
			)
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

/*
Position is the compiler-facing source position.

Line and Column are one-based.

Column is measured in UTF-8 bytes so existing command-line diagnostics retain
their current behavior.
*/
type Position struct {
	Line   int
	Column int
}

/*
LSPPosition is an editor-facing source position.

Line and Character are zero-based.

Character is measured in UTF-16 code units, as required by the default LSP
position encoding.
*/
type LSPPosition struct {
	Line      int
	Character int
}

type LSPRange struct {
	Start LSPPosition
	End   LSPPosition
}

/*
Position converts a UTF-8 byte offset into the compiler's existing one-based
byte position.
*/
func (f *File) Position(offset int) Position {
	if f == nil {
		return Position{}
	}

	offset = f.clampOffset(offset)

	line := f.lineIndexAtOffset(
		offset,
	)

	column :=
		offset -
			f.lineStarts[line]

	return Position{
		Line:   line + 1,
		Column: column + 1,
	}
}

/*
LSPPosition converts a UTF-8 byte offset into a zero-based UTF-16 position.

When an offset falls in the middle of a UTF-8 sequence, the position is mapped
to the beginning of that Unicode scalar. Compiler-generated spans should
normally already lie on valid boundaries.
*/
func (f *File) LSPPosition(
	offset int,
) LSPPosition {
	if f == nil {
		return LSPPosition{}
	}

	offset = f.clampOffset(
		offset,
	)

	line :=
		f.lineIndexAtOffset(
			offset,
		)

	lineStart :=
		f.lineStarts[line]

	character :=
		utf16LengthUntil(
			f.Text,
			lineStart,
			offset,
		)

	return LSPPosition{
		Line:      line,
		Character: character,
	}
}

/*
OffsetFromLSPPosition converts a zero-based UTF-16 LSP position into a UTF-8
byte offset.

Positions outside the document are clamped to the nearest valid document
position.

When Character points into the middle of a UTF-16 surrogate pair, the returned
offset is the beginning of that Unicode scalar.
*/
func (f *File) OffsetFromLSPPosition(
	position LSPPosition,
) int {
	if f == nil ||
		len(f.lineStarts) == 0 {
		return 0
	}

	line := position.Line

	if line < 0 {
		line = 0
	}

	if line >= len(f.lineStarts) {
		return len(f.Text)
	}

	character := position.Character

	if character < 0 {
		character = 0
	}

	lineStart :=
		f.lineStarts[line]

	lineEnd :=
		f.lineContentEnd(line)

	offset := lineStart
	currentCharacter := 0

	for offset < lineEnd {
		value, width :=
			utf8.DecodeRuneInString(
				f.Text[offset:lineEnd],
			)

		if width <= 0 {
			break
		}

		units :=
			utf16RuneLength(value)

		if character <
			currentCharacter+units {
			return offset
		}

		currentCharacter +=
			units

		offset += width

		if character ==
			currentCharacter {
			return offset
		}
	}

	return lineEnd
}

/*
LSPRange converts the span into an editor-facing range.

Malformed spans whose end precedes their start are normalized to an empty
range at the start position.
*/
func (s Span) LSPRange() LSPRange {
	if s.File == nil {
		return LSPRange{}
	}

	start :=
		s.File.clampOffset(
			s.Start,
		)

	end :=
		s.File.clampOffset(
			s.End,
		)

	if end < start {
		end = start
	}

	return LSPRange{
		Start: s.File.LSPPosition(
			start,
		),
		End: s.File.LSPPosition(
			end,
		),
	}
}

func (s Span) String() string {
	if s.File == nil {
		return "<unknown>"
	}

	pos :=
		s.File.Position(
			s.Start,
		)

	return fmt.Sprintf(
		"%s:%d:%d",
		s.File.Path,
		pos.Line,
		pos.Column,
	)
}

func (s Span) Text() string {
	if s.File == nil {
		return ""
	}

	start :=
		s.File.clampOffset(
			s.Start,
		)

	end :=
		s.File.clampOffset(
			s.End,
		)

	if start > end {
		return ""
	}

	return s.File.Text[start:end]
}

func (f *File) clampOffset(
	offset int,
) int {
	if offset < 0 {
		return 0
	}

	if offset > len(f.Text) {
		return len(f.Text)
	}

	return offset
}

/*
lineIndexAtOffset returns the zero-based line containing offset.

An offset immediately after a newline belongs to the following line.
*/
func (f *File) lineIndexAtOffset(
	offset int,
) int {
	if f == nil ||
		len(f.lineStarts) == 0 {
		return 0
	}

	offset =
		f.clampOffset(
			offset,
		)

	line := 0
	low := 0
	high := len(f.lineStarts)

	for low < high {
		middle :=
			(low + high) / 2

		if f.lineStarts[middle] <=
			offset {
			line = middle
			low = middle + 1
		} else {
			high = middle
		}
	}

	return line
}

/*
lineContentEnd returns the byte offset immediately after the visible content of
a line. The line-ending bytes are excluded.

Both LF and CRLF line endings are handled.
*/
func (f *File) lineContentEnd(
	line int,
) int {
	if f == nil ||
		len(f.lineStarts) == 0 {
		return 0
	}

	if line < 0 {
		line = 0
	}

	if line >= len(f.lineStarts) {
		return len(f.Text)
	}

	if line+1 >=
		len(f.lineStarts) {
		return len(f.Text)
	}

	end :=
		f.lineStarts[line+1] -
			1

	if end > f.lineStarts[line] &&
		f.Text[end-1] == '\r' {
		end--
	}

	return end
}

/*
utf16LengthUntil counts UTF-16 code units between start and end.

If end lies in the middle of a UTF-8 sequence, the incomplete scalar is not
counted.
*/
func utf16LengthUntil(
	text string,
	start int,
	end int,
) int {
	if start < 0 {
		start = 0
	}

	if end < start {
		return 0
	}

	if end > len(text) {
		end = len(text)
	}

	offset := start
	length := 0

	for offset < end {
		value, width :=
			utf8.DecodeRuneInString(
				text[offset:],
			)

		if width <= 0 ||
			offset+width > end {
			break
		}

		length +=
			utf16RuneLength(
				value,
			)

		offset += width
	}

	return length
}

func utf16RuneLength(
	value rune,
) int {
	if value > 0xFFFF {
		return 2
	}

	return 1
}
