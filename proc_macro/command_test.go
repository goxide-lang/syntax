package proc_macro_test

import (
	"bytes"
	"context"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/token"
)

type commandInvocation struct {
	name    string
	request proc_macro.Request
	payload []byte
}

func commandRequest(t *testing.T, name string, kind proc_macro.Kind, source string) commandInvocation {
	t.Helper()
	scope := token.NewScope("command-" + name)
	def, call := scope.NewContext(), scope.NewContext()
	defSpan, err := scope.NewSpan(10, 20)
	if err != nil {
		t.Fatal(err)
	}
	callSpan, err := scope.NewSpan(100, 200)
	if err != nil {
		t.Fatal(err)
	}
	input, err := token.ParseText(source, call)
	if err != nil {
		t.Fatal(err)
	}
	config := proc_macro.RequestConfig{
		Entry: name, Kind: kind, Position: proc_macro.ItemsPosition,
		DefSite: def, CallSite: call, DefSpan: defSpan, CallSpan: callSpan,
		Target:   proc_macro.Target{OS: "linux", Arch: "arm64"},
		Features: []string{"beta", "alpha"},
	}
	switch kind {
	case proc_macro.Function:
		config.Position, config.Input = proc_macro.ExpressionPosition, input
	case proc_macro.Attribute:
		config.Item = input
		config.Args, err = token.ParseText("keep", call)
		if err != nil {
			t.Fatal(err)
		}
	case proc_macro.Derive:
		config.Item = input
	}
	request, err := proc_macro.NewRequest(scope, config)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := proc_macro.WriteRequest(&encoded, request); err != nil {
		t.Fatal(err)
	}
	return commandInvocation{name: name, request: request, payload: encoded.Bytes()}
}

func commandEnvironment(goos, goarch string) []string {
	var result []string
	for _, variable := range os.Environ() {
		key, _, _ := strings.Cut(variable, "=")
		switch key {
		case "GOOS", "GOARCH", "GOTOOLCHAIN", "GOWORK", "CGO_ENABLED":
			continue
		}
		result = append(result, variable)
	}
	return append(result, "GOOS="+goos, "GOARCH="+goarch, "GOTOOLCHAIN=local", "GOWORK=off", "CGO_ENABLED=0")
}

func commandGo(t *testing.T, directory string, environment []string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", args...)
	command.Dir, command.Env = directory, environment
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}

func commandRun(t *testing.T, executable string, args []string, input []byte) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("macro process: %v\nstderr: %s\nstdout: %s", err, stderr.Bytes(), output)
	}
	if stderr.Len() != 0 {
		t.Fatalf("successful macro wrote unexpected stderr: %s", stderr.Bytes())
	}
	return output
}

func checkInputReferences(t *testing.T, got, want token.TokenStream) {
	t.Helper()
	actual, expected := got.Tokens(), want.Tokens()
	if len(actual) != len(expected) {
		t.Fatalf("returned %d tokens, want %d", len(actual), len(expected))
	}
	for i, node := range actual {
		original := expected[i]
		open, close := node.DelimiterSpans()
		wantOpen, wantClose := original.DelimiterSpans()
		if node.Reference() == 0 || node.Reference() != original.Reference() ||
			node.Context() != original.Context() || node.Span() != original.Span() ||
			open != wantOpen || close != wantClose || node.Kind() != original.Kind() ||
			node.Text() != original.Text() || node.Raw() != original.Raw() ||
			node.LiteralType() != original.LiteralType() || node.Delimiter() != original.Delimiter() ||
			node.Implicit() != original.Implicit() {
			t.Fatalf("input token %d changed identity, context, location or syntax", i)
		}
		checkInputReferences(t, node.Children(), original.Children())
	}
}

func checkGeneratedContexts(t *testing.T, stream token.TokenStream, context token.BindingContext) {
	t.Helper()
	for _, node := range stream.Tokens() {
		if node.Reference() != 0 || node.Context() != context {
			t.Fatal("derive output retained an input reference or lost its definition context")
		}
		checkGeneratedContexts(t, node.Children(), context)
	}
}

func TestCommandRoundTrip(t *testing.T) {
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(t.TempDir(), "macro")
	if runtime.GOOS == "windows" {
		native += ".exe"
	}
	nativeEnvironment := commandEnvironment(runtime.GOOS, runtime.GOARCH)
	commandGo(t, directory, nativeEnvironment, "build", "-o", native, "./testdata/command")
	invocations := []commandInvocation{
		commandRequest(t, "identity", proc_macro.Function, "r#match + (世界[2])"),
		commandRequest(t, "keep", proc_macro.Attribute, "type Kept struct { Value int }"),
		commandRequest(t, "Answer", proc_macro.Derive, "type Subject struct { Value int }"),
	}
	responses := make(map[string][]byte)
	var derived token.TokenStream
	for _, invocation := range invocations {
		t.Run(invocation.name, func(t *testing.T) {
			first := commandRun(t, native, nil, invocation.payload)
			second := commandRun(t, native, nil, invocation.payload)
			if !bytes.Equal(first, second) {
				t.Fatal("identical requests produced different response bytes")
			}
			responses[invocation.name] = first
			output, err := proc_macro.ReadResponse(bytes.NewReader(first), invocation.request)
			if err != nil {
				t.Fatal(err)
			}
			if len(output.Diagnostics) != 0 {
				t.Fatalf("successful fixture returned diagnostics: %v", output.Diagnostics)
			}
			switch invocation.request.Kind() {
			case proc_macro.Function:
				checkInputReferences(t, output.Tokens, invocation.request.Input())
			case proc_macro.Attribute:
				checkInputReferences(t, output.Tokens, invocation.request.Item())
			case proc_macro.Derive:
				if output.Tokens.IsEmpty() {
					t.Fatal("derive emitted no tokens")
				}
				checkGeneratedContexts(t, output.Tokens, invocation.request.Context().DefSite())
				derived = output.Tokens
			}
		})
	}
	t.Run("generated Go consumer", func(t *testing.T) {
		if derived.IsEmpty() {
			t.Fatal("derive response is required for the consumer check")
		}
		consumer := t.TempDir()
		source := "package main\n\nimport \"fmt\"\n\n" + derived.Display() + "\nfunc main() { fmt.Println(Answer()) }\n"
		formatted, err := format.Source([]byte(source))
		if err != nil {
			t.Fatalf("generated ordinary Go cannot be formatted: %v\n%s", err, source)
		}
		files := map[string]string{
			"go.mod":  "module example.test/macro-consumer\n\ngo 1.27\n",
			"main.go": string(formatted),
			"main_test.go": `package main
import "fmt"
func ExampleAnswer() {
	fmt.Println(Answer())
	// Output: 42
}
`,
		}
		for name, contents := range files {
			if err := os.WriteFile(filepath.Join(consumer, name), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		binary := filepath.Join(consumer, "consumer")
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		commandGo(t, consumer, nativeEnvironment, "build", "-o", binary, ".")
		if output := commandGo(t, consumer, nativeEnvironment, "run", "."); string(output) != "42\n" {
			t.Fatalf("generated consumer printed %q, want 42", output)
		}
		commandGo(t, consumer, nativeEnvironment, "test", "-count=1", ".")
		t.Logf("generated ordinary Go:\n%s", formatted)
	})
	t.Run("WASI command transport", func(t *testing.T) {
		// This verifies the command/transport mechanism, not an untrusted-code
		// sandbox or the compiler's eventual restricted execution host.
		if os.Getenv("GOXIDE_TEST_WASI") != "1" {
			t.Skip("set GOXIDE_TEST_WASI=1 to compare native and Node WASI command responses")
		}
		node, err := exec.LookPath("node")
		if err != nil {
			t.Fatal("GOXIDE_TEST_WASI=1 requires Node.js with node:wasi")
		}
		temporary := t.TempDir()
		wasm := filepath.Join(temporary, "macro.wasm")
		commandGo(t, directory, commandEnvironment("wasip1", "wasm"), "build", "-o", wasm, "./testdata/command")
		runner := filepath.Join(temporary, "run-wasi.cjs")
		script := `const { readFileSync } = require("node:fs");
const { WASI } = require("node:wasi");
(async () => {
	const wasi = new WASI({
		version: "preview1",
		args: ["macro.wasm"],
		env: {},
		preopens: {},
		returnOnExit: true,
	});
	const module = await WebAssembly.compile(readFileSync(process.argv[2]));
	const instance = await WebAssembly.instantiate(module, wasi.getImportObject());
	process.exitCode = wasi.start(instance);
})().catch(error => {
	console.error(error);
	process.exitCode = 1;
});
`
		if err := os.WriteFile(runner, []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, invocation := range invocations {
			t.Run(invocation.name, func(t *testing.T) {
				first := commandRun(t, node, []string{"--no-warnings", runner, wasm}, invocation.payload)
				second := commandRun(t, node, []string{"--no-warnings", runner, wasm}, invocation.payload)
				if !bytes.Equal(first, second) || !bytes.Equal(first, responses[invocation.name]) {
					t.Fatal("WASI responses differ between identical runs or from the native command")
				}
				if _, err := proc_macro.ReadResponse(bytes.NewReader(first), invocation.request); err != nil {
					t.Fatalf("host rejected WASI response: %v", err)
				}
			})
		}
	})
}
