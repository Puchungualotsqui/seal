package source

import "testing"

func TestFilePositionSingleLine(t *testing.T) {
	file := NewFile("main.seal", "abc def")

	pos := file.Position(4)

	if pos.Line != 1 || pos.Column != 5 {
		t.Fatalf("expected 1:5, got %d:%d", pos.Line, pos.Column)
	}
}

func TestFilePositionMultipleLines(t *testing.T) {
	file := NewFile("main.seal", "abc\nhello\nx")

	tests := []struct {
		offset int
		line   int
		column int
	}{
		{0, 1, 1},
		{3, 1, 4},
		{4, 2, 1},
		{9, 2, 6},
		{10, 3, 1},
	}

	for _, tt := range tests {
		pos := file.Position(tt.offset)

		if pos.Line != tt.line || pos.Column != tt.column {
			t.Fatalf("offset %d: expected %d:%d, got %d:%d",
				tt.offset,
				tt.line,
				tt.column,
				pos.Line,
				pos.Column,
			)
		}
	}
}

func TestSpanText(t *testing.T) {
	file := NewFile("main.seal", "hello world")
	span := NewSpan(file, 0, 5)

	if span.Text() != "hello" {
		t.Fatalf("expected hello, got %q", span.Text())
	}
}
