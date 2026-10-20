// Package proc_macro provides the ordinary Go SDK and transport for Goxide
// procedural macros. It does not execute WASM or access process I/O implicitly.
package proc_macro

import (
	"errors"
	"strings"

	"github.com/goxide-lang/syntax/token"
)

// Severity is the severity of a macro diagnostic.
type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
	Note    Severity = "note"
)

// Diagnostic is an ordered message with an optional diagnostic position.
// Its position does not change binding context or grant compiler permissions.
type Diagnostic struct {
	Severity Severity
	Message  string
	Span     token.Span
}

// Output is a macro's generated tokens and ordered diagnostics. An Error
// diagnostic makes the entire expansion fail.
type Output struct {
	Tokens      token.TokenStream
	Diagnostics []Diagnostic
}

func Success(tokens token.TokenStream) Output { return Output{Tokens: tokens} }

func Failure(message string, span token.Span) Output {
	return Output{Diagnostics: []Diagnostic{{Severity: Error, Message: message, Span: span}}}
}

// FromParse turns a parser result into output, preserving its position when the
// error implements Span. It never converts a failed parse into partial output.
func FromParse(tokens token.TokenStream, err error) Output {
	if err == nil {
		return Success(tokens)
	}
	var positioned interface{ Span() token.Span }
	var span token.Span
	if errors.As(err, &positioned) {
		span = positioned.Span()
	}
	return Failure(err.Error(), span)
}

// DiagnosticError preserves all diagnostics, in order, after an unsuccessful
// macro response. ReadResponse returns no usable tokens with this error.
type DiagnosticError struct {
	diagnostics []Diagnostic
}

func (e *DiagnosticError) Error() string {
	var messages []string
	for _, d := range e.diagnostics {
		if d.Severity == Error {
			messages = append(messages, d.Message)
		}
	}
	return "macro expansion failed: " + strings.Join(messages, "; ")
}

func (e *DiagnosticError) Diagnostics() []Diagnostic {
	return append([]Diagnostic(nil), e.diagnostics...)
}

func diagnosticError(ds []Diagnostic) error {
	for _, d := range ds {
		if d.Severity == Error {
			return &DiagnosticError{diagnostics: append([]Diagnostic(nil), ds...)}
		}
	}
	return nil
}
