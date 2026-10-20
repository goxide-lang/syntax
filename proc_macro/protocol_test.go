package proc_macro

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/goxide-lang/syntax/token"
)

func testConfig(t *testing.T, id string, kind Kind) (*token.Scope, RequestConfig) {
	t.Helper()
	scope := token.NewScope(id)
	def, call := scope.NewContext(), scope.NewContext()
	defSpan, err := scope.NewSpan(10, 20)
	if err != nil {
		t.Fatal(err)
	}
	callSpan, err := scope.NewSpan(100, 200)
	if err != nil {
		t.Fatal(err)
	}
	config := RequestConfig{
		Entry: "entry", Kind: kind, Position: ItemsPosition,
		DefSite: def, CallSite: call, DefSpan: defSpan, CallSpan: callSpan,
		Target: Target{OS: "linux", Arch: "arm64"}, Features: []string{"zeta", "alpha"},
	}
	input, err := token.NewIdent("input", false, call)
	if err != nil {
		t.Fatal(err)
	}
	switch kind {
	case Function:
		config.Input = token.New(input.WithSpan(callSpan))
		config.Position = ExpressionPosition
	case Attribute, Derive:
		config.Item = token.New(input.WithSpan(callSpan))
	}
	return scope, config
}

func testRequest(t *testing.T, id string, kind Kind) Request {
	t.Helper()
	scope, config := testConfig(t, id, kind)
	request, err := NewRequest(scope, config)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func guestRequest(t *testing.T, request Request) Request {
	t.Helper()
	var buffer bytes.Buffer
	if err := WriteRequest(&buffer, request); err != nil {
		t.Fatal(err)
	}
	guest, err := ReadRequest(&buffer)
	if err != nil {
		t.Fatal(err)
	}
	return guest
}

func TestRequestResponseRoundTrip(t *testing.T) {
	host := testRequest(t, "round-trip", Function)
	guest := guestRequest(t, host)
	result, err := Dispatch(guest, []Entry{{
		Name: "entry",
		Function: func(ctx Context, input token.TokenStream) Output {
			tokens := input.Tokens()
			tokens[0] = tokens[0].WithSpan(ctx.DefSpan())
			created, err := token.NewIdent("generated", false, token.BindingContext{})
			if err != nil {
				t.Fatal(err)
			}
			caller, err := token.NewIdent("caller", false, ctx.CallSite())
			if err != nil {
				t.Fatal(err)
			}
			return Output{
				Tokens: token.New(tokens[0], created, caller),
				Diagnostics: []Diagnostic{
					{Severity: Note, Message: "first", Span: ctx.CallSpan()},
					{Severity: Warning, Message: "second", Span: ctx.DefSpan()},
				},
			}
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var response bytes.Buffer
	if err := WriteResponse(&response, guest, result); err != nil {
		t.Fatal(err)
	}
	output, err := ReadResponse(&response, host)
	if err != nil {
		t.Fatal(err)
	}
	got := output.Tokens.Tokens()
	if len(got) != 3 {
		t.Fatalf("got %d output tokens", len(got))
	}
	if got[0].Reference() != host.Input().Tokens()[0].Reference() ||
		got[0].Context() != host.Context().CallSite() ||
		got[0].Span() != host.Context().DefSpan() {
		t.Fatal("changing display span changed input identity or binding")
	}
	if got[1].Context() != host.Context().DefSite() || got[2].Context() != host.Context().CallSite() {
		t.Fatal("new default and explicit call-site contexts were not preserved")
	}
	if len(output.Diagnostics) != 2 || output.Diagnostics[0].Message != "first" ||
		output.Diagnostics[1].Message != "second" {
		t.Fatalf("diagnostic order changed: %+v", output.Diagnostics)
	}
}

func TestRequestFreezesCapabilities(t *testing.T) {
	scope, config := testConfig(t, "frozen", Function)
	rootContext := scope.NewContext()
	root, err := token.NewIdent("canonical_runtime", false, rootContext)
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeRoots = map[string]token.TokenStream{"example.org/runtime": token.New(root)}
	request, err := NewRequest(scope, config)
	if err != nil {
		t.Fatal(err)
	}
	var first bytes.Buffer
	if err := WriteRequest(&first, request); err != nil {
		t.Fatal(err)
	}
	config.Features[0] = "mutated"
	delete(config.RuntimeRoots, "example.org/runtime")
	newContext := scope.NewContext()
	newSpan, err := scope.NewSpan(300, 400)
	if err != nil {
		t.Fatal(err)
	}
	newNode, err := token.NewIdent("later", false, newContext)
	if err != nil {
		t.Fatal(err)
	}
	newInput, err := scope.Capture(token.New(newNode), newContext)
	if err != nil {
		t.Fatal(err)
	}
	var second bytes.Buffer
	if err := WriteRequest(&second, request); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("caller mutations changed the frozen request")
	}
	ctx := request.Context()
	features := ctx.Features()
	features[0] = "mutated"
	if !ctx.HasFeature("alpha") || !ctx.HasFeature("zeta") || ctx.HasFeature("mutated") {
		t.Fatal("context features are mutable")
	}
	if ctx.Target() != (Target{OS: "linux", Arch: "arm64"}) {
		t.Fatalf("target changed: %+v", ctx.Target())
	}
	gotRoot, err := ctx.RuntimeRoot("example.org/runtime")
	if err != nil || gotRoot.Tokens()[0].Context().Reference() != rootContext.Reference() {
		t.Fatalf("canonical root binding was lost: %v", err)
	}
	_, err = ctx.RuntimeRoot("example.org/missing")
	var position interface{ Span() token.Span }
	if !errors.As(err, &position) || position.Span() != ctx.CallSpan() {
		t.Fatalf("missing runtime root did not report the call position: %v", err)
	}
	for name, payload := range map[string]string{
		"later context": fmt.Sprintf(`{"id":"frozen","tokens":[{"kind":"ident","text":"x","context":%d}]}`, newContext.Reference()),
		"later input":   fmt.Sprintf(`{"id":"frozen","tokens":[{"ref":%d}]}`, newInput.Tokens()[0].Reference()),
		"later span":    fmt.Sprintf(`{"id":"frozen","diagnostics":[{"severity":"note","message":"x","span":{"ref":%d,"start":300,"end":400}}]}`, newSpan.Reference()),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadResponse(strings.NewReader(payload), request); err == nil {
				t.Fatal("accepted capability added after the request snapshot")
			}
		})
	}
}

func TestEquivalentRequestsEncodeDeterministically(t *testing.T) {
	makeRequest := func(reverse bool) []byte {
		scope, config := testConfig(t, "deterministic", Function)
		first, err := token.NewIdent("first", false, config.CallSite)
		if err != nil {
			t.Fatal(err)
		}
		second, err := token.NewIdent("second", false, config.CallSite)
		if err != nil {
			t.Fatal(err)
		}
		config.RuntimeRoots = make(map[string]token.TokenStream)
		if reverse {
			config.Features = []string{"alpha", "zeta"}
			config.RuntimeRoots["example.org/b"] = token.New(second)
			config.RuntimeRoots["example.org/a"] = token.New(first)
		} else {
			config.RuntimeRoots["example.org/a"] = token.New(first)
			config.RuntimeRoots["example.org/b"] = token.New(second)
		}
		request, err := NewRequest(scope, config)
		if err != nil {
			t.Fatal(err)
		}
		var buffer bytes.Buffer
		if err := WriteRequest(&buffer, request); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	if !bytes.Equal(makeRequest(false), makeRequest(true)) {
		t.Fatal("equivalent maps or feature order produced different reference tables")
	}
}

func TestErrorResponsePreservesDiagnosticsWithoutTokens(t *testing.T) {
	host := testRequest(t, "failure", Function)
	guest := guestRequest(t, host)
	output := Output{
		Tokens: guest.Input(),
		Diagnostics: []Diagnostic{
			{Severity: Note, Message: "context"},
			{Severity: Error, Message: "failed", Span: guest.Context().CallSpan()},
			{Severity: Warning, Message: "follow-up"},
		},
	}
	var buffer bytes.Buffer
	if err := WriteResponse(&buffer, guest, output); err != nil {
		t.Fatal(err)
	}
	got, err := ReadResponse(&buffer, host)
	var diagnostic *DiagnosticError
	if !errors.As(err, &diagnostic) {
		t.Fatalf("got %v, want DiagnosticError", err)
	}
	if !got.Tokens.IsEmpty() || len(got.Diagnostics) != 3 || len(diagnostic.Diagnostics()) != 3 {
		t.Fatalf("failed response leaked tokens or lost diagnostics: %+v", got)
	}
	got.Diagnostics[0].Message = "changed"
	copy := diagnostic.Diagnostics()
	copy[1].Message = "changed"
	if diagnostic.Diagnostics()[0].Message != "context" || diagnostic.Diagnostics()[1].Message != "failed" {
		t.Fatal("DiagnosticError aliases returned mutable slices")
	}
}

func TestFragmentReferenceAndObservableViewRoundTrip(t *testing.T) {
	scope, config := testConfig(t, "fragment", Function)
	view, err := token.ParseText("left + right", config.CallSite)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := scope.CaptureFragment(token.ExprFragment, view, config.CallSite)
	if err != nil {
		t.Fatal(err)
	}
	config.Input = token.New(fragment)
	host, err := NewRequest(scope, config)
	if err != nil {
		t.Fatal(err)
	}
	guest := guestRequest(t, host)
	input := guest.Input().Tokens()[0]
	if input.Kind() != token.FragmentKind || input.FragmentType() != token.ExprFragment ||
		input.Reference() != fragment.Reference() {
		t.Fatal("fragment identity or category changed on the way to the guest")
	}
	var visit func(token.TokenStream)
	visit = func(stream token.TokenStream) {
		for _, child := range stream.Tokens() {
			if child.Reference() != 0 || child.Kind() == token.FragmentKind {
				t.Fatal("observable fragment view contains trusted input references")
			}
			visit(child.Children())
		}
	}
	visit(input.Children())
	var buffer bytes.Buffer
	if err := WriteResponse(&buffer, guest, Success(token.New(input.WithSpan(guest.Context().DefSpan())))); err != nil {
		t.Fatal(err)
	}
	retained, err := ReadResponse(&buffer, host)
	if err != nil {
		t.Fatal(err)
	}
	got := retained.Tokens.Tokens()[0]
	if got.Reference() != fragment.Reference() || got.FragmentType() != token.ExprFragment ||
		got.Context() != host.Context().CallSite() || got.Span() != host.Context().DefSpan() {
		t.Fatal("fragment pass-through lost identity, category, context or display span")
	}
	buffer.Reset()
	if err := WriteResponse(&buffer, guest, Success(input.Children())); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := ReadResponse(&buffer, host)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.Tokens.IsEmpty() {
		t.Fatal("fragment view unexpectedly disappeared")
	}
	visit(rebuilt.Tokens)
	for _, child := range rebuilt.Tokens.Tokens() {
		if child.Context() != host.Context().CallSite() {
			t.Fatal("reconstructed view lost its explicit binding context")
		}
	}
}

func TestFromParse(t *testing.T) {
	request := testRequest(t, "parse", Function)
	good := FromParse(token.ParseText("answer", request.Context().DefSite()))
	if good.Tokens.IsEmpty() || len(good.Diagnostics) != 0 {
		t.Fatalf("successful parse failed: %+v", good)
	}
	bad := FromParse(token.ParseText("\"unterminated", request.Context().DefSite()))
	if !bad.Tokens.IsEmpty() || len(bad.Diagnostics) != 1 || bad.Diagnostics[0].Severity != Error {
		t.Fatalf("parse failure lost diagnostic: %+v", bad)
	}
	var response bytes.Buffer
	if err := WriteResponse(&response, request, bad); err != nil {
		t.Fatalf("parse failure position cannot be serialized: %v", err)
	}
}

func TestStrictResponseJSON(t *testing.T) {
	request := testRequest(t, "strict", Function)
	tests := map[string]string{
		"duplicate":         `{"id":"strict","id":"strict"}`,
		"escaped duplicate": `{"id":"strict","i\u0064":"strict"}`,
		"nested duplicate":  `{"id":"strict","tokens":[{"ref":1,"ref":1}]}`,
		"unknown":           `{"id":"strict","trusted":true}`,
		"case alias":        `{"ID":"strict"}`,
		"nested alias":      `{"id":"strict","tokens":[{"Ref":1}]}`,
		"unknown nested":    `{"id":"strict","tokens":[{"ref":1,"checked":true}]}`,
		"zero context":      `{"id":"strict","tokens":[{"ref":1,"context":0}]}`,
		"false raw":         `{"id":"strict","tokens":[{"ref":1,"raw":false}]}`,
		"empty text":        `{"id":"strict","tokens":[{"ref":1,"text":""}]}`,
		"null children":     `{"id":"strict","tokens":[{"ref":1,"children":null}]}`,
		"zero reference":    `{"id":"strict","tokens":[{"ref":0,"kind":"ident","text":"x","context":1}]}`,
		"new fragment":      `{"id":"strict","tokens":[{"kind":"fragment","fragment":"expr","context":1}]}`,
		"negative":          `{"id":"strict","tokens":[{"ref":-1}]}`,
		"overflow":          `{"id":"strict","tokens":[{"ref":4294967296}]}`,
		"decimal":           `{"id":"strict","tokens":[{"ref":1.0}]}`,
		"exponent":          `{"id":"strict","tokens":[{"ref":1e0}]}`,
		"bad type":          `{"id":true}`,
		"null scalar":       `{"id":null}`,
		"extra value":       `{"id":"strict"} {"id":"strict"}`,
		"trailing junk":     `{"id":"strict"} x`,
		"unknown severity":  `{"id":"strict","diagnostics":[{"severity":"fatal","message":"x"}]}`,
		"empty diagnostic":  `{"id":"strict","diagnostics":[{"severity":"error","message":""}]}`,
		"high surrogate":    `{"id":"strict","diagnostics":[{"severity":"note","message":"\ud800"}]}`,
		"low surrogate":     `{"id":"strict","diagnostics":[{"severity":"note","message":"\udc00"}]}`,
		"wrong call":        `{"id":"another-call"}`,
		"unknown ref":       `{"id":"strict","tokens":[{"ref":2}]}`,
		"invalid UTF-8":     "{\"id\":\"strict\",\"diagnostics\":[{\"severity\":\"note\",\"message\":\"\xff\"}]}",
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			output, err := ReadResponse(strings.NewReader(payload), request)
			if err == nil || !output.Tokens.IsEmpty() {
				t.Fatalf("accepted invalid payload: output=%+v err=%v", output, err)
			}
		})
	}
	valid := " \n" + `{"id":"strict","diagnostics":[{"severity":"note","message":"\ud83d\ude00"}]}` + "\t\n"
	if output, err := ReadResponse(strings.NewReader(valid), request); err != nil ||
		len(output.Diagnostics) != 1 || output.Diagnostics[0].Message != "😀" {
		t.Fatalf("valid surrogate pair or framing whitespace rejected: %+v, %v", output, err)
	}
}

func TestStrictRequestJSON(t *testing.T) {
	request := testRequest(t, "request", Function)
	var buffer bytes.Buffer
	if err := WriteRequest(&buffer, request); err != nil {
		t.Fatal(err)
	}
	raw := buffer.String()
	tests := map[string]string{
		"case alias":        strings.Replace(raw, `"def_site"`, `"Def_Site"`, 1),
		"duplicate context": strings.Replace(raw, `"def_site":1`, `"def_site":1,"def_site":1`, 1),
		"unknown field":     strings.TrimSuffix(raw, "}") + `,"environment":{}}`,
		"unknown context":   strings.Replace(raw, `"call_site":2`, `"call_site":999`, 1),
		"unknown kind":      strings.Replace(raw, `"kind":"function"`, `"kind":"other"`, 1),
		"bad slot":          strings.Replace(raw, `"position":"expr"`, `"position":"pattern"`, 1),
		"duplicate feature": strings.Replace(raw, `["alpha","zeta"]`, `["alpha","alpha"]`, 1),
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			if payload == raw {
				t.Fatal("test failed to modify request")
			}
			if _, err := ReadRequest(strings.NewReader(payload)); err == nil {
				t.Fatal("accepted invalid request")
			}
		})
	}
}

func TestProtocolBudgetsAndNoPartialOutput(t *testing.T) {
	request := testRequest(t, "budget", Function)
	t.Run("message boundary", func(t *testing.T) {
		base := `{"id":"budget"}`
		atLimit := base + strings.Repeat(" ", MaxMessageBytes-len(base))
		if _, err := ReadResponse(strings.NewReader(atLimit), request); err != nil {
			t.Fatalf("exact message limit rejected: %v", err)
		}
		if _, err := ReadResponse(strings.NewReader(atLimit+" "), request); err == nil {
			t.Fatal("oversized message accepted")
		}
	})
	t.Run("diagnostic boundary", func(t *testing.T) {
		empty, err := json.Marshal([]wireDiagnostic{{Severity: Note, Message: ""}})
		if err != nil {
			t.Fatal(err)
		}
		message := strings.Repeat("a", MaxDiagnosticBytes-len(empty))
		var buffer bytes.Buffer
		output := Output{Diagnostics: []Diagnostic{{Severity: Note, Message: message}}}
		if err := WriteResponse(&buffer, request, output); err != nil {
			t.Fatalf("exact diagnostic limit rejected: %v", err)
		}
		if _, err := ReadResponse(&buffer, request); err != nil {
			t.Fatalf("exact diagnostic limit failed to read: %v", err)
		}
		output.Diagnostics[0].Message += "a"
		buffer.Reset()
		if err := WriteResponse(&buffer, request, output); err == nil || buffer.Len() != 0 {
			t.Fatal("oversized diagnostics accepted or partially written")
		}
		payload := `{"id":"budget","diagnostics":[{"severity":"note","message":"` + message + `a"}]}`
		if _, err := ReadResponse(strings.NewReader(payload), request); err == nil {
			t.Fatal("oversized diagnostics read")
		}
	})
	t.Run("semantic depth", func(t *testing.T) {
		stream := token.TokenStream{}
		for i := 0; i < token.MaxDepth; i++ {
			group, err := token.NewGroup(token.Parenthesis, stream, token.BindingContext{})
			if err != nil {
				t.Fatal(err)
			}
			stream = token.New(group)
		}
		var buffer bytes.Buffer
		if err := WriteResponse(&buffer, request, Success(stream)); err != nil {
			t.Fatalf("exact semantic depth rejected by JSON framing: %v", err)
		}
		if _, err := ReadResponse(&buffer, request); err != nil {
			t.Fatalf("exact semantic depth failed to read: %v", err)
		}
		group, err := token.NewGroup(token.Parenthesis, stream, token.BindingContext{})
		if err != nil {
			t.Fatal(err)
		}
		buffer.Reset()
		if err := WriteResponse(&buffer, request, Success(token.New(group))); err == nil || buffer.Len() != 0 {
			t.Fatal("excessive depth accepted or partially written")
		}
	})
	t.Run("expanded reference count", func(t *testing.T) {
		item := request.Input().Tokens()[0]
		tokens := make([]token.Token, token.MaxTokens)
		for i := range tokens {
			tokens[i] = item
		}
		var buffer bytes.Buffer
		if err := WriteResponse(&buffer, request, Success(token.New(tokens...))); err != nil {
			t.Fatalf("exact token budget rejected: %v", err)
		}
		if _, err := ReadResponse(&buffer, request); err != nil {
			t.Fatalf("exact token budget failed to read: %v", err)
		}
		tokens = append(tokens, item)
		buffer.Reset()
		if err := WriteResponse(&buffer, request, Success(token.New(tokens...))); err == nil || buffer.Len() != 0 {
			t.Fatal("excessive reference count accepted or partially written")
		}
		payload := `{"id":"budget","tokens":[` + strings.Repeat(`{"ref":1},`, token.MaxTokens) + `{"ref":1}]}`
		if _, err := ReadResponse(strings.NewReader(payload), request); err == nil {
			t.Fatal("excessive response reference count accepted")
		}
	})
}

func TestRequestAppliesOneBudgetAcrossStreamsAndRoots(t *testing.T) {
	scope, config := testConfig(t, "combined", Function)
	ident, err := token.NewIdent("value", false, config.CallSite)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]token.Token, 1000)
	for i := range items {
		items[i] = ident
	}
	group, err := token.NewGroup(token.Parenthesis, token.New(items...), config.CallSite)
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeRoots = make(map[string]token.TokenStream)
	for i := 0; i < 101; i++ {
		config.RuntimeRoots[fmt.Sprintf("example.org/%03d", i)] = token.New(group)
	}
	if _, err := NewRequest(scope, config); err == nil {
		t.Fatal("separate roots each received a new semantic budget")
	}
	snapshot, err := scope.Snapshot()
	if err != nil || len(snapshot.Inputs) != 0 {
		t.Fatalf("failed aggregate capture changed scope: %v", err)
	}

	config.RuntimeRoots = nil
	config.Input = token.New(group)
	request, err := NewRequest(scope, config)
	if err != nil {
		t.Fatal(err)
	}
	wire := request.wire
	wire.Roots = make(map[string][]token.WireToken)
	for i := 0; i < 101; i++ {
		wire.Roots[fmt.Sprintf("example.org/%03d", i)] = wire.Input
	}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRequest(bytes.NewReader(data)); err == nil {
		t.Fatal("received roots each received a new semantic budget")
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestProtocolWriterErrors(t *testing.T) {
	request := testRequest(t, "writer", Function)
	if err := WriteRequest(shortWriter{}, request); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write returned %v", err)
	}
	if err := WriteResponse(shortWriter{}, request, Success(request.Input())); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write returned %v", err)
	}
	other := testRequest(t, "other", Function)
	var output bytes.Buffer
	if err := WriteResponse(&output, request, Success(other.Input())); err == nil || output.Len() != 0 {
		t.Fatal("foreign input accepted or partially written")
	}
}
