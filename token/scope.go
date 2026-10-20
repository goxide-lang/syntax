package token

import (
	"fmt"
	"unicode/utf8"
)

// Scope is the host-owned reference table for one macro invocation. Its ID
// must be unique among invocations whose responses can still arrive. It is
// constructed before execution, not shared with a running macro.
type Scope struct {
	id       string
	contexts uint32
	spans    []Range
	inputs   []Token
	sizes    []size
	stored   size
}

func NewScope(id string) *Scope { return &Scope{id: id} }
func (s *Scope) ID() string     { return s.id }

func (s *Scope) NewContext() BindingContext {
	s.contexts++
	return BindingContext{scope: s, ref: s.contexts}
}

func (s *Scope) ContextID(context BindingContext) (uint32, error) {
	if context.scope != s || context.ref == 0 || context.ref > s.contexts {
		return 0, fmt.Errorf("binding context belongs to another invocation or is unknown")
	}
	return context.ref, nil
}

func (s *Scope) Context(id uint32) (BindingContext, error) {
	if id == 0 || id > s.contexts {
		return BindingContext{}, fmt.Errorf("unknown binding context %d", id)
	}
	return BindingContext{scope: s, ref: id}, nil
}

func (s *Scope) NewSpan(start, end uint32) (Span, error) {
	if end < start {
		return Span{}, fmt.Errorf("invalid span %d..%d", start, end)
	}
	s.spans = append(s.spans, Range{Start: start, End: end})
	return Span{scope: s, ref: uint32(len(s.spans)), start: start, end: end}, nil
}

type WireSpan struct {
	Ref   uint32 `json:"ref,omitempty"`
	Start uint32 `json:"start"`
	End   uint32 `json:"end"`
}

func (s *Scope) EncodeSpan(span Span) (WireSpan, error) {
	if span.scope != nil && span.scope != s {
		return WireSpan{}, fmt.Errorf("span belongs to another invocation")
	}
	wire := WireSpan{Ref: span.ref, Start: span.start, End: span.end}
	_, err := s.DecodeSpan(wire)
	return wire, err
}

func (s *Scope) DecodeSpan(wire WireSpan) (Span, error) {
	if wire.End < wire.Start {
		return Span{}, fmt.Errorf("invalid span %d..%d", wire.Start, wire.End)
	}
	if wire.Ref == 0 {
		return Span{start: wire.Start, end: wire.End}, nil
	}
	if wire.Ref > uint32(len(s.spans)) {
		return Span{}, fmt.Errorf("unknown span %d", wire.Ref)
	}
	original := s.spans[wire.Ref-1]
	if wire.Start < original.Start || wire.End > original.End {
		return Span{}, fmt.Errorf("span exceeds issued range")
	}
	return Span{scope: s, ref: wire.Ref, start: wire.Start, end: wire.End}, nil
}

// WireToken is an untrusted transport description. An original reference may
// only carry diagnostic span overrides. New fragment descriptions are only
// accepted inside a host's ScopeData, never in a macro response.
type WireToken struct {
	Ref       uint32      `json:"ref,omitempty"`
	Kind      string      `json:"kind,omitempty"`
	Text      string      `json:"text,omitempty"`
	Raw       bool        `json:"raw,omitempty"`
	Literal   string      `json:"literal,omitempty"`
	Delimiter string      `json:"delimiter,omitempty"`
	Implicit  bool        `json:"implicit,omitempty"`
	Context   uint32      `json:"context,omitempty"`
	Span      *WireSpan   `json:"span,omitempty"`
	Open      *WireSpan   `json:"open,omitempty"`
	Close     *WireSpan   `json:"close,omitempty"`
	Children  []WireToken `json:"children,omitempty"`
	Fragment  string      `json:"fragment,omitempty"`
}

// ScopeData is a host-to-guest snapshot, not something a response can replace.
// Inputs are in postorder: children may reference only earlier entries.
type ScopeData struct {
	ID       string      `json:"id"`
	Contexts uint32      `json:"contexts"`
	Spans    []Range     `json:"spans"`
	Inputs   []WireToken `json:"inputs"`
}

func (s *Scope) check() error {
	if s == nil || s.id == "" || len(s.id) > 256 || !utf8.ValidString(s.id) {
		return fmt.Errorf("invalid invocation ID")
	}
	if s.contexts > MaxNodes || len(s.spans) > MaxNodes || len(s.inputs) > MaxNodes {
		return fmt.Errorf("reference table budget exceeded")
	}
	return nil
}

// Capture issues references for an input tree. Existing references from this
// invocation retain their identities. On error the reference table is unchanged.
func (s *Scope) Capture(stream TokenStream, defaultContext BindingContext) (TokenStream, error) {
	if err := s.check(); err != nil {
		return TokenStream{}, err
	}
	if _, err := s.ContextID(defaultContext); err != nil {
		return TokenStream{}, err
	}
	if _, err := measure(stream); err != nil {
		return TokenStream{}, err
	}
	before := len(s.inputs)
	stored := s.stored
	result, err := s.capture(stream, defaultContext)
	if err != nil {
		s.inputs, s.sizes = s.inputs[:before], s.sizes[:before]
		s.stored = stored
	}
	return result, err
}

func (s *Scope) capture(stream TokenStream, defaultContext BindingContext) (TokenStream, error) {
	result := make([]Token, 0, stream.Len())
	for _, node := range stream.tokens {
		if node.input != nil {
			if _, err := s.input(node.input); err != nil {
				return TokenStream{}, err
			}
			if err := s.locations(node); err != nil {
				return TokenStream{}, err
			}
			result = append(result, node)
			continue
		}
		if node.context.ref == 0 {
			node.context = defaultContext
		}
		if _, err := s.ContextID(node.context); err != nil {
			return TokenStream{}, err
		}
		if err := s.locations(node); err != nil {
			return TokenStream{}, err
		}
		var err error
		node.children, err = s.capture(node.children, defaultContext)
		if err != nil {
			return TokenStream{}, err
		}
		node, err = s.addInput(node)
		if err != nil {
			return TokenStream{}, err
		}
		result = append(result, node)
	}
	return New(result...), nil
}

// CaptureFragment is for the compiler bridge after parsing a source fragment.
// The actual AST remains in the host, indexed by the returned Reference.
// Its observable children are new syntax: inspecting them cannot transfer the
// fragment's parsed category or trusted source identity to another output slot.
func (s *Scope) CaptureFragment(kind FragmentType, view TokenStream, context BindingContext) (Token, error) {
	if err := s.check(); err != nil {
		return Token{}, err
	}
	if !validFragment(kind) {
		return Token{}, fmt.Errorf("invalid fragment category %q", kind)
	}
	if _, err := s.ContextID(context); err != nil {
		return Token{}, err
	}
	if _, err := measure(view); err != nil {
		return Token{}, err
	}
	observed, err := s.observe(view, context)
	if err != nil {
		return Token{}, err
	}
	return s.addInput(Token{kind: FragmentKind, fragment: kind, children: observed, context: context})
}

func (s *Scope) observe(stream TokenStream, defaultContext BindingContext) (TokenStream, error) {
	var result []Token
	for _, node := range stream.tokens {
		if node.input != nil {
			if _, err := s.input(node.input); err != nil {
				return TokenStream{}, err
			}
		}
		node.input = nil
		if node.context.ref == 0 {
			node.context = defaultContext
		}
		if _, err := s.ContextID(node.context); err != nil {
			return TokenStream{}, err
		}
		if err := s.locations(node); err != nil {
			return TokenStream{}, err
		}
		children, err := s.observe(node.children, defaultContext)
		if err != nil {
			return TokenStream{}, err
		}
		if node.kind == FragmentKind {
			result = append(result, children.tokens...)
		} else {
			node.children = children
			result = append(result, node)
		}
	}
	return New(result...), nil
}

func (s *Scope) addInput(node Token) (Token, error) {
	if len(s.inputs) >= MaxNodes {
		return Token{}, fmt.Errorf("input reference budget exceeded")
	}
	sz, err := measure(New(node))
	if err != nil {
		return Token{}, err
	}
	stored := s.stored
	cost := storageSize(node, true)
	stored.nodes += cost.nodes
	stored.text += cost.text
	if stored.nodes > 2*MaxNodes || stored.text > MaxTextBytes {
		return Token{}, fmt.Errorf("stored input budget exceeded")
	}
	node.input = &inputRef{scope: s, id: uint32(len(s.inputs) + 1)}
	s.inputs, s.sizes = append(s.inputs, node), append(s.sizes, sz)
	s.stored = stored
	return node, nil
}

// Postorder trees store each source node and one reference per child. Fragment
// views instead store ordinary inline syntax. Bound that physical storage as
// well as each root's expanded size.
func storageSize(node Token, original bool) size {
	result := size{nodes: 1}
	if node.input != nil && !original {
		return result
	}
	result.text = len(node.text)
	for _, child := range node.children.tokens {
		cost := storageSize(child, false)
		result.nodes += cost.nodes
		result.text += cost.text
	}
	return result
}

func (s *Scope) input(ref *inputRef) (Token, error) {
	if ref.scope != s || ref.id == 0 || ref.id > uint32(len(s.inputs)) {
		return Token{}, fmt.Errorf("input reference belongs to another invocation or is unknown")
	}
	return s.inputs[ref.id-1], nil
}

func (s *Scope) locations(node Token) error {
	for _, span := range []Span{node.span, node.open, node.close} {
		if _, err := s.EncodeSpan(span); err != nil {
			return err
		}
	}
	if node.kind != GroupKind && (node.open != (Span{}) || node.close != (Span{})) {
		return fmt.Errorf("delimiter spans require a group")
	}
	return nil
}

func (s *Scope) Snapshot() (ScopeData, error) {
	if err := s.check(); err != nil {
		return ScopeData{}, err
	}
	data := ScopeData{ID: s.id, Contexts: s.contexts, Spans: append([]Range{}, s.spans...), Inputs: []WireToken{}}
	for _, node := range s.inputs {
		wire, err := s.encodeToken(node, node.context, true)
		if err != nil {
			return ScopeData{}, err
		}
		data.Inputs = append(data.Inputs, wire)
	}
	return data, nil
}

func RestoreScope(data ScopeData) (*Scope, error) {
	if err := checkStoredWire(data.Inputs); err != nil {
		return nil, err
	}
	s := NewScope(data.ID)
	s.contexts = data.Contexts
	s.spans = append([]Range(nil), data.Spans...)
	if err := s.check(); err != nil {
		return nil, err
	}
	if len(data.Inputs) > MaxNodes {
		return nil, fmt.Errorf("input reference budget exceeded")
	}
	for _, r := range s.spans {
		if r.End < r.Start {
			return nil, fmt.Errorf("invalid issued span")
		}
	}
	for _, wire := range data.Inputs {
		if wire.Ref != 0 {
			return nil, fmt.Errorf("input entry must describe an original node")
		}
		// Definitions share earlier children. Check each root's expanded size;
		// summing their expansion would charge the same source tree repeatedly.
		var total size
		node, err := s.decodeToken(wire, true, 0, &total)
		if err != nil {
			return nil, err
		}
		if _, err = s.addInput(node); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Scope) Encode(stream TokenStream, defaultContext BindingContext) ([]WireToken, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if _, err := s.ContextID(defaultContext); err != nil {
		return nil, err
	}
	if _, err := measure(stream); err != nil {
		return nil, err
	}
	return s.encode(stream, defaultContext)
}

func (s *Scope) encode(stream TokenStream, defaultContext BindingContext) ([]WireToken, error) {
	result := make([]WireToken, 0, stream.Len())
	for _, node := range stream.tokens {
		wire, err := s.encodeToken(node, defaultContext, false)
		if err != nil {
			return nil, err
		}
		result = append(result, wire)
	}
	return result, nil
}

func (s *Scope) encodeToken(node Token, defaultContext BindingContext, original bool) (WireToken, error) {
	if err := s.locations(node); err != nil {
		return WireToken{}, err
	}
	span, _ := s.EncodeSpan(node.span)
	wire := WireToken{Span: &span}
	if node.kind == GroupKind {
		open, _ := s.EncodeSpan(node.open)
		close, _ := s.EncodeSpan(node.close)
		wire.Open, wire.Close = &open, &close
	}
	if node.input != nil && !original {
		if _, err := s.input(node.input); err != nil {
			return WireToken{}, err
		}
		wire.Ref = node.input.id
		return wire, nil
	}
	if node.kind == FragmentKind && !original {
		return WireToken{}, fmt.Errorf("new fragment requires a host-issued reference")
	}
	context := node.context
	if context.ref == 0 {
		context = defaultContext
	}
	id, err := s.ContextID(context)
	if err != nil {
		return WireToken{}, err
	}
	wire.Kind, wire.Text, wire.Raw, wire.Literal = string(node.kind), node.text, node.raw, string(node.literal)
	wire.Delimiter, wire.Implicit, wire.Context, wire.Fragment = string(node.delimiter), node.implicit, id, string(node.fragment)
	if node.kind == GroupKind || node.kind == FragmentKind {
		wire.Children, err = s.encode(node.children, defaultContext)
	}
	return wire, err
}

func (s *Scope) Decode(wire []WireToken) (TokenStream, error) {
	if err := s.check(); err != nil {
		return TokenStream{}, err
	}
	var total size
	return s.decode(wire, false, 0, &total)
}

func (s *Scope) decode(wire []WireToken, originals bool, depth int, total *size) (TokenStream, error) {
	if len(wire) == 0 {
		return TokenStream{}, nil
	}
	if depth > MaxDepth || len(wire) > MaxNodes {
		return TokenStream{}, fmt.Errorf("token structure budget exceeded")
	}
	result := make([]Token, 0, len(wire))
	for _, w := range wire {
		node, err := s.decodeToken(w, originals, depth, total)
		if err != nil {
			return TokenStream{}, err
		}
		result = append(result, node)
	}
	return New(result...), nil
}

func (s *Scope) decodeToken(wire WireToken, original bool, depth int, total *size) (Token, error) {
	if depth > MaxDepth {
		return Token{}, fmt.Errorf("token nesting budget exceeded")
	}
	var node Token
	var err error
	if wire.Ref != 0 {
		if wire.Kind != "" || wire.Text != "" || wire.Raw || wire.Literal != "" || wire.Delimiter != "" ||
			wire.Implicit || wire.Context != 0 || wire.Children != nil || wire.Fragment != "" {
			return Token{}, fmt.Errorf("input reference cannot replace contents or binding")
		}
		if wire.Ref > uint32(len(s.inputs)) {
			return Token{}, fmt.Errorf("unknown input reference %d", wire.Ref)
		}
		node = s.inputs[wire.Ref-1]
		sz := s.sizes[wire.Ref-1]
		if depth+sz.depth > MaxDepth {
			return Token{}, fmt.Errorf("referenced token nesting budget exceeded")
		}
		if err = total.add(sz); err != nil {
			return Token{}, err
		}
	} else {
		node = Token{kind: Kind(wire.Kind), text: wire.Text, raw: wire.Raw, literal: LiteralType(wire.Literal),
			delimiter: Delimiter(wire.Delimiter), implicit: wire.Implicit, fragment: FragmentType(wire.Fragment)}
		if err = lexical(node, original); err != nil {
			return Token{}, err
		}
		if node.kind == GroupKind && depth+1 > MaxDepth {
			return Token{}, fmt.Errorf("token nesting budget exceeded")
		}
		node.context, err = s.Context(wire.Context)
		if err != nil {
			return Token{}, err
		}
		if node.kind != GroupKind && node.kind != FragmentKind && wire.Children != nil {
			return Token{}, fmt.Errorf("only groups and original fragments have children")
		}
		if err = total.add(size{nodes: 1, tokens: lexicalSize(node), text: len(node.text)}); err != nil {
			return Token{}, err
		}
		node.children, err = s.decode(wire.Children, false, depth+1, total)
		if err != nil {
			return Token{}, err
		}
		if node.kind == FragmentKind {
			if err := checkFragmentView(wire.Children); err != nil {
				return Token{}, err
			}
		}
	}
	if wire.Span != nil {
		node.span, err = s.DecodeSpan(*wire.Span)
		if err != nil {
			return Token{}, err
		}
	}
	if wire.Open != nil || wire.Close != nil {
		if node.kind != GroupKind {
			return Token{}, fmt.Errorf("delimiter spans require a group")
		}
		if wire.Open != nil {
			node.open, err = s.DecodeSpan(*wire.Open)
			if err != nil {
				return Token{}, err
			}
		}
		if wire.Close != nil {
			node.close, err = s.DecodeSpan(*wire.Close)
			if err != nil {
				return Token{}, err
			}
		}
	}
	return node, nil
}

func checkFragmentView(wire []WireToken) error {
	for _, node := range wire {
		if node.Ref != 0 || node.Kind == string(FragmentKind) {
			return fmt.Errorf("fragment view must contain new syntax, not input references")
		}
		if err := checkFragmentView(node.Children); err != nil {
			return err
		}
	}
	return nil
}

func checkStoredWire(wire []WireToken) error {
	var nodes, text int
	var visit func([]WireToken, int) error
	visit = func(stream []WireToken, depth int) error {
		if depth > MaxDepth && len(stream) != 0 {
			return fmt.Errorf("stored input nesting budget exceeded")
		}
		for _, node := range stream {
			nodes++
			text += len(node.Text)
			if nodes > 2*MaxNodes || text > MaxTextBytes {
				return fmt.Errorf("stored input budget exceeded")
			}
			if err := visit(node.Children, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(wire, 0)
}

func validFragment(kind FragmentType) bool {
	switch kind {
	case ExprFragment, TypeFragment, StmtFragment, ItemFragment, IdentFragment, PathFragment, TTFragment:
		return true
	}
	return false
}
