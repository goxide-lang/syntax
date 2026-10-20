package tokenbench

import (
	_ "embed"
	"testing"

	"github.com/goxide-lang/syntax/token"
)

// cloneDebugSource is an actual compiler acceptance program, copied from
// goxide/internal/lower/testdata/derive/clone_debug/main.gox.
//
//go:embed testdata/clone_debug.gox
var cloneDebugSource string

// seed is the shared, prevalidated input for both representations. Preparation,
// including lexing, validation, spans and invocation references, is not timed.
type seed struct {
	kind      token.Kind
	text      string
	raw       bool
	literal   token.LiteralType
	delimiter token.Delimiter
	implicit  bool
	context   token.BindingContext
	span      token.Span
	open      token.Span
	close     token.Span
	children  []seed
	fragment  token.FragmentType
	input     *inputReference
}

func sourceSeeds(tb testing.TB, source string) []seed {
	tb.Helper()
	scope := token.NewScope("tokenbench")
	context := scope.NewContext()
	stream, err := token.ParseText(source, context)
	if err != nil {
		tb.Fatal(err)
	}
	stream, err = scope.Capture(stream, context)
	if err != nil {
		tb.Fatal(err)
	}
	return seedsFromStream(scope, stream, make(map[uint32]*inputReference))
}

func seedsFromStream(scope *token.Scope, stream token.TokenStream, references map[uint32]*inputReference) []seed {
	if stream.IsEmpty() {
		return nil
	}
	nodes := stream.Tokens()
	result := make([]seed, len(nodes))
	for i, node := range nodes {
		open, close := node.DelimiterSpans()
		var input *inputReference
		if id := node.Reference(); id != 0 {
			input = references[id]
			if input == nil {
				input = &inputReference{scope: scope, id: id}
				references[id] = input
			}
		}
		result[i] = seed{
			kind: node.Kind(), text: node.Text(), raw: node.Raw(),
			literal: node.LiteralType(), delimiter: node.Delimiter(),
			implicit: node.Implicit(), context: node.Context(), span: node.Span(),
			open: open, close: close, children: seedsFromStream(scope, node.Children(), references),
			fragment: node.FragmentType(), input: input,
		}
	}
	return result
}

func buildFlat(source []seed) []flatToken {
	if len(source) == 0 {
		return nil
	}
	result := make([]flatToken, len(source))
	for i, node := range source {
		result[i] = flatToken{
			kind: node.kind, text: node.text, raw: node.raw,
			literal: node.literal, delimiter: node.delimiter, implicit: node.implicit,
			context: node.context, span: node.span, open: node.open, close: node.close,
			children: flatStream{tokens: buildFlat(node.children)},
			fragment: node.fragment, input: node.input,
		}
	}
	return result
}

func buildCandidate(source []seed) []Candidate {
	if len(source) == 0 {
		return nil
	}
	result := make([]Candidate, len(source))
	for i, node := range source {
		var payload Payload
		switch node.kind {
		case token.IdentKind:
			payload = &Ident{Text: node.text, Raw: node.raw}
		case token.KeywordKind:
			payload = &Keyword{Text: node.text}
		case token.LiteralKind:
			payload = &Literal{Kind: node.literal, Text: node.text}
		case token.PunctKind:
			payload = &Punct{Text: node.text}
		case token.SemicolonKind:
			payload = &Semicolon{Implicit: node.implicit}
		case token.GroupKind:
			payload = &Group{
				Delimiter: node.delimiter, Open: node.open, Close: node.close,
				Children: buildCandidate(node.children),
			}
		case token.FragmentKind:
			payload = &Fragment{Kind: node.fragment, View: buildCandidate(node.children)}
		default:
			panic("tokenbench: invalid seed kind")
		}
		result[i] = Candidate{Context: node.context, Span: node.span, Input: node.input, Payload: payload}
	}
	return result
}

func flatSeeds(nodes []flatToken) []seed {
	if len(nodes) == 0 {
		return nil
	}
	result := make([]seed, len(nodes))
	for i, node := range nodes {
		result[i] = seed{
			kind: node.kind, text: node.text, raw: node.raw,
			literal: node.literal, delimiter: node.delimiter, implicit: node.implicit,
			context: node.context, span: node.span, open: node.open, close: node.close,
			children: flatSeeds(node.children.tokens), fragment: node.fragment, input: node.input,
		}
	}
	return result
}

func candidateSeeds(nodes []Candidate) []seed {
	if len(nodes) == 0 {
		return nil
	}
	result := make([]seed, len(nodes))
	for i, node := range nodes {
		value := seed{context: node.Context, span: node.Span, input: node.Input}
		switch payload := node.Payload.(type) {
		case *Ident:
			value.kind, value.text, value.raw = token.IdentKind, payload.Text, payload.Raw
		case *Keyword:
			value.kind, value.text = token.KeywordKind, payload.Text
		case *Literal:
			value.kind, value.literal, value.text = token.LiteralKind, payload.Kind, payload.Text
		case *Punct:
			value.kind, value.text = token.PunctKind, payload.Text
		case *Semicolon:
			value.kind, value.implicit = token.SemicolonKind, payload.Implicit
		case *Group:
			value.kind, value.delimiter = token.GroupKind, payload.Delimiter
			value.open, value.close, value.children = payload.Open, payload.Close, candidateSeeds(payload.Children)
		case *Fragment:
			value.kind, value.fragment, value.children = token.FragmentKind, payload.Kind, candidateSeeds(payload.View)
		default:
			panic("tokenbench: invalid candidate payload")
		}
		result[i] = value
	}
	return result
}

type fixture struct {
	name  string
	nodes []seed
	count int
}

func benchmarkFixtures(tb testing.TB) []fixture {
	tb.Helper()
	small := sourceSeeds(tb, cloneDebugSource)
	// Repetition changes working-set size without changing the token mix.
	// Strings and metadata are immutable shared inputs; both timed builders
	// allocate every token array and the candidate allocates every payload.
	large := make([]seed, 0, 32*len(small))
	for range 32 {
		large = append(large, small...)
	}
	return []fixture{
		{name: "Program", nodes: small, count: countSeeds(small)},
		{name: "ProgramX32", nodes: large, count: countSeeds(large)},
	}
}

func countSeeds(nodes []seed) int {
	count := len(nodes)
	for _, node := range nodes {
		count += countSeeds(node.children)
	}
	return count
}
