package formatter

import "testing"

func TestFormatBasic(t *testing.T) {
	input := "Main::task(){\nvalue:=Foo( 1,2 )\nif value==3{\nreturn\n}\n}\n"
	want := "Main :: task() {\n    value := Foo(1, 2)\n    if value == 3 {\n        return\n    }\n}\n"
	got := Format(input, Options{TabSize: 4, InsertSpaces: true})
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFormatSwitchAndContinuation(t *testing.T) {
	input := "Main :: task() {\nswitch value type {\ncase int:\nx:=Call(\na,\nb,\n)\ndefault:\nx=0\n}\n}\n"
	want := "Main :: task() {\n    switch value type {\n        case int:\n            x := Call(\n                a,\n                b,\n            )\n        default:\n            x = 0\n    }\n}\n"
	got := Format(input, Options{TabSize: 4, InsertSpaces: true})
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatIgnoresLiteralAndCommentDelimiters(t *testing.T) {
	input := "Main :: task() {\n// { comment\ntext:=\"// }\"\n/* {\n        preserved comment indent\n} */\n}\n"
	want := "Main :: task() {\n    // { comment\n    text := \"// }\"\n    /* {\n            preserved comment indent\n    } */\n}\n"
	got := Format(input, Options{TabSize: 4, InsertSpaces: true})
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatExpressionContinuation(t *testing.T) {
	input := "Main :: task() {\nremaining,valid:=\nByteSpanFrom(\ndestination,\noffset,\n)\noffset=\noffset+\ncount\n}\n"
	want := "Main :: task() {\n    remaining, valid :=\n        ByteSpanFrom(\n            destination,\n            offset,\n        )\n    offset =\n        offset+\n        count\n}\n"
	got := Format(input, Options{TabSize: 4, InsertSpaces: true})
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
