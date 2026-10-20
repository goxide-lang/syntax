package token

import "fmt"

type size struct{ nodes, tokens, text, depth int }

func (s *size) add(other size) error {
	s.nodes += other.nodes
	s.tokens += other.tokens
	s.text += other.text
	if s.nodes > MaxNodes || s.tokens > MaxTokens || s.text > MaxTextBytes {
		return fmt.Errorf("token output budget exceeded")
	}
	if other.depth > s.depth {
		s.depth = other.depth
	}
	return nil
}

// Validate checks lexical shape and the same structural limits used by the
// wire protocol. Reference ownership is additionally checked by Scope.
func (s TokenStream) Validate() error {
	_, err := measure(s)
	return err
}

func measure(stream TokenStream) (size, error) {
	var total size
	var visit func(Token, int) error
	visit = func(node Token, depth int) error {
		if depth > MaxDepth {
			return fmt.Errorf("token nesting budget exceeded")
		}
		if err := lexical(node, node.input != nil || node.kind == FragmentKind); err != nil {
			return err
		}
		if node.input != nil {
			ref := node.input
			if ref.scope == nil || ref.id == 0 || ref.id > uint32(len(ref.scope.sizes)) {
				return fmt.Errorf("unknown input reference")
			}
			sz := ref.scope.sizes[ref.id-1]
			sz.depth += depth
			if sz.depth > MaxDepth {
				return fmt.Errorf("referenced token nesting budget exceeded")
			}
			return total.add(sz)
		}
		nesting := depth
		if node.kind == GroupKind {
			nesting++
		}
		if nesting > MaxDepth {
			return fmt.Errorf("token nesting budget exceeded")
		}
		if err := total.add(size{nodes: 1, tokens: lexicalSize(node), text: len(node.text), depth: nesting}); err != nil {
			return err
		}
		for _, child := range node.children.tokens {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, node := range stream.tokens {
		if err := visit(node, 0); err != nil {
			return size{}, err
		}
	}
	return total, nil
}

func lexicalSize(node Token) int {
	if node.kind == GroupKind {
		return 2
	}
	if node.kind == FragmentKind {
		return 0
	}
	return 1
}

func lexical(node Token, original bool) error {
	if len(node.text) > MaxTextBytes {
		return fmt.Errorf("token spelling budget exceeded")
	}
	if node.kind != IdentKind && node.raw ||
		node.kind != LiteralKind && node.literal != "" ||
		node.kind != GroupKind && node.delimiter != "" ||
		node.kind != SemicolonKind && node.implicit ||
		node.kind != FragmentKind && node.fragment != "" {
		return fmt.Errorf("token fields do not match kind %q", node.kind)
	}
	if node.kind != GroupKind && node.kind != FragmentKind && node.children.Len() != 0 {
		return fmt.Errorf("token cannot have children")
	}
	var err error
	switch node.kind {
	case IdentKind:
		_, err = NewIdent(node.text, node.raw, node.context)
	case KeywordKind:
		_, err = NewKeyword(node.text, node.context)
	case LiteralKind:
		_, err = NewLiteral(node.literal, node.text, node.context)
	case PunctKind:
		_, err = NewPunct(node.text, node.context)
	case SemicolonKind:
		if node.text != "" {
			err = fmt.Errorf("semicolon cannot carry a spelling")
		}
	case GroupKind:
		_, err = NewGroup(node.delimiter, node.children, node.context)
		if node.text != "" {
			err = fmt.Errorf("group cannot carry a spelling")
		}
	case FragmentKind:
		if !original || !validFragment(node.fragment) || node.text != "" {
			err = fmt.Errorf("fragment requires an issued category and reference")
		}
	default:
		err = fmt.Errorf("invalid token kind %q", node.kind)
	}
	return err
}
