package lex

import (
	"reflect"
	"strings"
	"testing"
)

func kinds(ts []Token) []Kind {
	ks := make([]Kind, len(ts))
	for i, t := range ts {
		ks[i] = t.Kind
	}
	return ks
}

func TestNewTokensAndLongestMatch(t *testing.T) {
	src := ":: => ? # $ ~ != := : = > ! &^= >>= ..."
	got, ds := Lex([]byte(src))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	want := []Kind{DCOLON, FATARROW, QUESTION, HASH, DOLLAR, TILDE, NEQ, DEFINE, COLON, ASSIGN, GTR, NOT, AND_NOT_ASSIGN, SHR_ASSIGN, ELLIPSIS, EOF}
	if !reflect.DeepEqual(kinds(got), want) {
		t.Fatalf("got %v, want %v", kinds(got), want)
	}
	for _, tok := range got[:len(got)-1] {
		if tok.Text != src[tok.Start:tok.End] {
			t.Errorf("incorrect span: %+v", tok)
		}
	}
}

func TestRawIdentifiers(t *testing.T) {
	for _, name := range []string{"match", "Self", "func", "async", "where", "你好", "_"} {
		src := "r#" + name
		ts, ds := Lex([]byte(src))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		if ts[0].Kind != IDENT || ts[0].Text != name || !ts[0].Raw || ts[0].Start != 0 || ts[0].End != uint32(len(src)) {
			t.Errorf("%s: %+v", src, ts[0])
		}
		if ts[1].Kind != SEMICOLON || !ts[1].Implicit {
			t.Errorf("raw identifier must end a statement: %v", ts)
		}
	}
}

func TestContextualWordsStayIdentifiers(t *testing.T) {
	ts, ds := Lex([]byte("func (self *T) M(where int, yield func(int) bool) { yield(where) }"))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tok := range ts {
		if tok.Text == "self" || tok.Text == "where" || tok.Text == "yield" {
			if tok.Kind != IDENT {
				t.Errorf("ordinary identifier: %+v", tok)
			}
		}
	}
	ts, ds = Lex([]byte("mut ref as where self super in macro_rules async await let fn loop while macro"))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tok := range ts[:len(ts)-2] {
		if tok.Kind != IDENT {
			t.Errorf("reserved tier incorrectly promoted: %+v", tok)
		}
	}
}

func TestAutomaticSemicolons(t *testing.T) {
	for _, src := range []string{"x\n", "123\n", "2.3\n", "2i\n", "'x'\n", "\"x\"\n", "break\n", "continue\n", "fallthrough\n", "return\n", "x++\n", "x--\n", "f()\n", "a[0]\n", "{}\n", "Self\n", "x"} {
		ts, ds := Lex([]byte(src))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		if last := ts[len(ts)-2]; last.Kind != SEMICOLON || !last.Implicit {
			t.Errorf("%q: no implicit semicolon: %v", src, ts)
		}
	}
	for _, src := range []string{"x +\n", "a,\n", "func\n", "if\n", "x;\n"} {
		ts, ds := Lex([]byte(src))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		for _, tok := range ts {
			if tok.Implicit {
				t.Errorf("%q: unexpected semicolon: %v", src, tok)
			}
		}
	}
}

func TestSupplementarySemicolons(t *testing.T) {
	t.Run("postfix question", func(t *testing.T) {
		ts, ds := Lex([]byte("x := f()?\ny := 1"))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		for i, tok := range ts {
			if tok.Kind == QUESTION && (ts[i+1].Kind != SEMICOLON || !ts[i+1].Implicit) {
				t.Fatal(ts)
			}
		}
	})
	t.Run("attribute closing bracket", func(t *testing.T) {
		ts, ds := Lex([]byte("#[derive(a([1], { x: [2] }))]\n#[cfg(feature = \"x\")]\nstruct S {}\na[0]\n"))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		semis := 0
		for _, tok := range ts {
			if tok.Implicit {
				semis++
			}
		}
		if semis != 2 {
			t.Fatalf("want semicolons only after declaration and indexing, got %v", ts)
		}
	})
	t.Run("last match arm", func(t *testing.T) {
		ts, ds := Lex([]byte("match x {\n A => 1,\n B => 2\n}"))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		for i, tok := range ts {
			if tok.Text == "2" && (ts[i+1].Kind != SEMICOLON || !ts[i+1].Implicit || ts[i+2].Kind != RBRACE) {
				t.Fatal(ts)
			}
		}
	})
}

func TestCommentsAndSemicolons(t *testing.T) {
	for _, src := range []string{"x // comment\ny", "x /* comment\ncomment */ y", "x /* comment */\ny"} {
		ts, ds := Lex([]byte(src))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		want := []Kind{IDENT, COMMENT, SEMICOLON, IDENT, SEMICOLON, EOF}
		if !reflect.DeepEqual(kinds(ts), want) {
			t.Errorf("%q: got %v", src, kinds(ts))
		}
	}
	ts, _ := Lex([]byte("#[a] /* comment\ncomment */\nfunc F() {}"))
	for i, tok := range ts {
		if tok.Kind == COMMENT && ts[i+1].Kind == SEMICOLON {
			t.Fatal("attribute newline must not insert semicolon")
		}
	}
}

func TestCommentAndUnicodePositions(t *testing.T) {
	ts, ds := Lex([]byte("α/*\n*/β"))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	want := []struct {
		kind       Kind
		start, end uint32
	}{
		{IDENT, 0, 2}, {COMMENT, 2, 7},
		{SEMICOLON, 4, 4}, {IDENT, 7, 9},
		{SEMICOLON, 9, 9}, {EOF, 9, 9},
	}
	if len(ts) != len(want) {
		t.Fatal(ts)
	}
	for i, w := range want {
		if got := ts[i]; got.Kind != w.kind || got.Start != w.start || got.End != w.end {
			t.Errorf("token %d: got %+v, want %+v", i, got, w)
		}
	}
	ts, ds = Lex([]byte("\ufeffα\r\n"))
	if len(ds) != 0 || ts[0].Start != 3 || ts[0].End != 5 || ts[1].Start != 6 {
		t.Fatalf("BOM and CRLF byte positions: %v, %v", ts, ds)
	}
	for _, src := range []string{"//\xff\n", "/*\ufeff*/", "`\x00`", "\"\xff\""} {
		if _, ds := Lex([]byte(src)); len(ds) == 0 {
			t.Errorf("source encoding error hidden inside comment or literal: %q", src)
		}
	}
}

func TestNumericLiterals(t *testing.T) {
	valid := map[Kind][]string{
		INT:   {"0", "42", "0600", "0_600", "0b1010", "0B_1010", "0o600", "0O_600", "0xBadFace", "0X_Bad_Face", "1_000"},
		FLOAT: {"0.", "72.40", "072.40", ".25", "1e6", "1.e+0", "1_5.0_0e+0_2", "0x1p-2", "0x1.fp+2", "0X.8p-0", "0x_1.ff_fp+0_2"},
		IMAG:  {"0i", "0129i", "0o123i", "0xabci", "0.i", ".5i", "1e3i", "0x1p-2i"},
	}
	for want, literals := range valid {
		for _, src := range literals {
			ts, ds := Lex([]byte(src))
			if len(ds) != 0 || ts[0].Kind != want || ts[0].Text != src {
				t.Errorf("%s: got %v, diagnostics %v", src, ts, ds)
			}
		}
	}
	for _, src := range []string{"0x", "0b2", "0o9", "08", "1e", "1p2", "0b1.0", "0o1e2", "0x1.2", "1__2", "1_", "1_.0", "1._0", "1e_2", "0x__1", "0x1p_2"} {
		if _, ds := Lex([]byte(src)); len(ds) == 0 {
			t.Errorf("%s: expected diagnostic", src)
		}
	}
}

func TestLiteralsAndUnicode(t *testing.T) {
	for _, src := range []string{"\"hello\\n\\u4e16\\U0001f600\"", "'世'", "'\\x41'", "`raw\nstring\r`", "世界 := 1", "\ufeffpackage p"} {
		if _, ds := Lex([]byte(src)); len(ds) != 0 {
			t.Errorf("%q: %v", src, ds)
		}
	}
	for _, src := range []string{"'ab'", "''", "'\\uD800'", "\"\\q\"", "\"line\nnext", "`unclosed", "/* unclosed", "\x00", "x\ufeff", "\xff"} {
		if _, ds := Lex([]byte(src)); len(ds) == 0 {
			t.Errorf("%q: expected diagnostics", src)
		}
	}
}

func TestErrorRecovery(t *testing.T) {
	src := "@\n\"unterminated\nfunc after() {}"
	ts, ds := Lex([]byte(src))
	if len(ds) < 2 {
		t.Fatal(ds)
	}
	found := false
	for _, tok := range ts {
		if tok.Kind == IDENT && tok.Text == "after" {
			found = true
		}
	}
	if !found || ts[len(ts)-1].Kind != EOF {
		t.Fatal("lexing did not recover", ts)
	}
	for _, d := range ds {
		if d.Start > d.End || int(d.End) > len(src) || strings.TrimSpace(d.Message) == "" {
			t.Fatalf("invalid diagnostic: %+v", d)
		}
	}
}

func TestEmptySource(t *testing.T) {
	for _, src := range []string{"", " \t\r\n", "\ufeff"} {
		ts, ds := Lex([]byte(src))
		want := []Token{{Kind: EOF, Start: uint32(len(src)), End: uint32(len(src))}}
		if len(ds) != 0 || !reflect.DeepEqual(ts, want) {
			t.Errorf("%q: got %v, %v; want %v", src, ts, ds, want)
		}
	}
}

func TestCompleteVocabulary(t *testing.T) {
	for kind := ADD; kind <= SELF; kind++ {
		src := kind.String()
		ts, ds := Lex([]byte(src))
		if len(ds) != 0 {
			t.Errorf("%q: unexpected diagnostics: %v", src, ds)
			continue
		}
		want := Token{Kind: kind, Text: src, End: uint32(len(src))}
		if ts[0] != want {
			t.Errorf("%q: got %+v, want %+v", src, ts[0], want)
		}
	}
}

func TestDiagnosticByteOffsets(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   Diagnostic
	}{
		{"α 0b2", Diagnostic{3, 6, "invalid digit in numeric literal"}},
		{"α 1e+", Diagnostic{3, 6, "exponent has no digits"}},
		{"α \"\\q\"", Diagnostic{3, 7, "invalid string or rune literal"}},
		{"α /*", Diagnostic{3, 5, "unterminated block comment"}},
		{"α @", Diagnostic{3, 4, "unexpected character '@'"}},
	} {
		ts, ds := Lex([]byte(tc.source))
		if !reflect.DeepEqual(ds, []Diagnostic{tc.want}) {
			t.Errorf("%q: got %v, want %v", tc.source, ds, tc.want)
		}
		if last := ts[len(ts)-1]; last.Kind != EOF || last.Start != uint32(len(tc.source)) || last.End != last.Start {
			t.Errorf("%q: invalid EOF: %+v", tc.source, last)
		}
	}
}

func TestLexLimitedMatchesUnboundedLex(t *testing.T) {
	for _, source := range []string{
		"", "\ufeff", "\ufeffα\r\n", "r#match >>= 0x1.fp+2;",
		"#[derive(Debug)]\nstruct 世界 { Name string }\n",
		"x /* block\ncomment */ y // line\n",
		"0b2 1e+ @ 'ab' \"\\q\" /*\x00*/\xff",
	} {
		wantTokens, wantDiagnostics := Lex([]byte(source))
		for _, limit := range []int{-1, 0, 1000} {
			gotTokens, gotDiagnostics := LexLimited([]byte(source), limit)
			if !reflect.DeepEqual(gotTokens, wantTokens) || !reflect.DeepEqual(gotDiagnostics, wantDiagnostics) {
				t.Errorf("%q limit %d: tokens %v, diagnostics %v; want %v, %v", source, limit, gotTokens, gotDiagnostics, wantTokens, wantDiagnostics)
			}
		}
	}
}

func TestLexLimitedCountsCommentsSemicolonsAndEOF(t *testing.T) {
	for _, source := range []string{"", "+", "x", "// comment", "x/*\n*/y", "x;"} {
		wantTokens, wantDiagnostics := Lex([]byte(source))
		atLimit, diagnostics := LexLimited([]byte(source), len(wantTokens))
		if !reflect.DeepEqual(atLimit, wantTokens) || !reflect.DeepEqual(diagnostics, wantDiagnostics) {
			t.Errorf("%q: exact token budget changed output: %v, %v", source, atLimit, diagnostics)
		}
		if len(wantTokens) == 1 {
			continue
		}
		limited, diagnostics := LexLimited([]byte(source), len(wantTokens)-1)
		if len(limited) != len(wantTokens)-1 || len(diagnostics) != 1 || diagnostics[0].Message != "token count budget exceeded" {
			t.Errorf("%q: budget did not count every physical token: %v, %v", source, limited, diagnostics)
		}
		if !reflect.DeepEqual(limited, wantTokens[:len(wantTokens)-1]) {
			t.Errorf("%q: budget changed the valid token prefix", source)
		}
	}
}

func TestLexLimitedStopsBeforeInvalidSuffix(t *testing.T) {
	source := strings.Repeat("+ ", 1000) + "\x00\xff"
	tokens, diagnostics := LexLimited([]byte(source), 3)
	if len(tokens) != 3 || len(diagnostics) != 1 {
		t.Fatalf("got %d tokens and %v", len(tokens), diagnostics)
	}
	want := Diagnostic{Start: 5, End: 5, Message: "token count budget exceeded"}
	if diagnostics[0] != want {
		t.Fatalf("scanner inspected invalid suffix after its budget: got %v, want %v", diagnostics[0], want)
	}
}

func TestLexLimitedBoundsDiagnosticsWithinOneToken(t *testing.T) {
	source := "/*" + strings.Repeat("\x00", 1000) + "*/"
	tokens, diagnostics := LexLimited([]byte(source), 2)
	if len(tokens) != 1 || tokens[0].Kind != COMMENT {
		t.Fatalf("expected truncated comment token: %v", tokens)
	}
	want := []Diagnostic{
		{Start: 2, End: 3, Message: "NUL is not permitted in source"},
		{Start: 3, End: 4, Message: "NUL is not permitted in source"},
		{Start: 4, End: 5, Message: "diagnostic count budget exceeded"},
	}
	if !reflect.DeepEqual(diagnostics, want) {
		t.Fatalf("unbounded source diagnostics: got %v, want %v", diagnostics, want)
	}
}
