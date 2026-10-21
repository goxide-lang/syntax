// Package syn reads single Goxide type, struct and enum declarations from public
// token trees. It provides immutable syntax views, not resolved names or types.
//
// Parse checks declaration structure and the type syntax needed to find field
// boundaries. Expressions, attribute arguments and function/interface bodies
// remain token views; Parse is not a replacement for the compiler's parser or
// semantic checks. Grouped type declarations and other items are not supported.
//
// ToTokens returns original tokens, including their binding contexts, spans,
// implicit semicolons and fragment references. It never prints and reparses
// text. A fragment may be inspected through its public view, but only replaying
// the original fragment retains its identity. Descendants of that view do not
// acquire the enclosing fragment's identity. Whitespace and comments discarded
// by the token package cannot be recovered.
package syn

import (
	"fmt"

	"github.com/goxide-lang/syntax/token"
)

// ItemKind records the declaration's written keyword.
type ItemKind string

const (
	TypeItem   ItemKind = "type"
	StructItem ItemKind = "struct"
	EnumItem   ItemKind = "enum"
)

// VariantKind distinguishes the three syntactic enum payload forms.
type VariantKind string

const (
	UnitVariant   VariantKind = "unit"
	TupleVariant  VariantKind = "tuple"
	StructVariant VariantKind = "struct"
)

type node struct{ tokens token.TokenStream }

// ToTokens replays this node's original tokens. Item includes surrounding
// semicolons; fields and variants exclude their following separators.
func (n node) ToTokens() token.TokenStream { return n.tokens }

// Span is a diagnostic anchor at the node's first token, not a range synthesized
// across tokens that may come from different source files or macro expansions.
func (n node) Span() token.Span {
	tokens := n.tokens.Tokens()
	if len(tokens) == 0 {
		return token.Span{}
	}
	return tokens[0].Span()
}

// Ident preserves the original identifier token or identifier fragment.
type Ident struct {
	node
	view token.Token
}

func (i Ident) Text() string                  { return i.view.Text() }
func (i Ident) Raw() bool                     { return i.view.Raw() }
func (i Ident) Context() token.BindingContext { return i.view.Context() }

// Attribute retains the path and the entire uninterpreted argument suffix.
// Arguments includes any delimiter group or equals sign written after the path.
type Attribute struct {
	node
	path      token.TokenStream
	arguments token.TokenStream
}

func (a Attribute) Path() token.TokenStream      { return a.path }
func (a Attribute) Arguments() token.TokenStream { return a.arguments }

// Type is a syntactic token view. It carries no canonical identity, alias
// expansion, inferred type, impl selection or compiler state.
type Type struct {
	node
	bare       *Ident
	structType bool
	fields     []Field
}

// StructFields reports fields only for a directly written struct type. It does
// not look through parentheses, aliases, paths or opaque type fragments.
func (t Type) StructFields() ([]Field, bool) {
	return append([]Field(nil), t.fields...), t.structType
}

// Field represents a struct field or enum payload entry. Names is empty for an
// embedded struct field or positional enum payload. A blank name remains "_".
type Field struct {
	node
	attributes []Attribute
	visibility token.TokenStream
	names      []Ident
	typ        Type
	tag        token.Token
	hasTag     bool
}

func (f Field) Attributes() []Attribute       { return append([]Attribute(nil), f.attributes...) }
func (f Field) Visibility() token.TokenStream { return f.visibility }
func (f Field) Names() []Ident                { return append([]Ident(nil), f.names...) }
func (f Field) Type() Type                    { return f.typ }
func (f Field) Tag() (token.Token, bool)      { return f.tag, f.hasTag }

type Variant struct {
	node
	attributes []Attribute
	name       Ident
	kind       VariantKind
	fields     []Field
}

func (v Variant) Attributes() []Attribute { return append([]Attribute(nil), v.attributes...) }
func (v Variant) Name() Ident             { return v.name }
func (v Variant) Kind() VariantKind       { return v.kind }
func (v Variant) Fields() []Field         { return append([]Field(nil), v.fields...) }

// Item is a read-only declaration view. Generics includes its bracket group;
// Where includes the where keyword. Visibility reports only an explicit pub,
// without interpreting the export convention of a Go-style type declaration.
type Item struct {
	node
	kind       ItemKind
	attributes []Attribute
	visibility token.TokenStream
	name       Ident
	generics   token.TokenStream
	where      token.TokenStream
	alias      bool
	typ        Type
	variants   []Variant
}

func (i Item) Kind() ItemKind                { return i.kind }
func (i Item) Attributes() []Attribute       { return append([]Attribute(nil), i.attributes...) }
func (i Item) Visibility() token.TokenStream { return i.visibility }
func (i Item) Name() Ident                   { return i.name }
func (i Item) Generics() token.TokenStream   { return i.generics }
func (i Item) Where() token.TokenStream      { return i.where }
func (i Item) Alias() bool                   { return i.alias }

// Type returns the written underlying type. For struct sugar it is the brace
// body alone; the struct keyword belongs to the declaration's header. For an
// enum it is empty, because the declaration has variants instead.
func (i Item) Type() Type          { return i.typ }
func (i Item) Variants() []Variant { return append([]Variant(nil), i.variants...) }

// ParseError identifies the offending token, or the closing delimiter when a
// required token is absent. It does not invent a new binding or source span.
type ParseError struct {
	Message string
	At      token.Span
}

func (e *ParseError) Error() string {
	start, _ := e.At.Range()
	return fmt.Sprintf("%s at byte %d", e.Message, start)
}
func (e *ParseError) Span() token.Span { return e.At }

// Parse reads exactly one type, struct or enum declaration. Leading and trailing
// semicolons are accepted and retained by Item.ToTokens. All views are immutable;
// callers construct changed syntax explicitly with the token package.
func Parse(input token.TokenStream) (Item, error) {
	if err := input.Validate(); err != nil {
		return Item{}, &ParseError{Message: err.Error(), At: (node{input}).Span()}
	}
	return parseItem(input, 0)
}
