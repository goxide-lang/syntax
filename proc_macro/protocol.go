package proc_macro

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"unicode/utf8"

	"github.com/goxide-lang/syntax/token"
)

const (
	// MaxMessageBytes bounds each complete request or response, including JSON.
	MaxMessageBytes = 8 << 20
	// MaxDiagnosticBytes bounds the canonical JSON encoding of all diagnostics.
	// Any execution host's stderr allowance must share this budget.
	MaxDiagnosticBytes = 64 << 10
)

type wireRequest struct {
	Scope    token.ScopeData              `json:"scope"`
	Entry    string                       `json:"entry"`
	Kind     Kind                         `json:"kind"`
	Position Position                     `json:"position"`
	DefSite  uint32                       `json:"def_site"`
	CallSite uint32                       `json:"call_site"`
	DefSpan  token.WireSpan               `json:"def_span"`
	CallSpan token.WireSpan               `json:"call_span"`
	Target   Target                       `json:"target"`
	Features []string                     `json:"features,omitempty"`
	Roots    map[string][]token.WireToken `json:"roots,omitempty"`
	Input    []token.WireToken            `json:"input,omitempty"`
	Args     []token.WireToken            `json:"args,omitempty"`
	Item     []token.WireToken            `json:"item,omitempty"`
}

type wireDiagnostic struct {
	Severity Severity        `json:"severity"`
	Message  string          `json:"message"`
	Span     *token.WireSpan `json:"span,omitempty"`
}

type wireResponse struct {
	ID          string            `json:"id"`
	Tokens      []token.WireToken `json:"tokens,omitempty"`
	Diagnostics []wireDiagnostic  `json:"diagnostics,omitempty"`
}

// NewRequest captures the host-supplied streams and takes an independent scope
// snapshot. Later changes to scope or config cannot add capabilities to it.
func NewRequest(scope *token.Scope, config RequestConfig) (Request, error) {
	if scope == nil || !validName(scope.ID()) {
		return Request{}, fmt.Errorf("request needs a scope with a nonempty UTF-8 ID")
	}
	if err := validateHeader(config.Entry, config.Kind, config.Position, config.Target); err != nil {
		return Request{}, err
	}
	if err := validateStreams(config.Kind, config.Input, config.Args, config.Item); err != nil {
		return Request{}, err
	}
	def, err := scope.ContextID(config.DefSite)
	if err != nil || def == 0 {
		return Request{}, fmt.Errorf("request definition context must belong to its scope")
	}
	call, err := scope.ContextID(config.CallSite)
	if err != nil || call == 0 {
		return Request{}, fmt.Errorf("request call context must belong to its scope")
	}
	defSpan, err := scope.EncodeSpan(config.DefSpan)
	if err != nil {
		return Request{}, fmt.Errorf("definition span: %w", err)
	}
	callSpan, err := scope.EncodeSpan(config.CallSpan)
	if err != nil {
		return Request{}, fmt.Errorf("call span: %w", err)
	}
	features, err := normalizeFeatures(config.Features)
	if err != nil {
		return Request{}, err
	}
	wire := wireRequest{
		Entry: config.Entry, Kind: config.Kind, Position: config.Position,
		DefSite: def, CallSite: call, DefSpan: defSpan, CallSpan: callSpan,
		Target: config.Target, Features: features,
	}
	if len(config.RuntimeRoots) > token.MaxNodes {
		return Request{}, fmt.Errorf("too many runtime roots")
	}
	keys := sortedKeys(config.RuntimeRoots)
	var all []token.Token
	add := func(stream token.TokenStream) error {
		if stream.Len() > token.MaxNodes-len(all) {
			return fmt.Errorf("request streams exceed %d nodes", token.MaxNodes)
		}
		all = append(all, stream.Tokens()...)
		return nil
	}
	for _, stream := range []token.TokenStream{config.Input, config.Args, config.Item} {
		if err := add(stream); err != nil {
			return Request{}, err
		}
	}
	for _, key := range keys {
		if !validName(key) || config.RuntimeRoots[key].IsEmpty() {
			return Request{}, fmt.Errorf("invalid runtime root %q", key)
		}
		if err := add(config.RuntimeRoots[key]); err != nil {
			return Request{}, err
		}
	}
	// Capture live streams together to apply one expanded-tree budget and
	// roll back the entire capture when any stream is invalid.
	captured, err := scope.Capture(token.New(all...), config.CallSite)
	if err != nil {
		return Request{}, fmt.Errorf("request streams: %w", err)
	}
	encoded, err := scope.Encode(captured, config.CallSite)
	if err != nil {
		return Request{}, err
	}
	take := func(n int) []token.WireToken {
		result := encoded[:n]
		encoded = encoded[n:]
		return result
	}
	wire.Input, wire.Args, wire.Item = take(config.Input.Len()), take(config.Args.Len()), take(config.Item.Len())
	if len(keys) != 0 {
		wire.Roots = make(map[string][]token.WireToken, len(keys))
	}
	for _, key := range keys {
		wire.Roots[key] = take(config.RuntimeRoots[key].Len())
	}
	wire.Scope, err = scope.Snapshot()
	if err != nil {
		return Request{}, err
	}
	// The same validation and size bound apply to local and received requests.
	if _, err := encodeMessage(wire); err != nil {
		return Request{}, err
	}
	return restoreRequest(wire)
}

func normalizeFeatures(features []string) ([]string, error) {
	if len(features) > token.MaxNodes {
		return nil, fmt.Errorf("too many features")
	}
	out := append([]string(nil), features...)
	sort.Strings(out)
	for i, feature := range out {
		if !validName(feature) || i > 0 && out[i-1] == feature {
			return nil, fmt.Errorf("invalid or duplicate feature %q", feature)
		}
	}
	return out, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func validateStreams(kind Kind, input, args, item token.TokenStream) error {
	switch kind {
	case Function:
		if !args.IsEmpty() || !item.IsEmpty() {
			return fmt.Errorf("function macro cannot receive args or item streams")
		}
	case Attribute:
		if !input.IsEmpty() {
			return fmt.Errorf("attribute macro cannot receive an input stream")
		}
	case Derive:
		if !input.IsEmpty() || !args.IsEmpty() {
			return fmt.Errorf("derive macro cannot receive input or args streams")
		}
	}
	return nil
}

func restoreRequest(wire wireRequest) (Request, error) {
	if !validName(wire.Scope.ID) {
		return Request{}, fmt.Errorf("invalid request scope ID")
	}
	if err := validateHeader(wire.Entry, wire.Kind, wire.Position, wire.Target); err != nil {
		return Request{}, err
	}
	scope, err := token.RestoreScope(wire.Scope)
	if err != nil {
		return Request{}, fmt.Errorf("request scope: %w", err)
	}
	if wire.DefSite == 0 || wire.CallSite == 0 {
		return Request{}, fmt.Errorf("request contexts must be nonzero references")
	}
	ctx := Context{target: wire.Target}
	if ctx.defSite, err = scope.Context(wire.DefSite); err != nil {
		return Request{}, fmt.Errorf("definition context: %w", err)
	}
	if ctx.callSite, err = scope.Context(wire.CallSite); err != nil {
		return Request{}, fmt.Errorf("call context: %w", err)
	}
	if ctx.defSpan, err = scope.DecodeSpan(wire.DefSpan); err != nil {
		return Request{}, fmt.Errorf("definition span: %w", err)
	}
	if ctx.callSpan, err = scope.DecodeSpan(wire.CallSpan); err != nil {
		return Request{}, fmt.Errorf("call span: %w", err)
	}
	if ctx.features, err = normalizeFeatures(wire.Features); err != nil {
		return Request{}, err
	}
	if len(wire.Roots) > token.MaxNodes {
		return Request{}, fmt.Errorf("too many runtime roots")
	}
	// Decode all live streams together so repeated references and roots share
	// the expansion budget rather than each receiving a separate allowance.
	var all []token.WireToken
	add := func(stream []token.WireToken) error {
		if len(stream) > token.MaxNodes-len(all) {
			return fmt.Errorf("request streams exceed %d nodes", token.MaxNodes)
		}
		all = append(all, stream...)
		return nil
	}
	for _, stream := range [][]token.WireToken{wire.Input, wire.Args, wire.Item} {
		if err := add(stream); err != nil {
			return Request{}, err
		}
	}
	keys := sortedKeys(wire.Roots)
	for _, key := range keys {
		if !validName(key) || len(wire.Roots[key]) == 0 {
			return Request{}, fmt.Errorf("invalid runtime root %q", key)
		}
		if err := add(wire.Roots[key]); err != nil {
			return Request{}, err
		}
	}
	decoded, err := scope.Decode(all)
	if err != nil {
		return Request{}, fmt.Errorf("request streams: %w", err)
	}
	tokens := decoded.Tokens()
	take := func(n int) token.TokenStream {
		result := token.New(tokens[:n]...)
		tokens = tokens[n:]
		return result
	}
	request := Request{wire: wire, scope: scope}
	request.input = take(len(wire.Input))
	request.args = take(len(wire.Args))
	request.item = take(len(wire.Item))
	if err := validateStreams(wire.Kind, request.input, request.args, request.item); err != nil {
		return Request{}, err
	}
	ctx.roots = make(map[string]token.TokenStream, len(keys))
	for _, key := range keys {
		ctx.roots[key] = take(len(wire.Roots[key]))
	}
	request.context = ctx
	return request, nil
}

// WriteRequest writes one complete UTF-8 JSON request. It does not close w.
// Validation completes before any bytes are written.
func WriteRequest(w io.Writer, request Request) error {
	if request.scope == nil {
		return fmt.Errorf("invalid zero request")
	}
	data, err := encodeMessage(request.wire)
	if err != nil {
		return err
	}
	return writeMessage(w, data)
}

// ReadRequest accepts exactly one JSON request followed by EOF.
func ReadRequest(r io.Reader) (Request, error) {
	var wire wireRequest
	if err := readMessage(r, &wire); err != nil {
		return Request{}, err
	}
	return restoreRequest(wire)
}

// WriteResponse validates all tokens and diagnostics before writing any bytes.
// Error diagnostics are valid failure responses; they are not Go I/O errors.
func WriteResponse(w io.Writer, request Request, output Output) error {
	if request.scope == nil {
		return fmt.Errorf("invalid zero request")
	}
	tokens, err := request.scope.Encode(output.Tokens, request.context.defSite)
	if err != nil {
		return fmt.Errorf("response tokens: %w", err)
	}
	wire := wireResponse{ID: request.ID(), Tokens: tokens}
	if len(output.Diagnostics) > MaxDiagnosticBytes {
		return fmt.Errorf("too many diagnostics")
	}
	messageBytes := 0
	for _, d := range output.Diagnostics {
		if err := validateDiagnostic(d.Severity, d.Message); err != nil {
			return err
		}
		if len(d.Message) > MaxDiagnosticBytes-messageBytes {
			return fmt.Errorf("diagnostics exceed %d bytes", MaxDiagnosticBytes)
		}
		messageBytes += len(d.Message)
		wd := wireDiagnostic{Severity: d.Severity, Message: d.Message}
		if d.Span != (token.Span{}) {
			span, err := request.scope.EncodeSpan(d.Span)
			if err != nil {
				return fmt.Errorf("diagnostic span: %w", err)
			}
			wd.Span = &span
		}
		wire.Diagnostics = append(wire.Diagnostics, wd)
	}
	if err := checkDiagnosticsSize(wire.Diagnostics); err != nil {
		return err
	}
	data, err := encodeMessage(wire)
	if err != nil {
		return err
	}
	return writeMessage(w, data)
}

// ReadResponse admits a response only against request's frozen reference table.
// An Error diagnostic returns a DiagnosticError and no output tokens; all
// diagnostics remain available in both the returned Output and the error.
func ReadResponse(r io.Reader, request Request) (Output, error) {
	if request.scope == nil {
		return Output{}, fmt.Errorf("invalid zero request")
	}
	var wire wireResponse
	if err := readMessage(r, &wire); err != nil {
		return Output{}, err
	}
	if wire.ID != request.ID() {
		return Output{}, fmt.Errorf("response request ID does not match")
	}
	if err := checkDiagnosticsSize(wire.Diagnostics); err != nil {
		return Output{}, err
	}
	output := Output{}
	for _, d := range wire.Diagnostics {
		if err := validateDiagnostic(d.Severity, d.Message); err != nil {
			return Output{}, err
		}
		diagnostic := Diagnostic{Severity: d.Severity, Message: d.Message}
		if d.Span != nil {
			span, err := request.scope.DecodeSpan(*d.Span)
			if err != nil {
				return Output{}, fmt.Errorf("diagnostic span: %w", err)
			}
			diagnostic.Span = span
		}
		output.Diagnostics = append(output.Diagnostics, diagnostic)
	}
	tokens, err := request.scope.Decode(wire.Tokens)
	if err != nil {
		return Output{}, fmt.Errorf("response tokens: %w", err)
	}
	if err := diagnosticError(output.Diagnostics); err != nil {
		return output, err
	}
	output.Tokens = tokens
	return output, nil
}

func validateDiagnostic(severity Severity, message string) error {
	switch severity {
	case Error, Warning, Note:
	default:
		return fmt.Errorf("unknown diagnostic severity %q", severity)
	}
	if message == "" || !utf8.ValidString(message) {
		return fmt.Errorf("diagnostic message must be nonempty UTF-8")
	}
	return nil
}

func checkDiagnosticsSize(diagnostics []wireDiagnostic) error {
	data, err := json.Marshal(diagnostics)
	if err != nil {
		return err
	}
	if len(data) > MaxDiagnosticBytes {
		return fmt.Errorf("diagnostics exceed %d bytes", MaxDiagnosticBytes)
	}
	return nil
}

func readMessage(r io.Reader, destination any) error {
	if r == nil {
		return fmt.Errorf("nil protocol reader")
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxMessageBytes+1))
	if err != nil {
		return fmt.Errorf("read protocol message: %w", err)
	}
	if len(data) > MaxMessageBytes {
		return fmt.Errorf("protocol message exceeds %d bytes", MaxMessageBytes)
	}
	return decodeStrict(data, destination)
}

func encodeMessage(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxMessageBytes {
		return nil, fmt.Errorf("protocol message exceeds %d bytes", MaxMessageBytes)
	}
	// Check the same strict structural shape on output. In particular, a
	// hand-constructed transport value cannot exploit omitted zero fields.
	if err := validateJSON(data, value); err != nil {
		return nil, err
	}
	return data, nil
}

func writeMessage(w io.Writer, data []byte) error {
	if w == nil {
		return fmt.Errorf("nil protocol writer")
	}
	n, err := w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

// Keep the byte parser independent of any process-level stdin/stdout choices.
func decodeStrict(data []byte, destination any) error {
	if err := validateJSON(data, destination); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid protocol JSON: %w", err)
	}
	return nil
}
