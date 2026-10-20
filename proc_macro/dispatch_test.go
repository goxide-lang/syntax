package proc_macro

import (
	"testing"

	"github.com/goxide-lang/syntax/token"
)

func TestDispatchKinds(t *testing.T) {
	for _, kind := range []Kind{Function, Attribute, Derive} {
		t.Run(string(kind), func(t *testing.T) {
			request := testRequest(t, "dispatch", kind)
			called := false
			helper := func(ctx Context, stream token.TokenStream) Output {
				called = true
				if ctx.CallSite() != request.Context().CallSite() || stream.IsEmpty() {
					t.Fatal("dispatch changed context or selected the wrong stream")
				}
				return Success(stream)
			}
			entry := Entry{Name: "entry"}
			switch kind {
			case Function:
				entry.Function = helper
			case Attribute:
				entry.Attribute = func(ctx Context, args, item token.TokenStream) Output {
					if !args.IsEmpty() {
						t.Fatal("unexpected attribute arguments")
					}
					return helper(ctx, item)
				}
			case Derive:
				entry.Derive = helper
			}
			output, err := Dispatch(request, []Entry{entry})
			if err != nil || !called || output.Tokens.IsEmpty() {
				t.Fatalf("dispatch failed: called=%v output=%+v err=%v", called, output, err)
			}
		})
	}
}

func TestDispatchRejectsInvalidTablesBeforeCalling(t *testing.T) {
	request := testRequest(t, "dispatch", Function)
	called := false
	handler := func(Context, token.TokenStream) Output {
		called = true
		return Output{}
	}
	for name, entries := range map[string][]Entry{
		"unknown":    {{Name: "other", Function: handler}},
		"duplicate":  {{Name: "entry", Function: handler}, {Name: "entry", Function: handler}},
		"empty":      {{Name: "", Function: handler}},
		"missing":    {{Name: "entry"}},
		"multiple":   {{Name: "entry", Function: handler, Derive: handler}},
		"wrong kind": {{Name: "entry", Derive: handler}},
		"invalid unselected": {
			{Name: "entry", Function: handler},
			{Name: "unused"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Dispatch(request, entries); err == nil {
				t.Fatal("accepted invalid static table")
			}
			if called {
				t.Fatal("executed entry before validating entire static table")
			}
		})
	}
}
