package proc_macro

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func FuzzProtocolReaders(f *testing.F) {
	requestJSON := []byte(`{"scope":{"id":"fuzz","contexts":2,"spans":[],"inputs":[]},"entry":"entry","kind":"function","position":"expr","def_site":1,"call_site":2,"def_span":{"start":0,"end":0},"call_span":{"start":0,"end":0},"target":{"os":"linux","arch":"amd64"}}`)
	request, err := ReadRequest(bytes.NewReader(requestJSON))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(true, requestJSON)
	f.Add(true, []byte(`{"scope":{"id":"x","id":"y"}}`))
	f.Add(false, []byte(`{"id":"fuzz","tokens":[{"kind":"ident","text":"answer","context":1}]}`))
	f.Add(false, []byte(`{"id":"fuzz","diagnostics":[{"severity":"error","message":"failed"}]}`))
	f.Add(false, []byte(`{"id":"fuzz","tokens":[{"ref":1,"raw":false}]}`))
	f.Fuzz(func(t *testing.T, isRequest bool, data []byte) {
		var first, second bytes.Buffer
		if isRequest {
			decoded, err := ReadRequest(bytes.NewReader(data))
			if err != nil {
				return
			}
			if err := WriteRequest(&first, decoded); err != nil {
				// Canonical escaping can exceed the wire limit even when the
				// incoming UTF-8 representation fitted. Never emit it partly.
				if first.Len() != 0 {
					t.Fatal("failed request write emitted partial bytes")
				}
				if !strings.HasPrefix(err.Error(), "protocol message exceeds ") {
					t.Fatalf("admitted request failed to encode: %v", err)
				}
				return
			}
			again, err := ReadRequest(bytes.NewReader(first.Bytes()))
			if err != nil {
				t.Fatalf("writer produced unreadable request: %v", err)
			}
			if err := WriteRequest(&second, again); err != nil {
				t.Fatal(err)
			}
		} else {
			decoded, err := ReadResponse(bytes.NewReader(data), request)
			var failure *DiagnosticError
			isFailure := errors.As(err, &failure)
			if err != nil && !isFailure {
				return
			}
			if isFailure && !decoded.Tokens.IsEmpty() {
				t.Fatal("failed response exposed tokens")
			}
			if err := WriteResponse(&first, request, decoded); err != nil {
				// The canonical reference spelling can include more explicit
				// position data; the writer independently enforces its limit.
				if first.Len() != 0 {
					t.Fatal("failed response write emitted partial bytes")
				}
				if !strings.HasPrefix(err.Error(), "protocol message exceeds ") {
					t.Fatalf("admitted response failed to encode: %v", err)
				}
				return
			}
			again, err := ReadResponse(bytes.NewReader(first.Bytes()), request)
			if errors.As(err, &failure) != isFailure || err != nil && !isFailure {
				t.Fatalf("writer changed response failure status: %v", err)
			}
			if err := WriteResponse(&second, request, again); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatal("canonical protocol encoding did not stabilize")
		}
	})
}
