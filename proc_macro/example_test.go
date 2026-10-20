package proc_macro_test

import (
	"bytes"
	"fmt"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/token"
)

func Example() {
	scope := token.NewScope("example-invocation")
	host, err := pm.NewRequest(scope, pm.RequestConfig{
		Entry: "answer", Kind: pm.Function, Position: pm.ExpressionPosition,
		DefSite: scope.NewContext(), CallSite: scope.NewContext(),
		Target: pm.Target{OS: "linux", Arch: "arm64"},
	})
	if err != nil {
		panic(err)
	}
	var requestBytes bytes.Buffer
	if err := pm.WriteRequest(&requestBytes, host); err != nil {
		panic(err)
	}

	// A launcher chooses explicit I/O and supplies its static entry table.
	guest, err := pm.ReadRequest(&requestBytes)
	if err != nil {
		panic(err)
	}
	output, err := pm.Dispatch(guest, []pm.Entry{{
		Name: "answer",
		Function: func(ctx pm.Context, _ token.TokenStream) pm.Output {
			number, err := token.NewLiteral(token.Integer, "42", ctx.DefSite())
			if err != nil {
				return pm.Failure(err.Error(), ctx.CallSpan())
			}
			return pm.Success(token.New(number))
		},
	}})
	if err != nil {
		panic(err)
	}
	var responseBytes bytes.Buffer
	if err := pm.WriteResponse(&responseBytes, guest, output); err != nil {
		panic(err)
	}
	result, err := pm.ReadResponse(&responseBytes, host)
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Tokens.Display())
	// Output: 42
}
