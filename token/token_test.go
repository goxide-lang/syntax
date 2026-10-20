package token_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/syntax/token"
)

var keywords = strings.Fields("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var enum match trait impl use pub mod unsafe declare Self")

var ordinaryNames = strings.Fields("mut ref as where self super in macro_rules async await let fn loop while macro yield derive cfg Option Result Some None Ok Err i32 move dyn static crate")

var punctuation = strings.Fields("+ - * / % & | ^ << >> &^ += -= *= /= %= &= |= ^= <<= >>= &^= && || <- ++ -- == < > = ! != <= >= := ... , . : :: => ? # ~ $")

func testIdent(t *testing.T, name string, raw bool) token.Token {
	t.Helper()
	got, err := token.NewIdent(name, raw, token.BindingContext{})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestIdentifierAndKeywordConstructors(t *testing.T) {
	for _, name := range keywords {
		got, err := token.NewKeyword(name, token.BindingContext{})
		if err != nil || got.Kind() != token.KeywordKind || got.Text() != name || got.Raw() {
			t.Errorf("keyword %q: got %v, %v", name, got, err)
		}
		if _, err := token.NewIdent(name, false, token.BindingContext{}); err == nil {
			t.Errorf("hard keyword %q accepted as ordinary identifier", name)
		}
		raw := testIdent(t, name, true)
		if raw.Kind() != token.IdentKind || raw.Text() != name || !raw.Raw() {
			t.Errorf("raw keyword %q: got %v", name, raw)
		}
	}
	names := append([]string{"_", "_x2", "alpha", "世界", "α2", "字٣"}, ordinaryNames...)
	for _, name := range names {
		for _, raw := range []bool{false, true} {
			got := testIdent(t, name, raw)
			if got.Kind() != token.IdentKind || got.Text() != name || got.Raw() != raw {
				t.Errorf("identifier %q raw=%v: got %v", name, raw, got)
			}
		}
		if _, err := token.NewKeyword(name, token.BindingContext{}); err == nil {
			t.Errorf("ordinary name %q accepted as keyword", name)
		}
	}
	for _, name := range []string{"", "1name", "a-b", "a.b", " x", "x ", "r#match", "e\u0301", "\xff"} {
		for _, raw := range []bool{false, true} {
			if _, err := token.NewIdent(name, raw, token.BindingContext{}); err == nil {
				t.Errorf("invalid identifier %q raw=%v accepted", name, raw)
			}
		}
	}
}

func TestPunctuationConstructor(t *testing.T) {
	for _, text := range punctuation {
		got, err := token.NewPunct(text, token.BindingContext{})
		if err != nil || got.Kind() != token.PunctKind || got.Text() != text {
			t.Errorf("%q: got %v, %v", text, got, err)
		}
	}
	for _, text := range []string{"", "(", ")", "[", "]", "{", "}", ";", "@", "->", " +", "+ ", "+++"} {
		if _, err := token.NewPunct(text, token.BindingContext{}); err == nil {
			t.Errorf("invalid punctuation %q accepted", text)
		}
	}
	semicolon := token.NewSemicolon(token.BindingContext{})
	if semicolon.Kind() != token.SemicolonKind || semicolon.Implicit() {
		t.Fatalf("constructor must create an explicit semicolon: %v", semicolon)
	}
}

func TestLiteralConstructors(t *testing.T) {
	valid := map[token.LiteralType][]string{
		token.Integer:   {"0", "42", "0600", "0b1010", "0o600", "0X_Bad_Face", "1_000"},
		token.Float:     {".25", "1e6", "1.e+0", "0x1.fp+2", "0x_1.ff_fp+0_2"},
		token.Imaginary: {"0129i", ".5i", "0xabci", "0x1p-2i"},
		token.Character: {"'世'", "'\\x41'", "'\\U0001f600'"},
		token.String:    {`"hello\n\u4e16"`, "`raw\nstring`"},
	}
	for kind, examples := range valid {
		for _, text := range examples {
			got, err := token.NewLiteral(kind, text, token.BindingContext{})
			if err != nil || got.Kind() != token.LiteralKind || got.LiteralType() != kind || got.Text() != text {
				t.Errorf("%s %q: got %v, %v", kind, text, got, err)
			}
			for _, wrong := range []token.LiteralType{token.Integer, token.Float, token.Imaginary, token.Character, token.String} {
				if wrong != kind {
					if _, err := token.NewLiteral(wrong, text, token.BindingContext{}); err == nil {
						t.Errorf("%q accepted as %s instead of %s", text, wrong, kind)
					}
				}
			}
		}
	}
	for _, tc := range []struct {
		kind token.LiteralType
		text string
	}{
		{token.Integer, "-1"}, {token.Float, "-1.0"}, {token.Imaginary, "-1i"},
		{token.Integer, ""}, {token.Integer, " 1"}, {token.Integer, "1 "},
		{token.Integer, "1;"},
		{token.Integer, "1 /* comment */"}, {token.Integer, "1 2"},
		{token.Integer, "0b2"}, {token.Integer, "1__2"}, {token.Float, "1e+"},
		{token.Character, "'ab'"}, {token.Character, "''"}, {token.Character, "'\\uD800'"},
		{token.String, `"bad\q"`}, {token.String, "`unclosed"}, {token.String, "\"line\nnext\""},
		{token.LiteralType("number"), "1"},
	} {
		if _, err := token.NewLiteral(tc.kind, tc.text, token.BindingContext{}); err == nil {
			t.Errorf("invalid %s %q accepted", tc.kind, tc.text)
		}
	}
}

func TestStreamsDoNotExposeMutableSlices(t *testing.T) {
	first, second := testIdent(t, "first", false), testIdent(t, "second", false)
	input := []token.Token{first}
	stream := token.New(input...)
	input[0] = second
	if stream.Tokens()[0].Text() != "first" {
		t.Fatal("New retained the caller's slice")
	}
	exported := stream.Tokens()
	exported[0] = second
	if stream.Tokens()[0].Text() != "first" {
		t.Fatal("Tokens exposed the stream's slice")
	}
	appended := []token.Token{second}
	extended := stream.Append(appended...)
	appended[0] = first
	if stream.Len() != 1 || extended.Len() != 2 || extended.Tokens()[1].Text() != "second" {
		t.Fatal("Append mutated the original stream or retained the input slice")
	}
	group, err := token.NewGroup(token.Brace, stream, token.BindingContext{})
	if err != nil {
		t.Fatal(err)
	}
	children := group.Children().Tokens()
	children[0] = second
	extendedChildren := group.Children().Append(second)
	if group.Children().Len() != 1 || group.Children().Tokens()[0].Text() != "first" || extendedChildren.Len() != 2 {
		t.Fatal("Children exposed mutable state")
	}
	var empty token.TokenStream
	if !empty.IsEmpty() || empty.Len() != 0 || len(empty.Tokens()) != 0 || empty.Display() != "" {
		t.Fatal("zero TokenStream must be empty")
	}
	if got := empty.Append(first); got.Len() != 1 || !empty.IsEmpty() {
		t.Fatal("Append changed an empty source stream")
	}
}

func TestGroupsAndLocalSpans(t *testing.T) {
	for _, delimiter := range []token.Delimiter{token.Parenthesis, token.Bracket, token.Brace} {
		group, err := token.NewGroup(delimiter, token.New(testIdent(t, "child", false)), token.BindingContext{})
		if err != nil || group.Kind() != token.GroupKind || group.Delimiter() != delimiter || group.Children().Len() != 1 {
			t.Errorf("%s: got %v, %v", delimiter, group, err)
		}
	}
	if _, err := token.NewGroup(token.Delimiter("none"), token.New(), token.BindingContext{}); err == nil {
		t.Fatal("invalid delimiter accepted")
	}
	if _, err := token.LocalSpan(5, 4); err == nil {
		t.Fatal("reversed span accepted")
	}
	span, err := token.LocalSpan(4, 9)
	if err != nil {
		t.Fatal(err)
	}
	first := testIdent(t, "first", false)
	located := first.WithSpan(span)
	start, end := located.Span().Range()
	if start != 4 || end != 9 || located.Span().Reference() != 0 || located.Reference() != 0 || located.Context().Reference() != 0 {
		t.Fatal("local span acquired host identity or lost its byte range")
	}
	if start, end := first.Span().Range(); start != 0 || end != 0 {
		t.Fatal("WithSpan changed the original token")
	}
	if token.New(located).Display() != "first" {
		t.Fatal("diagnostic span changed displayed syntax")
	}
}

func TestDisplayIsReadableSource(t *testing.T) {
	stream, err := token.ParseText("r#match /* retained only as source trivia */ => [\"héllo\"];\n", token.BindingContext{})
	if err != nil {
		t.Fatal(err)
	}
	display := stream.Display()
	for _, text := range []string{"r#match", "=>", "[", `"héllo"`, "]", ";"} {
		if !strings.Contains(display, text) {
			t.Errorf("display %q lost %q", display, text)
		}
	}
	if strings.Contains(display, "trivia") {
		t.Fatalf("Display unexpectedly preserved comments: %q", display)
	}
	// This checks readable syntax, not a binding/provenance round trip.
	if _, err := token.ParseText(display, token.BindingContext{}); err != nil {
		t.Fatalf("display is not readable token text: %q: %v", display, err)
	}
	withNewline, err := token.ParseText("first\nsecond", token.BindingContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withNewline.Display(), "\n") {
		t.Fatal("implicit semicolons must remain visible as line breaks")
	}
}
