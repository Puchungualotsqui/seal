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
