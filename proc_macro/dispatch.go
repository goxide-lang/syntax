package proc_macro

import (
	"fmt"

	"github.com/goxide-lang/syntax/token"
)

// Entry is one row of a launcher-generated, static entry table. Exactly one
// handler must be set. The SDK does not discover or dynamically register macros.
type Entry struct {
	Name      string
	Function  func(Context, token.TokenStream) Output
	Attribute func(Context, token.TokenStream, token.TokenStream) Output
	Derive    func(Context, token.TokenStream) Output
}

// Dispatch validates a static table and invokes the entry selected by request.
// Handler panics are not swallowed: the execution host treats them as failures.
func Dispatch(request Request, entries []Entry) (Output, error) {
	if request.scope == nil {
		return Output{}, fmt.Errorf("invalid zero request")
	}
	names := make(map[string]bool, len(entries))
	selected := -1
	for i, entry := range entries {
		if !validName(entry.Name) {
			return Output{}, fmt.Errorf("invalid static entry name")
		}
		if names[entry.Name] {
			return Output{}, fmt.Errorf("duplicate static entry %q", entry.Name)
		}
		names[entry.Name] = true
		count := 0
		if entry.Function != nil {
			count++
		}
		if entry.Attribute != nil {
			count++
		}
		if entry.Derive != nil {
			count++
		}
		if count != 1 {
			return Output{}, fmt.Errorf("static entry %q must have exactly one handler", entry.Name)
		}
		if entry.Name == request.Entry() {
			selected = i
		}
	}
	if selected < 0 {
		return Output{}, fmt.Errorf("unknown static entry %q", request.Entry())
	}
	entry := entries[selected]
	ctx := request.Context()
	switch {
	case request.Kind() == Function && entry.Function != nil:
		return entry.Function(ctx, request.Input()), nil
	case request.Kind() == Attribute && entry.Attribute != nil:
		return entry.Attribute(ctx, request.Args(), request.Item()), nil
	case request.Kind() == Derive && entry.Derive != nil:
		return entry.Derive(ctx, request.Item()), nil
	default:
		return Output{}, fmt.Errorf("static entry %q does not match kind %q", entry.Name, request.Kind())
	}
}
