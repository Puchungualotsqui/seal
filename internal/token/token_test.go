package token

import "testing"

func TestLookupIdentKeyword(t *testing.T) {
	if LookupIdent("task") != KeywordTask {
		t.Fatalf("expected task keyword")
	}

	if LookupIdent("struct") != KeywordStruct {
		t.Fatalf("expected struct keyword")
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
