package syn_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/goxide-lang/syntax/syn"
	"github.com/goxide-lang/syntax/token"
)

func lex(t *testing.T, source string, context token.BindingContext) token.TokenStream {
	t.Helper()
	stream, err := token.ParseText(source, context)
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func parse(t *testing.T, source string) syn.Item {
	t.Helper()
	stream := lex(t, source, token.BindingContext{})
	item, err := syn.Parse(stream)
	if err != nil {
		t.Fatalf("Parse(%q): %v", source, err)
	}
	equalTokens(t, stream, item.ToTokens())
	return item
}

func equalTokens(t *testing.T, want, got token.TokenStream) {
	t.Helper()
	a, b := want.Tokens(), got.Tokens()
	if len(a) != len(b) {
		t.Fatalf("token count changed: %d to %d", len(a), len(b))
	}
	for i := range a {
		aw, ac := a[i].DelimiterSpans()
		bw, bc := b[i].DelimiterSpans()
		if a[i].Kind() != b[i].Kind() || a[i].Text() != b[i].Text() || a[i].Raw() != b[i].Raw() ||
			a[i].LiteralType() != b[i].LiteralType() || a[i].Implicit() != b[i].Implicit() ||
			a[i].Delimiter() != b[i].Delimiter() || a[i].FragmentType() != b[i].FragmentType() ||
			a[i].Reference() != b[i].Reference() || a[i].Context() != b[i].Context() ||
			a[i].Span() != b[i].Span() || aw != bw || ac != bc {
			t.Fatalf("token %d changed: %s to %s", i, token.New(a[i]).Display(), token.New(b[i]).Display())
		}
		equalTokens(t, a[i].Children(), b[i].Children())
	}
}

func TestStructFieldsAndSyntaxViews(t *testing.T) {
	item := parse(t, "#[derive(custom::Read, custom::Write)]\n"+
		"#[codec(name = \"record\")]\n"+
		"pub struct r#type[T any] where T: custom::Bound {\n"+
		"  #[field(note = \"pair\")] pub first, second []T `wire:\"pair\"`\n"+
		"  embedded::Name[T]\n"+
		"  *native::Thing\n"+
		"  count [2 + 3]int\n"+
		"  _ map[string]*T\n"+
		"  callback func(a T, rest ...T) (T, error)\n"+
		"  handle <-chan T\n"+
		"  owned &mut T\n"+
		"  nested struct { value T }\n"+
		"}")
	if item.Kind() != syn.StructItem || item.Name().Text() != "type" || !item.Name().Raw() || item.Alias() {
		t.Fatal("declaration header was not preserved")
	}
	if item.Visibility().Display() != "pub" || item.Generics().Display() != "[T any]" || item.Where().Display() != "where T : custom :: Bound" {
		t.Fatalf("unexpected header: %s %s %s", item.Visibility().Display(), item.Generics().Display(), item.Where().Display())
	}
	attrs := item.Attributes()
	if len(attrs) != 2 || attrs[1].Path().Display() != "codec" || attrs[1].Arguments().Display() != "(name = \"record\")" {
		t.Fatal("attribute arguments were interpreted or lost")
	}
	fields, ok := item.Type().StructFields()
	if !ok || len(fields) != 9 {
		t.Fatalf("fields: struct=%v count=%d", ok, len(fields))
	}
	if names := fields[0].Names(); len(names) != 2 || names[0].Text() != "first" || names[1].Text() != "second" {
		t.Fatal("multiple field names were not preserved")
	}
	if fields[0].Visibility().Display() != "pub" || len(fields[0].Attributes()) != 1 || fields[0].Type().ToTokens().Display() != "[] T" {
		t.Fatal("field syntax was not preserved")
	}
	if tag, ok := fields[0].Tag(); !ok || tag.Text() != "`wire:\"pair\"`" {
		t.Fatal("field tag was lost")
	}
	if len(fields[1].Names()) != 0 || len(fields[2].Names()) != 0 || fields[4].Names()[0].Text() != "_" {
		t.Fatal("embedded and blank fields were not distinguished")
	}
	if fields[3].Names()[0].Text() != "count" || fields[3].Type().ToTokens().Display() != "[2 + 3] int" {
		t.Fatal("named array field was mistaken for an embedded generic type")
	}
	if nested, ok := fields[8].Type().StructFields(); !ok || len(nested) != 1 || nested[0].Names()[0].Text() != "value" {
		t.Fatal("nested struct field boundaries were lost")
	}
	// Returned collections are independent snapshots, not mutable AST storage.
	attrs[0] = syn.Attribute{}
	fields[0] = syn.Field{}
	if item.Attributes()[0].Path().Display() != "derive" {
		t.Fatal("attribute slice mutated the parsed node")
	}
	again, _ := item.Type().StructFields()
	names := again[0].Names()
	names[0] = syn.Ident{}
	if again[0].Names()[0].Text() != "first" {
		t.Fatal("field-name slice mutated the parsed node")
	}
}

func TestEnumPayloadShapes(t *testing.T) {
	item := parse(t, `#[derive(Clone)]
pub enum Message[T any] where T: Clone {
    #[wire(label = "idle")] Idle,
    Value(#[payload] T, *other::Payload,),
    Named { #[field] left, right T, text string },
    Tuple(),
    Record{},
}`)
	variants := item.Variants()
	if item.Kind() != syn.EnumItem || len(variants) != 5 {
		t.Fatal("enum structure was lost")
	}
	for i, kind := range []syn.VariantKind{syn.UnitVariant, syn.TupleVariant, syn.StructVariant, syn.TupleVariant, syn.StructVariant} {
		if variants[i].Kind() != kind {
			t.Errorf("variant %d kind = %s, want %s", i, variants[i].Kind(), kind)
		}
	}
	if len(variants[0].Attributes()) != 1 || len(variants[1].Fields()) != 2 || len(variants[1].Fields()[0].Names()) != 0 {
		t.Fatal("unit or tuple payload attributes were lost")
	}
	if len(variants[1].Fields()[0].Attributes()) != 1 || variants[1].Fields()[1].Type().ToTokens().Display() != "* other :: Payload" {
		t.Fatal("tuple type view is incorrect")
	}
	fields := variants[2].Fields()
	if len(fields) != 2 || len(fields[0].Names()) != 2 || fields[1].Names()[0].Text() != "text" {
		t.Fatal("named variant fields are incorrect")
	}
	variants[0] = syn.Variant{}
	if item.Variants()[0].Name().Text() != "Idle" {
		t.Fatal("variant slice mutated the parsed node")
	}
}

func TestTypeFormsAndArrayParameterAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		source   string
		generic  bool
		alias    bool
		isStruct bool
	}{
		{"type Record struct { value string }", false, false, true},
		{"type Alias = map[string][]*other::Value", false, true, false},
		{"type Number int", false, false, false},
		{"type Function func() where", false, false, false},
		{"type Array [N]int", false, false, false},
		{"type Product [T * U]int", false, false, false},
		{"type Call [T(U)]int", false, false, false},
		{"type Wrapped[T any] struct { value T } where T: Bound", true, false, true},
		{"type Pair[T, U] map[T]U", true, false, false},
		{"type Bound[T ~int | ~string] struct {}", true, false, true},
		{"type Callback[T func()] struct {}", true, false, true},
		{"type Pointer[T *[N]U] struct {}", true, false, true},
		{"type Alias[T any] = other::Wrapped[T]", true, true, false},
		{"type Alias[T] = []T", true, true, false},
		{"type Alias[T *U] = T", true, true, false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			item := parse(t, tc.source)
			_, isStruct := item.Type().StructFields()
			if item.Kind() != syn.TypeItem || item.Alias() != tc.alias || !item.Generics().IsEmpty() != tc.generic || isStruct != tc.isStruct {
				t.Fatalf("kind=%s alias=%v generic=%v struct=%v", item.Kind(), item.Alias(), !item.Generics().IsEmpty(), isStruct)
			}
		})
	}
	if fields, ok := parse(t, "struct Empty {}").Type().StructFields(); !ok || len(fields) != 0 {
		t.Fatal("empty struct was not represented")
	}
	if variants := parse(t, "enum Empty {}").Variants(); len(variants) != 0 {
		t.Fatal("empty enum was not represented")
	}
}

func TestOpaqueTypeAndAttributeTokens(t *testing.T) {
	item := parse(t, `#[vendor::any = not_a_supported_meta_form!(a, b)]
type S struct {
    first interface { Read(p []byte) (int, error) }
    second make_type!(unknown + grammar)
    third other::Name[Output = []int]
    fourth (map[string]int)
}`)
	if item.Attributes()[0].Arguments().Display() != "= not_a_supported_meta_form ! (a , b)" {
		t.Fatal("custom attribute tokens were interpreted")
	}
	fields, _ := item.Type().StructFields()
	if len(fields) != 4 || fields[1].Type().ToTokens().Display() != "make_type ! (unknown + grammar)" {
		t.Fatal("opaque type syntax was lost")
	}
}

func TestLocatedErrors(t *testing.T) {
	for _, tc := range []struct {
		source  string
		at      string
		message string
	}{
		{"func f() {}", "func", "type, struct or enum"},
		{"type (A int)", "(A", "grouped type"},
		{"struct {}", "{", "identifier"},
		{"#![x] struct S {}", "!", "inner attributes"},
		{"#[x::] struct S {}", "]", "identifier"},
		{"pub(crate) struct S {}", "(crate)", "type, struct or enum"},
		{"struct S { a int, b int }", ",", "separator after field"},
		{"enum E { A; B }", ";", "comma between enum"},
		{"enum E { A(int string) }", "string", "comma between tuple"},
		{"enum E { A { pub x int } }", "pub", "identifier"},
		{"struct S[T,] where T Bound {}", "Bound", "expected :"},
		{"struct S where T: Bound {}", "where", "requires generic"},
		{"struct S { f map[]int }", "]", "expected type"},
		{"type Alias =", "=", "expected type"},
		{"struct A {}; struct B {}", "struct B", "single declaration"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			_, err := syn.Parse(lex(t, tc.source, token.BindingContext{}))
			var diagnostic *syn.ParseError
			if !errors.As(err, &diagnostic) || !strings.Contains(diagnostic.Message, tc.message) {
				t.Fatalf("error = %v, want %q", err, tc.message)
			}
			start, _ := diagnostic.Span().Range()
			if want := strings.Index(tc.source, tc.at); start != uint32(want) {
				t.Fatalf("diagnostic offset = %d, want %d (%q): %v", start, want, tc.at, err)
			}
		})
	}
	_, err := syn.Parse(lex(t, "type Deep "+strings.Repeat("*", token.MaxDepth+2)+"int", token.BindingContext{}))
	if err == nil || !strings.Contains(err.Error(), "nesting budget") {
		t.Fatalf("unbounded pointer nesting: %v", err)
	}
}

func capture(t *testing.T, scope *token.Scope, input token.TokenStream, context token.BindingContext) token.TokenStream {
	t.Helper()
	stream, err := scope.Capture(input, context)
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func fragment(t *testing.T, scope *token.Scope, kind token.FragmentType, input token.TokenStream, context token.BindingContext) token.Token {
	t.Helper()
	result, err := scope.CaptureFragment(kind, input, context)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func withoutSemis(stream token.TokenStream) token.TokenStream {
	var tokens []token.Token
	for _, t := range stream.Tokens() {
		if t.Kind() != token.SemicolonKind {
			tokens = append(tokens, t)
		}
	}
	return token.New(tokens...)
}

func TestReferenceAndMixedContextPreservation(t *testing.T) {
	scope := token.NewScope("syn-provenance")
	def, call := scope.NewContext(), scope.NewContext()
	typeFragment := fragment(t, scope, token.TypeFragment, withoutSemis(lex(t, "other::T", def)), def)
	nameFragment := fragment(t, scope, token.IdentFragment, withoutSemis(lex(t, "r#type", call)), call)
	fieldName := fragment(t, scope, token.IdentFragment, withoutSemis(lex(t, "value", call)), call)
	fieldBody := token.New(fieldName, typeFragment, token.NewSemicolon(call))
	fieldBody = fieldBody.Append(withoutSemis(lex(t, "second", call)).Tokens()...)
	fieldBody = fieldBody.Append(typeFragment)
	body, err := token.NewGroup(token.Brace, fieldBody, def)
	if err != nil {
		t.Fatal(err)
	}
	open, _ := scope.NewSpan(20, 21)
	close, _ := scope.NewSpan(70, 71)
	body = body.WithDelimiterSpans(open, close)
	input := withoutSemis(lex(t, "pub struct", def)).Append(nameFragment, body)
	input = capture(t, scope, input, call)
	item, err := syn.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	equalTokens(t, input, item.ToTokens())
	if item.Name().Text() != "type" || !item.Name().Raw() || item.Name().Context() != call || item.Name().ToTokens().Tokens()[0].Reference() != nameFragment.Reference() {
		t.Fatal("identifier fragment was flattened or rebound")
	}
	fields, _ := item.Type().StructFields()
	if fields[0].Names()[0].Context() != call || fields[0].Type().ToTokens().Tokens()[0].Context() != def {
		t.Fatal("field name and type lost their independent bindings")
	}
	for _, f := range fields {
		if got := f.Type().ToTokens().Tokens()[0]; got.Reference() != typeFragment.Reference() || got.FragmentType() != token.TypeFragment {
			t.Fatal("reused type fragment lost identity")
		}
	}
	wantWire, err := scope.Encode(input, def)
	if err != nil {
		t.Fatal(err)
	}
	gotWire, err := scope.Encode(item.ToTokens(), def)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(wantWire)
	gotJSON, _ := json.Marshal(gotWire)
	if !bytes.Equal(wantJSON, gotJSON) {
		t.Fatal("replayed output does not preserve the host reference encoding")
	}
}

func TestItemFragmentViewAndDiagnosticIdentity(t *testing.T) {
	scope := token.NewScope("syn-item-fragment")
	context := scope.NewContext()
	source := capture(t, scope, lex(t, "type Record struct { value int }", context), context)
	itemFragment := fragment(t, scope, token.ItemFragment, source, context)
	item, err := syn.Parse(token.New(itemFragment))
	if err != nil {
		t.Fatal(err)
	}
	if item.Name().Text() != "Record" || item.ToTokens().Tokens()[0].Reference() != itemFragment.Reference() {
		t.Fatal("item fragment could not be inspected and replayed")
	}
	if item.Name().ToTokens().Tokens()[0].Reference() != 0 || item.Name().Context() != context {
		t.Fatal("fragment view granted child identity or lost binding")
	}
	bad := fragment(t, scope, token.ExprFragment, withoutSemis(lex(t, "value", context)), context)
	issued, _ := scope.NewSpan(200, 210)
	bad = bad.WithSpan(issued)
	_, err = syn.Parse(token.New(bad))
	var diagnostic *syn.ParseError
	if !errors.As(err, &diagnostic) || diagnostic.Span() != issued {
		t.Fatalf("fragment diagnostic lost the original span: %v", err)
	}
	two := fragment(t, scope, token.ItemFragment, lex(t, "struct A {}; struct B {}", context), context)
	if _, err := syn.Parse(token.New(two)); err == nil {
		t.Fatal("accepted two declarations inside an item fragment")
	}
}

func TestAttributePathFragmentAndClosingDelimiterSpan(t *testing.T) {
	scope := token.NewScope("syn-path-fragment")
	context := scope.NewContext()
	path := fragment(t, scope, token.PathFragment, withoutSemis(lex(t, "vendor::custom", context)), context)
	arguments, _ := token.NewGroup(token.Parenthesis, withoutSemis(lex(t, "raw = 1", context)), context)
	attribute, _ := token.NewGroup(token.Bracket, token.New(path, arguments), context)
	hash, _ := token.NewPunct("#", context)
	input := token.New(hash, attribute).Append(lex(t, "struct S {}", context).Tokens()...)
	item, err := syn.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if item.Attributes()[0].Path().Tokens()[0].Reference() != path.Reference() {
		t.Fatal("attribute path fragment was flattened")
	}
	equalTokens(t, input, item.ToTokens())
	open, _ := scope.NewSpan(30, 31)
	close, _ := scope.NewSpan(90, 91)
	body, _ := token.NewGroup(token.Brace, withoutSemis(lex(t, "first,", context)), context)
	body = body.WithDelimiterSpans(open, close)
	_, err = syn.Parse(withoutSemis(lex(t, "struct S", context)).Append(body))
	var diagnostic *syn.ParseError
	if !errors.As(err, &diagnostic) || diagnostic.Span() != close {
		t.Fatalf("missing field name did not point to the original closing delimiter: %v", err)
	}
}

func TestImplicitSemicolonsAndRawNamesAreRetained(t *testing.T) {
	input := lex(t, ";\n#[r#default]\npub struct 世界 {\n r#type int\n}\n;", token.BindingContext{})
	item, err := syn.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	equalTokens(t, input, item.ToTokens())
	fields, _ := item.Type().StructFields()
	if item.Name().Text() != "世界" || !fields[0].Names()[0].Raw() {
		t.Fatal("Unicode or raw name changed")
	}
}

func ExampleParse() {
	input, _ := token.ParseText("pub struct Pair { left, right int }", token.BindingContext{})
	item, _ := syn.Parse(input)
	fields, _ := item.Type().StructFields()
	fmt.Println(item.Kind(), item.Name().Text())
	for _, field := range fields {
		for _, name := range field.Names() {
			fmt.Println(name.Text(), field.Type().ToTokens().Display())
		}
	}
	// Output:
	// struct Pair
	// left int
	// right int
}

func FuzzParse(f *testing.F) {
	for _, source := range []string{
		"type Record struct { a, b []int; native::Embedded }",
		"#[custom(whatever)] struct S[T any] where T: Bound { x T }",
		"enum E { A, B(int), C { x, y string }, }",
		"type Array[T * U] int",
		"type Alias[T any] = *other::T[T]",
		"struct S { f map[]int }",
	} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 64<<10 {
			t.Skip()
		}
		input, err := token.ParseText(source, token.BindingContext{})
		if err != nil {
			return
		}
		if item, err := syn.Parse(input); err == nil {
			equalTokens(t, input, item.ToTokens())
		}
	})
}
