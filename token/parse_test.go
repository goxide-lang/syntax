package token_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/goxide-lang/syntax/token"
)

func testParse(t *testing.T, text string) []token.Token {
	t.Helper()
	stream, err := token.ParseText(text, token.BindingContext{})
	if err != nil {
		t.Fatalf("ParseText(%q): %v", text, err)
	}
	return stream.Tokens()
}

func TestParseVocabulary(t *testing.T) {
	for _, name := range keywords {
		got := testParse(t, name+";")
		if len(got) != 2 || got[0].Kind() != token.KeywordKind || got[0].Text() != name || got[0].Raw() {
			t.Errorf("keyword %q: %v", name, got)
		}
		raw := testParse(t, "r#"+name)
		if len(raw) != 2 || raw[0].Kind() != token.IdentKind || raw[0].Text() != name || !raw[0].Raw() {
			t.Errorf("raw keyword %q: %v", name, raw)
		}
	}
	for _, name := range append([]string{"世界", "α2", "_", "字٣"}, ordinaryNames...) {
		got := testParse(t, name)
		if len(got) != 2 || got[0].Kind() != token.IdentKind || got[0].Text() != name || got[0].Raw() {
			t.Errorf("ordinary name %q: %v", name, got)
		}
	}
	raw := testParse(t, "r#世界")
	if raw[0].Text() != "世界" || !raw[0].Raw() {
		t.Fatal(raw)
	}
	if start, end := raw[0].Span().Range(); start != 0 || end != 8 {
		t.Fatalf("raw identifier span must include r# and UTF-8 bytes: %d..%d", start, end)
	}
	got := testParse(t, strings.Join(punctuation, " "))
	if len(got) != len(punctuation) {
		t.Fatalf("punctuation count: got %d, want %d", len(got), len(punctuation))
	}
	for i, text := range punctuation {
		if got[i].Kind() != token.PunctKind || got[i].Text() != text {
			t.Errorf("punctuation %q split or changed: %v", text, got[i])
		}
	}
}

func TestParseFiveLiteralKindsAndNegativeNumbers(t *testing.T) {
	for _, tc := range []struct {
		source string
		kind   token.LiteralType
	}{
		{"0x_1f", token.Integer}, {".25", token.Float}, {"0x1p-2i", token.Imaginary},
		{"'世'", token.Character}, {"`raw\nstring`", token.String},
	} {
		got := testParse(t, tc.source)
		if len(got) != 2 || got[0].Kind() != token.LiteralKind || got[0].LiteralType() != tc.kind || got[0].Text() != tc.source {
			t.Errorf("%q: %v", tc.source, got)
		}
	}
	for _, tc := range []struct {
		source string
		kind   token.LiteralType
	}{
		{"-12", token.Integer}, {"-1.5", token.Float}, {"-2i", token.Imaginary},
	} {
		got := testParse(t, tc.source)
		if len(got) != 3 || got[0].Kind() != token.PunctKind || got[0].Text() != "-" ||
			got[1].Kind() != token.LiteralKind || got[1].LiteralType() != tc.kind || got[1].Text() != tc.source[1:] {
			t.Errorf("negative literal must be punctuation plus literal: %q: %v", tc.source, got)
		}
	}
}

func TestParseSemicolons(t *testing.T) {
	for _, tc := range []struct {
		source   string
		start    uint32
		end      uint32
		implicit bool
	}{
		{"x", 1, 1, true}, {"x\n", 1, 1, true}, {"x;", 1, 2, false},
		{"α\r\n", 3, 3, true}, {"Self\n", 4, 4, true}, {"f()?\n", 4, 4, true},
	} {
		got := testParse(t, tc.source)
		last := got[len(got)-1]
		start, end := last.Span().Range()
		if last.Kind() != token.SemicolonKind || last.Implicit() != tc.implicit || start != tc.start || end != tc.end {
			t.Errorf("%q: last token %v at %d..%d", tc.source, last, start, end)
		}
	}
	for _, source := range []string{"x +\n", "x,\n", "func\n", "if\n"} {
		for _, got := range testParse(t, source) {
			if got.Kind() == token.SemicolonKind {
				t.Errorf("%q: unexpected semicolon", source)
			}
		}
	}
	attributes := testParse(t, "#[derive(Debug)]\n#![cfg(feature = \"x\")]\nstruct S {}\n")
	semicolons := 0
	for _, got := range attributes {
		if got.Kind() == token.SemicolonKind {
			semicolons++
		}
	}
	if semicolons != 1 || attributes[len(attributes)-1].Kind() != token.SemicolonKind {
		t.Fatalf("attribute closing brackets inserted semicolons: %v", attributes)
	}
}

func TestParseCommentsAndBOM(t *testing.T) {
	for _, source := range []string{"x // line\ny", "x /* block\ncomment */ y", "x /* block */\ny"} {
		got := testParse(t, source)
		if len(got) != 4 || got[0].Text() != "x" || got[1].Kind() != token.SemicolonKind ||
			!got[1].Implicit() || got[2].Text() != "y" || got[3].Kind() != token.SemicolonKind || !got[3].Implicit() {
			t.Errorf("comment or newline handling changed: %q: %v", source, got)
		}
	}
	afterAttribute := testParse(t, "#[a] /* comment\ncomment */\nstruct S {}")
	for _, got := range afterAttribute[:len(afterAttribute)-1] {
		if got.Kind() == token.SemicolonKind {
			t.Fatal("comment after attribute re-enabled implicit semicolon")
		}
	}
	for _, source := range []string{"", " \t\r\n", "// comment", "/* comment\n*/", "\ufeff"} {
		if got := testParse(t, source); len(got) != 0 {
			t.Errorf("%q: expected empty stream, got %v", source, got)
		}
	}
	bom := testParse(t, "\ufeffα\r\n")
	if start, end := bom[0].Span().Range(); start != 3 || end != 5 {
		t.Fatalf("BOM shifted Unicode byte span: %d..%d", start, end)
	}
	if start, end := bom[1].Span().Range(); start != 6 || end != 6 {
		t.Fatalf("CRLF semicolon span: %d..%d", start, end)
	}
}

func TestParseGroupsAndDelimiterSpans(t *testing.T) {
	got := testParse(t, "([{}]);")
	if len(got) != 2 || got[1].Kind() != token.SemicolonKind || got[1].Implicit() {
		t.Fatal(got)
	}
	group := got[0]
	for i, delimiter := range []token.Delimiter{token.Parenthesis, token.Bracket, token.Brace} {
		if group.Kind() != token.GroupKind || group.Delimiter() != delimiter {
			t.Fatalf("depth %d: got %v", i, group)
		}
		start, end := group.Span().Range()
		open, close := group.DelimiterSpans()
		openStart, openEnd := open.Range()
		closeStart, closeEnd := close.Range()
		if start != uint32(i) || end != uint32(6-i) || openStart != start || openEnd != start+1 ||
			closeStart != end-1 || closeEnd != end {
			t.Errorf("depth %d: full=%d..%d open=%d..%d close=%d..%d", i, start, end, openStart, openEnd, closeStart, closeEnd)
		}
		children := group.Children().Tokens()
		if i < 2 {
			if len(children) != 1 {
				t.Fatal(children)
			}
			group = children[0]
		} else if len(children) != 0 {
			t.Fatal("innermost brace must be empty", children)
		}
	}
}

func TestParseErrorsHaveByteLocations(t *testing.T) {
	for _, tc := range []struct {
		source  string
		message string
		start   uint32
		end     uint32
	}{
		{"(", "unmatched delimiter", 1, 1},
		{"[)", "unmatched delimiter", 1, 2},
		{"([)]", "unmatched delimiter", 2, 3},
		{"α)", "unmatched delimiter", 2, 3},
		{"α @", "unexpected character", 3, 4},
		{"α 0b2", "invalid digit", 3, 6},
		{"α \"\\q\"", "invalid string or rune literal", 3, 7},
		{"α /*", "unterminated block comment", 3, 5},
		{"α \xff", "invalid UTF-8", 3, 4},
		{"α \x00", "NUL", 3, 4},
		{"α \ufeff", "byte order mark", 3, 6},
	} {
		stream, err := token.ParseText(tc.source, token.BindingContext{})
		var parseError *token.ParseError
		if !errors.As(err, &parseError) {
			t.Errorf("%q: expected ParseError, got %v", tc.source, err)
			continue
		}
		if !stream.IsEmpty() {
			t.Errorf("%q: failure returned a partial stream", tc.source)
		}
		start, end := parseError.Span().Range()
		if start != tc.start || end != tc.end || !strings.Contains(err.Error(), tc.message) {
			t.Errorf("%q: got %v at %d..%d; want %q at %d..%d", tc.source, err, start, end, tc.message, tc.start, tc.end)
		}
	}
}

func TestParseNestingAndTextBudgets(t *testing.T) {
	text := strings.Repeat("(", token.MaxDepth) + strings.Repeat(")", token.MaxDepth)
	if _, err := token.ParseText(text, token.BindingContext{}); err != nil {
		t.Fatalf("nesting at the limit rejected: %v", err)
	}
	for _, source := range []string{"(" + text + ")", strings.Repeat(" ", token.MaxTextBytes+1)} {
		if _, err := token.ParseText(source, token.BindingContext{}); err == nil {
			t.Fatal("resource budget exceeded without an error")
		}
	}
}
