// Package token provides Goxide token trees without depending on a compiler.
package token

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/goxide-lang/syntax/internal/lex"
)

const (
	MaxDepth     = 128
	MaxNodes     = 100_000
	MaxTokens    = 100_000
	MaxTextBytes = 8 << 20
)

type Kind string

const (
	IdentKind     Kind = "ident"
	KeywordKind   Kind = "keyword"
	LiteralKind   Kind = "literal"
	PunctKind     Kind = "punct"
	SemicolonKind Kind = "semicolon"
	GroupKind     Kind = "group"
	FragmentKind  Kind = "fragment"
)

type LiteralType string

const (
	Integer   LiteralType = "int"
	Float     LiteralType = "float"
	Imaginary LiteralType = "imag"
	Character LiteralType = "char"
	String    LiteralType = "string"
)

type Delimiter string

const (
	Parenthesis Delimiter = "parenthesis"
	Bracket     Delimiter = "bracket"
	Brace       Delimiter = "brace"
)

// FragmentType records syntax already parsed by the host. It is not a type or
// permission proof, and cannot be changed while retaining the input reference.
type FragmentType string

const (
	ExprFragment  FragmentType = "expr"
	TypeFragment  FragmentType = "type"
	StmtFragment  FragmentType = "stmt"
	ItemFragment  FragmentType = "item"
	IdentFragment FragmentType = "ident"
	PathFragment  FragmentType = "path"
	TTFragment    FragmentType = "tt"
)

// Range uses byte offsets. It does not name a file or carry binding information.
type Range struct {
	Start uint32 `json:"start"`
	End   uint32 `json:"end"`
}

// Span is a diagnostic location. A zero reference denotes generated text.
type Span struct {
	scope *Scope
	ref   uint32
	start uint32
	end   uint32
}

func (s Span) Range() (uint32, uint32) { return s.start, s.end }
func (s Span) Reference() uint32       { return s.ref }

// LocalSpan describes offsets in generated text, not a host source file.
func LocalSpan(start, end uint32) (Span, error) {
	if end < start {
		return Span{}, fmt.Errorf("invalid span %d..%d", start, end)
	}
	return Span{start: start, end: end}, nil
}

// BindingContext is an opaque, invocation-local name binding. Its zero value
// works in offline tools; a macro response binds new zero-context tokens at the
// definition site supplied by the host.
type BindingContext struct {
	scope *Scope
	ref   uint32
}

func (c BindingContext) Reference() uint32 { return c.ref }

type inputRef struct {
	scope *Scope
	id    uint32
}

// Token is immutable. Reconstructing a token with a constructor creates new
// syntax; WithSpan only changes its diagnostic location.
type Token struct {
	kind      Kind
	text      string
	raw       bool
	literal   LiteralType
	delimiter Delimiter
	implicit  bool
	context   BindingContext
	span      Span
	open      Span
	close     Span
	children  TokenStream
	fragment  FragmentType
	input     *inputRef
}

func (t Token) Kind() Kind                   { return t.kind }
func (t Token) Text() string                 { return t.text }
func (t Token) Raw() bool                    { return t.raw }
func (t Token) LiteralType() LiteralType     { return t.literal }
func (t Token) Delimiter() Delimiter         { return t.delimiter }
func (t Token) Implicit() bool               { return t.implicit }
func (t Token) Context() BindingContext      { return t.context }
func (t Token) Span() Span                   { return t.span }
func (t Token) DelimiterSpans() (Span, Span) { return t.open, t.close }
func (t Token) Children() TokenStream        { return t.children }
func (t Token) FragmentType() FragmentType   { return t.fragment }
func (t Token) WithSpan(span Span) Token     { t.span = span; return t }
func (t Token) WithDelimiterSpans(open, close Span) Token {
	t.open, t.close = open, close
	return t
}

// Reference identifies an original input in its Scope; zero means new syntax.
func (t Token) Reference() uint32 {
	if t.input != nil {
		return t.input.id
	}
	return 0
}

type TokenStream struct{ tokens []Token }

func New(tokens ...Token) TokenStream {
	return TokenStream{tokens: append([]Token(nil), tokens...)}
}

func (s TokenStream) Len() int        { return len(s.tokens) }
func (s TokenStream) IsEmpty() bool   { return len(s.tokens) == 0 }
func (s TokenStream) Tokens() []Token { return append([]Token(nil), s.tokens...) }

func (s TokenStream) Append(tokens ...Token) TokenStream {
	all := make([]Token, 0, len(s.tokens)+len(tokens))
	all = append(all, s.tokens...)
	return TokenStream{tokens: append(all, tokens...)}
}

func NewIdent(name string, raw bool, context BindingContext) (Token, error) {
	if len(name) > MaxTextBytes {
		return Token{}, fmt.Errorf("identifier exceeds token text budget")
	}
	if !validIdent(name) || !raw && lex.Lookup(name).IsKeyword() {
		return Token{}, fmt.Errorf("invalid identifier %q", name)
	}
	return Token{kind: IdentKind, text: name, raw: raw, context: context}, nil
}

func validIdent(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && !unicode.IsLetter(r) && (i == 0 || !unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

func NewKeyword(text string, context BindingContext) (Token, error) {
	if !lex.Lookup(text).IsKeyword() {
		return Token{}, fmt.Errorf("invalid keyword %q", text)
	}
	return Token{kind: KeywordKind, text: text, context: context}, nil
}

func NewLiteral(kind LiteralType, text string, context BindingContext) (Token, error) {
	if len(text) > MaxTextBytes {
		return Token{}, fmt.Errorf("literal exceeds token text budget")
	}
	lexed, diagnostics := lex.LexLimited([]byte(text), 3)
	want, ok := literalKinds[kind]
	if !ok || len(diagnostics) != 0 || len(lexed) == 0 ||
		lexed[0].Kind != want || lexed[0].Start != 0 || int(lexed[0].End) != len(text) {
		return Token{}, fmt.Errorf("invalid %s literal %q", kind, text)
	}
	return Token{kind: LiteralKind, text: text, literal: kind, context: context}, nil
}

var literalKinds = map[LiteralType]lex.Kind{
	Integer: lex.INT, Float: lex.FLOAT, Imaginary: lex.IMAG,
	Character: lex.CHAR, String: lex.STRING,
}

func NewPunct(text string, context BindingContext) (Token, error) {
	for k := lex.ADD; k <= lex.DOLLAR; k++ {
		if k.String() == text && k != lex.SEMICOLON && !isDelimiter(k) {
			return Token{kind: PunctKind, text: text, context: context}, nil
		}
	}
	return Token{}, fmt.Errorf("invalid punctuation %q", text)
}

func isDelimiter(k lex.Kind) bool {
	return k == lex.LPAREN || k == lex.RPAREN || k == lex.LBRACK ||
		k == lex.RBRACK || k == lex.LBRACE || k == lex.RBRACE
}

func NewSemicolon(context BindingContext) Token {
	return Token{kind: SemicolonKind, context: context}
}

func NewGroup(delimiter Delimiter, children TokenStream, context BindingContext) (Token, error) {
	if delimiter != Parenthesis && delimiter != Bracket && delimiter != Brace {
		return Token{}, fmt.Errorf("invalid delimiter %q", delimiter)
	}
	return Token{kind: GroupKind, delimiter: delimiter, children: children, context: context}, nil
}

// Display is readable source text. It deliberately does not serialize binding,
// provenance, reference identity, whitespace or comments.
func (s TokenStream) Display() string {
	var out strings.Builder
	var write func(TokenStream)
	write = func(stream TokenStream) {
		for i, t := range stream.tokens {
			if i > 0 {
				out.WriteByte(' ')
			}
			switch t.kind {
			case GroupKind:
				open, close := delimiters(t.delimiter)
				out.WriteString(open)
				write(t.children)
				out.WriteString(close)
			case FragmentKind:
				write(t.children)
			case SemicolonKind:
				if t.implicit {
					out.WriteByte('\n')
				} else {
					out.WriteByte(';')
				}
			case IdentKind:
				if t.raw {
					out.WriteString("r#")
				}
				out.WriteString(t.text)
			default:
				out.WriteString(t.text)
			}
		}
	}
	write(s)
	return out.String()
}

func delimiters(d Delimiter) (string, string) {
	switch d {
	case Parenthesis:
		return "(", ")"
	case Bracket:
		return "[", "]"
	case Brace:
		return "{", "}"
	}
	return "", ""
}
