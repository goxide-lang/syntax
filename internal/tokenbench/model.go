// Package tokenbench compares token layouts, without measuring lexing or JSON.
package tokenbench

import "github.com/goxide-lang/syntax/token"

//go:generate goxide gen .

type Span = token.Span
type LiteralType = token.LiteralType
type Delimiter = token.Delimiter
type FragmentType = token.FragmentType

type inputReference struct {
	scope *token.Scope
	id    uint32
}

// flatToken mirrors token.Token. Tests check every field's layout; the two
// private recursive types are checked structurally instead of by nominal name.
type flatToken struct {
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
	children  flatStream
	fragment  token.FragmentType
	input     *inputReference
}

type flatStream struct{ tokens []flatToken }

// Candidate keeps shared metadata outside the generated, variant-specific data.
type Candidate struct {
	Context token.BindingContext
	Span    token.Span
	Input   *inputReference
	Payload Payload
}

func scoreText(value string) uint64 {
	if len(value) == 0 {
		return 0
	}
	return uint64(len(value))*131 + uint64(value[0])*17 + uint64(value[len(value)-1])
}

func scoreBool(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

func scoreSpan(value Span) uint64 {
	start, end := value.Range()
	return uint64(start)*17 + uint64(end)*31 + uint64(value.Reference())
}

func scoreMetadata(context token.BindingContext, span Span, input *inputReference) uint64 {
	result := uint64(context.Reference())*13 + scoreSpan(span)
	if input != nil {
		result += uint64(input.id)*7 + uint64(len(input.scope.ID()))
	}
	return result
}

func traverseFlat(nodes []flatToken) uint64 {
	var score uint64
	for _, node := range nodes {
		score += scoreMetadata(node.context, node.span, node.input)
		switch node.kind {
		case token.IdentKind:
			score += 17 + scoreText(node.text) + scoreBool(node.raw)
		case token.KeywordKind:
			score += 31 + scoreText(node.text)
		case token.LiteralKind:
			score += 47 + scoreText(node.text) + scoreText(string(node.literal))
		case token.PunctKind:
			score += 61 + scoreText(node.text)
		case token.SemicolonKind:
			score += 79 + scoreBool(node.implicit)
		case token.GroupKind:
			score += 97 + scoreText(string(node.delimiter)) + scoreSpan(node.open) + scoreSpan(node.close) + traverseFlat(node.children.tokens)
		case token.FragmentKind:
			score += 109 + scoreText(string(node.fragment)) + traverseFlat(node.children.tokens)
		default:
			panic("tokenbench: invalid flat kind")
		}
	}
	return score
}
