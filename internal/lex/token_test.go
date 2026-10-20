package lex

import "testing"

func TestVocabulary(t *testing.T) {
	for k := ILLEGAL; k <= SELF; k++ {
		if k.String() == "" {
			t.Errorf("missing spelling for %d", k)
		}
	}
	for name, want := range map[string]Kind{"::": DCOLON, "=>": FATARROW, "?": QUESTION, "#": HASH, "$": DOLLAR, "~": TILDE, "!=": NEQ, "&^=": AND_NOT_ASSIGN} {
		if want.String() != name {
			t.Errorf("%d: got %q, want %q", want, want.String(), name)
		}
	}
	if Kind(65535).String() != "token(65535)" {
		t.Fatal("unknown token formatting")
	}
}

func TestReservedTiers(t *testing.T) {
	for _, name := range []string{"enum", "match", "trait", "impl", "use", "pub", "mod", "unsafe", "declare", "Self", "func", "range", "import"} {
		if !IsHardReserved(name) || Lookup(name) == IDENT {
			t.Errorf("hard reserved: %s", name)
		}
	}
	for _, name := range []string{"mut", "ref", "as", "where", "self", "super", "in", "macro_rules"} {
		if !IsContextual(name) || Lookup(name) != IDENT || IsHardReserved(name) {
			t.Errorf("contextual: %s", name)
		}
	}
	for _, name := range []string{"async", "await", "let", "fn", "loop", "while", "macro"} {
		if !IsFutureReserved(name) || Lookup(name) != IDENT {
			t.Errorf("future reserved: %s", name)
		}
	}
	for _, name := range []string{"yield", "derive", "cfg", "Option", "Result", "Some", "None", "Ok", "Err", "i32", "move", "dyn", "static", "crate"} {
		if IsContextual(name) || IsFutureReserved(name) || Lookup(name) != IDENT {
			t.Errorf("ordinary name: %s", name)
		}
	}
}

func TestPrecedence(t *testing.T) {
	for k, want := range map[Kind]int{LOR: 1, LAND: 2, EQL: 3, NEQ: 3, LSS: 3, LEQ: 3, GTR: 3, GEQ: 3, ADD: 4, SUB: 4, OR: 4, XOR: 4, MUL: 5, QUO: 5, REM: 5, SHL: 5, SHR: 5, AND: 5, AND_NOT: 5, NOT: 0, DCOLON: 0, QUESTION: 0} {
		if got := k.Precedence(); got != want {
			t.Errorf("%s: got %d, want %d", k, got, want)
		}
	}
}
