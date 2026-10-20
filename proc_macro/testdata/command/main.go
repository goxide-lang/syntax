package main

import (
	"fmt"
	"os"

	"github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/token"
)

var entries = []proc_macro.Entry{
	{
		Name: "identity",
		Function: func(_ proc_macro.Context, input token.TokenStream) proc_macro.Output {
			return proc_macro.Success(input)
		},
	},
	{
		Name: "keep",
		Attribute: func(_ proc_macro.Context, _ token.TokenStream, item token.TokenStream) proc_macro.Output {
			return proc_macro.Success(item)
		},
	},
	{
		Name: "Answer",
		Derive: func(ctx proc_macro.Context, _ token.TokenStream) proc_macro.Output {
			return proc_macro.FromParse(token.ParseText("func Answer() int { return 42 }", ctx.DefSite()))
		},
	},
}

func run() error {
	request, err := proc_macro.ReadRequest(os.Stdin)
	if err != nil {
		return err
	}
	output, err := proc_macro.Dispatch(request, entries)
	if err != nil {
		return err
	}
	return proc_macro.WriteResponse(os.Stdout, request, output)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
