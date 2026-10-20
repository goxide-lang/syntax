package token_test

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/syntax/token"
)

func scopeParse(t *testing.T, text string, context token.BindingContext) token.TokenStream {
	t.Helper()
	stream, err := token.ParseText(text, context)
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func mustCapture(t *testing.T, s *token.Scope, stream token.TokenStream, context token.BindingContext) token.TokenStream {
	t.Helper()
	captured, err := s.Capture(stream, context)
	if err != nil {
		t.Fatal(err)
	}
	return captured
}

func TestScopeRoundTripPreservesBindingAndIdentity(t *testing.T) {
	host := token.NewScope("invocation-1")
	def, call := host.NewContext(), host.NewContext()
	source := mustCapture(t, host, scopeParse(t, "(r#match + 1);", call), call)
	snapshot, err := host.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	guest, err := token.RestoreScope(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := host.Encode(source, def)
	if err != nil {
		t.Fatal(err)
	}
	received, err := guest.Decode(wire)
	if err != nil {
		t.Fatal(err)
	}
	guestDef, _ := guest.Context(def.Reference())
	newName, _ := token.NewIdent("answer", false, token.BindingContext{})
	output := received.Append(newName)
	wireOutput, err := guest.Encode(output, guestDef)
	if err != nil {
		t.Fatal(err)
	}
	result, err := host.Decode(wireOutput)
	if err != nil {
		t.Fatal(err)
	}
	nodes := result.Tokens()
	if nodes[0].Reference() != source.Tokens()[0].Reference() || nodes[0].Context() != call {
		t.Fatal("original input lost identity or binding")
	}
	if nodes[0].Children().Tokens()[0].Raw() != true {
		t.Fatal("raw identifier was lost")
	}
	if nodes[2].Reference() != 0 || nodes[2].Context() != def {
		t.Fatal("new zero-context syntax did not bind at definition site")
	}
	again, err := guest.Snapshot()
	if err != nil || !reflect.DeepEqual(snapshot, again) {
		t.Fatalf("snapshot round trip changed: %v", err)
	}
}

func TestSpanDoesNotChangeBindingOrProvenance(t *testing.T) {
	s := token.NewScope("locations")
	def, call := s.NewContext(), s.NewContext()
	issued, _ := s.NewSpan(100, 120)
	original := mustCapture(t, s, scopeParse(t, "name;", call), call).Tokens()[0]
	moved := original.WithSpan(issued)
	wire, err := s.Encode(token.New(moved), def)
	if err != nil {
		t.Fatal(err)
	}
	if wire[0].Ref != original.Reference() || wire[0].Context != 0 || wire[0].Text != "" {
		t.Fatal("original reference carried replacement data")
	}
	decoded, err := s.Decode(wire)
	if err != nil || decoded.Tokens()[0].Context() != call || decoded.Tokens()[0].Span() != issued {
		t.Fatalf("diagnostic override affected binding: %v", err)
	}
	newName, _ := token.NewIdent("name", false, def)
	if newName.WithSpan(issued).Reference() != 0 {
		t.Fatal("span granted provenance to new syntax")
	}
	for _, bad := range []token.WireSpan{
		{Start: 10, End: 9}, {Ref: issued.Reference(), Start: 99, End: 110},
		{Ref: issued.Reference(), Start: 100, End: 121}, {Ref: math.MaxUint32},
	} {
		if _, err := s.DecodeSpan(bad); err == nil {
			t.Fatalf("accepted invalid span %+v", bad)
		}
	}
	subspan, err := s.DecodeSpan(token.WireSpan{Ref: issued.Reference(), Start: 105, End: 110})
	if err != nil || subspan.Reference() != issued.Reference() {
		t.Fatal("valid issued subspan rejected")
	}
}

func TestFragmentViewCannotTransferParsedIdentity(t *testing.T) {
	s := token.NewScope("fragments")
	context := s.NewContext()
	captured := mustCapture(t, s, scopeParse(t, "(value);", context), context)
	fragment, err := s.CaptureFragment(token.ExprFragment, captured, context)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, _ := s.CaptureFragment(token.TTFragment, token.New(fragment), context)
	var inspect func(token.TokenStream)
	inspect = func(stream token.TokenStream) {
		for _, node := range stream.Tokens() {
			if node.Reference() != 0 || node.Kind() == token.FragmentKind || node.Context() != context {
				t.Fatalf("view leaked parsed identity or lost binding: %+v", node)
			}
			inspect(node.Children())
		}
	}
	inspect(fragment.Children())
	inspect(wrapped.Children())
	wire, err := s.Encode(token.New(fragment), context)
	if err != nil || wire[0].Ref != fragment.Reference() {
		t.Fatal("opaque fragment could not be forwarded")
	}
	viewWire, err := s.Encode(fragment.Children(), context)
	if err != nil || viewWire[0].Ref != 0 || viewWire[0].Kind != string(token.GroupKind) {
		t.Fatal("view did not encode as new syntax")
	}
	snapshot, _ := s.Snapshot()
	restored, err := token.RestoreScope(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	forwarded, err := restored.Decode(wire)
	if err != nil || forwarded.Tokens()[0].FragmentType() != token.ExprFragment {
		t.Fatal("fragment category lost during transport")
	}
	if _, err := s.Decode([]token.WireToken{{Kind: "fragment", Fragment: "type", Context: 1}}); err == nil {
		t.Fatal("response forged fragment category")
	}
	bad := snapshot
	bad.Inputs = append([]token.WireToken(nil), snapshot.Inputs...)
	bad.Inputs[fragment.Reference()-1].Children = []token.WireToken{{Ref: captured.Tokens()[0].Reference()}}
	if _, err := token.RestoreScope(bad); err == nil {
		t.Fatal("fragment view accepted original child references")
	}
}

func TestScopeRejectsForeignReferencesAndRollsBack(t *testing.T) {
	s, foreign := token.NewScope("local"), token.NewScope("foreign")
	context, other := s.NewContext(), foreign.NewContext()
	original := mustCapture(t, foreign, scopeParse(t, "foreign;", other), other)
	if _, err := s.Encode(original, context); err == nil {
		t.Fatal("accepted foreign original")
	}
	localName, _ := token.NewIdent("local", false, context)
	combined := token.New(localName).Append(original.Tokens()...)
	if _, err := s.Capture(combined, context); err == nil {
		t.Fatal("accepted foreign capture")
	}
	snapshot, _ := s.Snapshot()
	if len(snapshot.Inputs) != 0 {
		t.Fatal("failed capture left partial reference entries")
	}
	got := mustCapture(t, s, token.New(localName), context)
	if got.Tokens()[0].Reference() != 1 {
		t.Fatal("failed capture consumed input IDs")
	}
	foreignSpan, _ := foreign.NewSpan(0, 1)
	if _, err := s.Encode(token.New(localName.WithSpan(foreignSpan)), context); err == nil {
		t.Fatal("accepted foreign diagnostic span")
	}
}

func TestScopeRejectsMalformedOutput(t *testing.T) {
	s := token.NewScope("invalid")
	context := s.NewContext()
	original := mustCapture(t, s, scopeParse(t, "name;", context), context).Tokens()[0]
	cases := []token.WireToken{
		{Ref: math.MaxUint32},
		{Ref: original.Reference(), Text: "other"},
		{Ref: original.Reference(), Context: 1},
		{Ref: original.Reference(), Children: []token.WireToken{}},
		{Kind: "ident", Text: "x", Context: math.MaxUint32},
		{Kind: "ident", Text: "match", Context: 1},
		{Kind: "ident", Text: "x", Context: 1, Implicit: true},
		{Kind: "ident", Text: "x", Context: 1, Open: &token.WireSpan{}},
		{Kind: "punct", Text: ")", Context: 1},
		{Kind: "literal", Text: "1 2", Literal: "int", Context: 1},
		{Kind: "group", Delimiter: "unknown", Context: 1},
		{Kind: "group", Delimiter: "brace", Context: 0},
		{Kind: "eof", Context: 1},
	}
	for _, invalid := range cases {
		if _, err := s.Decode([]token.WireToken{invalid}); err == nil {
			t.Errorf("accepted malformed output %+v", invalid)
		}
	}
}

func TestScopeReferenceBudgetsCountExpandedOutput(t *testing.T) {
	s := token.NewScope("amplification")
	context := s.NewContext()
	name, _ := token.NewIdent("x", false, context)
	children := make([]token.Token, 500)
	for i := range children {
		children[i] = name
	}
	group, _ := token.NewGroup(token.Brace, token.New(children...), context)
	captured := mustCapture(t, s, token.New(group), context).Tokens()[0]
	wire := make([]token.WireToken, 201)
	for i := range wire {
		wire[i].Ref = captured.Reference()
	}
	if _, err := s.Decode(wire); err == nil {
		t.Fatal("compact references amplified beyond expanded token budget")
	}
}

func TestScopeDepthAndLargePostorderBoundary(t *testing.T) {
	s := token.NewScope("depth")
	context := s.NewContext()
	empty := token.New()
	for range token.MaxDepth {
		group, _ := token.NewGroup(token.Brace, empty, context)
		empty = token.New(group)
	}
	captured := mustCapture(t, s, empty, context)
	snapshot, _ := s.Snapshot()
	if _, err := token.RestoreScope(snapshot); err != nil {
		t.Fatalf("maximum legal depth did not round trip: %v", err)
	}
	group, _ := token.NewGroup(token.Brace, captured, context)
	if _, err := s.Encode(token.New(group), context); err == nil {
		t.Fatal("reference nesting escaped depth budget")
	}
	leaf, _ := token.NewIdent("leaf", false, context)
	withLeaf := token.New(leaf)
	for range token.MaxDepth {
		group, _ := token.NewGroup(token.Brace, withLeaf, context)
		withLeaf = token.New(group)
	}
	deepLeaf := mustCapture(t, s, withLeaf, context)
	leafSnapshot, _ := s.Snapshot()
	restored, err := token.RestoreScope(leafSnapshot)
	if err != nil {
		t.Fatalf("leaf at maximum depth did not round trip: %v", err)
	}
	leafWire, _ := s.Encode(deepLeaf, context)
	if _, err := restored.Decode(leafWire); err != nil {
		t.Fatalf("leaf at maximum depth did not decode: %v", err)
	}
	large := token.NewScope("large")
	largeContext := large.NewContext()
	name, _ := token.NewIdent("x", false, largeContext)
	children := make([]token.Token, 50_000)
	for i := range children {
		children[i] = name
	}
	root, _ := token.NewGroup(token.Brace, token.New(children...), largeContext)
	mustCapture(t, large, token.New(root), largeContext)
	data, err := large.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := token.RestoreScope(data); err != nil {
		t.Fatalf("postorder references were charged twice: %v", err)
	}
}

func TestStoredInputBudgetIncludesInlineFragmentViews(t *testing.T) {
	longName := strings.Repeat("x", token.MaxTextBytes/2+1)
	view := []token.WireToken{{Kind: "ident", Text: longName, Context: 1}}
	data := token.ScopeData{ID: "stored", Contexts: 1, Inputs: []token.WireToken{
		{Kind: "fragment", Fragment: "expr", Context: 1, Children: view},
		{Kind: "fragment", Fragment: "expr", Context: 1, Children: view},
	}}
	if _, err := token.RestoreScope(data); err == nil {
		t.Fatal("individually legal fragment views exceeded total stored text budget")
	}
}
