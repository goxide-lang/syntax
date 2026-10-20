package proc_macro

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/goxide-lang/syntax/token"
)

// Kind identifies one of the three statically registered macro entry forms.
type Kind string

const (
	Function  Kind = "function"
	Attribute Kind = "attribute"
	Derive    Kind = "derive"
)

// Position is the output grammar slot. The compiler, not this SDK, parses and
// checks the emitted syntax for the selected slot.
type Position string

const (
	ExpressionPosition   Position = "expr"
	TypePosition         Position = "type"
	StatementPosition    Position = "stmt"
	StatementsPosition   Position = "stmts"
	ItemPosition         Position = "item"
	ItemsPosition        Position = "items"
	TraitMembersPosition Position = "trait_members"
	ImplMembersPosition  Position = "impl_members"
)

// Target is a frozen description of the program being expanded, not of the
// machine or WASI environment executing the macro.
type Target struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// Context contains only the capabilities supplied in this request. It has no
// environment, filesystem, network, type-query, or process access methods.
type Context struct {
	defSite, callSite token.BindingContext
	defSpan, callSpan token.Span
	target            Target
	features          []string
	roots             map[string]token.TokenStream
}

func (c Context) DefSite() token.BindingContext  { return c.defSite }
func (c Context) CallSite() token.BindingContext { return c.callSite }
func (c Context) DefSpan() token.Span            { return c.defSpan }
func (c Context) CallSpan() token.Span           { return c.callSpan }
func (c Context) Target() Target                 { return c.target }
func (c Context) Features() []string             { return append([]string(nil), c.features...) }

func (c Context) HasFeature(name string) bool {
	i := sort.SearchStrings(c.features, name)
	return i < len(c.features) && c.features[i] == name
}

// RuntimeRoot looks up a host-supplied logical module identity. It neither
// resolves aliases nor discovers dependencies; an absent root is an error.
func (c Context) RuntimeRoot(logicalModulePath string) (token.TokenStream, error) {
	root, ok := c.roots[logicalModulePath]
	if !ok {
		return token.TokenStream{}, &ContextError{
			message: fmt.Sprintf("runtime root %q is not available", logicalModulePath),
			span:    c.callSpan,
		}
	}
	return root, nil
}

// ContextError reports an unavailable capability at the macro call position.
type ContextError struct {
	message string
	span    token.Span
}

func (e *ContextError) Error() string    { return e.message }
func (e *ContextError) Span() token.Span { return e.span }

// RequestConfig supplies a host-authored invocation. NewRequest captures input
// tokens using CallSite as the default context, then freezes the reference table.
type RequestConfig struct {
	Entry        string
	Kind         Kind
	Position     Position
	DefSite      token.BindingContext
	CallSite     token.BindingContext
	DefSpan      token.Span
	CallSpan     token.Span
	Target       Target
	Features     []string
	RuntimeRoots map[string]token.TokenStream
	Input        token.TokenStream
	Args         token.TokenStream
	Item         token.TokenStream
}

// Request is an immutable invocation and its private reference-table snapshot.
// The snapshot is used when admitting the corresponding response.
type Request struct {
	wire    wireRequest
	scope   *token.Scope
	context Context
	input   token.TokenStream
	args    token.TokenStream
	item    token.TokenStream
}

func (r Request) ID() string               { return r.wire.Scope.ID }
func (r Request) Entry() string            { return r.wire.Entry }
func (r Request) Kind() Kind               { return r.wire.Kind }
func (r Request) Position() Position       { return r.wire.Position }
func (r Request) Context() Context         { return r.context }
func (r Request) Input() token.TokenStream { return r.input }
func (r Request) Args() token.TokenStream  { return r.args }
func (r Request) Item() token.TokenStream  { return r.item }

func validName(s string) bool {
	return s != "" && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func validateHeader(entry string, kind Kind, position Position, target Target) error {
	if !validName(entry) {
		return fmt.Errorf("invalid macro entry name")
	}
	switch kind {
	case Function, Attribute, Derive:
	default:
		return fmt.Errorf("unknown macro kind %q", kind)
	}
	switch position {
	case ExpressionPosition, TypePosition, StatementPosition, StatementsPosition,
		ItemPosition, ItemsPosition, TraitMembersPosition, ImplMembersPosition:
	default:
		return fmt.Errorf("unknown output position %q", position)
	}
	if !validName(target.OS) || !validName(target.Arch) {
		return fmt.Errorf("target OS and architecture must be nonempty UTF-8 strings")
	}
	return nil
}
