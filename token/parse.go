package token

import (
	"fmt"

	"github.com/goxide-lang/syntax/internal/lex"
)

type ParseError struct {
	Message string
	At      Span
}

func (e *ParseError) Error() string { return fmt.Sprintf("%s at byte %d", e.Message, e.At.start) }
func (e *ParseError) Span() Span    { return e.At }

// ParseText lexes and groups text. It does not parse expressions, items or
// types, and never recovers binding or input identity from their spelling.
func ParseText(text string, context BindingContext) (TokenStream, error) {
	if len(text) > MaxTextBytes {
		return TokenStream{}, &ParseError{Message: "source exceeds token text budget"}
	}
	tokens, diagnostics := lex.LexLimited([]byte(text), MaxTokens+1)
	if len(diagnostics) != 0 {
		d := diagnostics[0]
		return TokenStream{}, &ParseError{Message: d.Message, At: Span{start: d.Start, end: d.End}}
	}
	p := treeParser{tokens: tokens, context: context}
	stream, err := p.parse(lex.EOF, 0)
	if err != nil {
		return TokenStream{}, err
	}
	if err := stream.Validate(); err != nil {
		return TokenStream{}, &ParseError{Message: err.Error()}
	}
	return stream, nil
}

type treeParser struct {
	tokens  []lex.Token
	pos     int
	count   int
	context BindingContext
}

func (p *treeParser) parse(end lex.Kind, depth int) (TokenStream, error) {
	if depth > MaxDepth {
		return TokenStream{}, &ParseError{Message: "token nesting budget exceeded"}
	}
	var result []Token
	for p.pos < len(p.tokens) {
		source := p.tokens[p.pos]
		if source.Kind == end {
			if end == lex.EOF {
				p.pos++
			}
			return New(result...), nil
		}
		if source.Kind == lex.EOF || source.Kind == lex.RPAREN || source.Kind == lex.RBRACK || source.Kind == lex.RBRACE {
			return TokenStream{}, &ParseError{Message: "unmatched delimiter", At: Span{start: source.Start, end: source.End}}
		}
		p.pos++
		if source.Kind == lex.COMMENT {
			continue
		}
		p.count++
		if p.count > MaxNodes {
			return TokenStream{}, &ParseError{Message: "token count budget exceeded"}
		}
		node := Token{context: p.context, span: Span{start: source.Start, end: source.End}}
		switch source.Kind {
		case lex.IDENT:
			node.kind, node.text, node.raw = IdentKind, source.Text, source.Raw
		case lex.INT, lex.FLOAT, lex.IMAG, lex.CHAR, lex.STRING:
			node.kind, node.text = LiteralKind, source.Text
			for kind, lexerKind := range literalKinds {
				if lexerKind == source.Kind {
					node.literal = kind
				}
			}
		case lex.SEMICOLON:
			node.kind, node.implicit = SemicolonKind, source.Implicit
		case lex.LPAREN, lex.LBRACK, lex.LBRACE:
			node.kind, node.open = GroupKind, node.span
			closeKind := lex.RPAREN
			node.delimiter = Parenthesis
			if source.Kind == lex.LBRACK {
				closeKind, node.delimiter = lex.RBRACK, Bracket
			} else if source.Kind == lex.LBRACE {
				closeKind, node.delimiter = lex.RBRACE, Brace
			}
			var err error
			node.children, err = p.parse(closeKind, depth+1)
			if err != nil {
				return TokenStream{}, err
			}
			close := p.tokens[p.pos]
			p.pos++
			node.close = Span{start: close.Start, end: close.End}
			node.span.end = close.End
		default:
			node.text = source.Text
			if source.Kind.IsKeyword() {
				node.kind = KeywordKind
			} else {
				node.kind = PunctKind
			}
		}
		result = append(result, node)
	}
	return TokenStream{}, &ParseError{Message: "unmatched delimiter"}
}
