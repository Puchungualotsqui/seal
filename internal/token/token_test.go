package token

import "testing"

func TestLookupIdentKeyword(t *testing.T) {
	tests := map[string]Kind{
		"task":      KeywordTask,
		"struct":    KeywordStruct,
		"distinct":  KeywordDistinct,
		"dyn":       KeywordDyn,
		"interface": KeywordInterface,
		"type":      KeywordType,
	}

	for text, want := range tests {
		if got := LookupIdent(text); got != want {
			t.Fatalf("expected %q to be %s, got %s", text, want.String(), got.String())
		}
	}
}

func TestLookupIdentNormalIdentifier(t *testing.T) {
	if LookupIdent("Damage") != Ident {
		t.Fatalf("expected identifier")
	}

	if LookupIdent("enemyCount") != Ident {
		t.Fatalf("expected identifier")
	}
}

func TestKindString(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{KeywordTask, "task"},
		{KeywordDistinct, "distinct"},
		{KeywordDyn, "dyn"},
		{KeywordType, "type"},
		{ColonColon, "::"},
		{ColonEq, ":="},
		{Ellipsis, "..."},
		{Ident, "Ident"},
	}

	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Fatalf("expected %q, got %q", tt.want, got)
		}
	}
}

func TestLookupInterfaceKeywords(t *testing.T) {
	tests := []struct {
		text string
		want Kind
	}{
		{"interface", KeywordInterface},
		{"impl", KeywordImpl},
		{"using", KeywordUsing},
		{"self", KeywordSelf},
		{"dyn", KeywordDyn},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := LookupIdent(tt.text); got != tt.want {
				t.Fatalf(
					"LookupIdent(%q) = %v; want %v",
					tt.text,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestInterfaceKeywordStrings(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{KeywordInterface, "interface"},
		{KeywordImpl, "impl"},
		{KeywordUsing, "using"},
		{KeywordSelf, "self"},
		{KeywordDyn, "dyn"},
	}

	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf(
				"%v.String() = %q; want %q",
				tt.kind,
				got,
				tt.want,
			)
		}
	}
}
