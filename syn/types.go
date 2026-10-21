package syn

import "github.com/goxide-lang/syntax/token"

func typeStart(t token.Token) bool {
	if t.Kind() == token.IdentKind {
		return true
	}
	if t.Kind() == token.FragmentKind {
		return t.FragmentType() == token.TypeFragment || t.FragmentType() == token.PathFragment || t.FragmentType() == token.IdentFragment
	}
	if t.Kind() == token.GroupKind {
		return t.Delimiter() == token.Bracket || t.Delimiter() == token.Parenthesis
	}
	switch t.Text() {
	case "Self", "*", "&", "~", "map", "chan", "<-", "func", "struct", "interface":
		return true
	}
	return false
}

func (p *cursor) typ() (Type, error) {
	start := p.pos
	typ, err := p.typeAtom()
	if err != nil {
		return Type{}, err
	}
	if p.at("|") {
		typ = Type{}
		for p.accept("|") {
			if _, err := p.typeAtom(); err != nil {
				return Type{}, err
			}
		}
	}
	typ.tokens = p.stream(start)
	return typ, nil
}

func (p *cursor) typeAtom() (Type, error) {
	if p.depth >= token.MaxDepth {
		return Type{}, p.failure("syntax nesting budget exceeded")
	}
	p.depth++
	defer func() { p.depth-- }()
	start := p.pos
	typ := Type{}
	t := p.peek()
	switch {
	case t.Kind() == token.FragmentKind:
		if !typeStart(t) {
			return Type{}, p.failure("fragment is not a type, path or identifier")
		}
		if id, ok := identifier(t); ok {
			typ.bare = &id
		}
		p.pos++
	case t.Kind() == token.IdentKind || p.at("Self"):
		if id, ok := identifier(t); ok {
			typ.bare = &id
		}
		p.pos++
		if p.group(token.Bracket) {
			if err := p.typeArgs(); err != nil {
				return Type{}, err
			}
			typ.bare = nil
		}
	case p.at("*") || p.at("&") || p.at("~"):
		p.pos++
		if t.Text() == "&" && p.at("mut") && p.pos+1 < len(p.tokens) && typeStart(p.tokens[p.pos+1]) {
			p.pos++
		}
		if _, err := p.typeAtom(); err != nil {
			return Type{}, err
		}
	case p.group(token.Bracket):
		// The length is an opaque expression. Empty brackets denote a slice.
		p.pos++
		if _, err := p.typeAtom(); err != nil {
			return Type{}, err
		}
	case p.accept("map"):
		key, err := p.takeGroup(token.Bracket)
		if err != nil {
			return Type{}, err
		}
		if err := wholeType(key, p.depth); err != nil {
			return Type{}, err
		}
		if _, err := p.typeAtom(); err != nil {
			return Type{}, err
		}
	case p.accept("chan"):
		p.accept("<-")
		if _, err := p.typeAtom(); err != nil {
			return Type{}, err
		}
	case p.accept("<-"):
		if err := p.expect("chan"); err != nil {
			return Type{}, err
		}
		if _, err := p.typeAtom(); err != nil {
			return Type{}, err
		}
	case p.accept("func"):
		// Parameters and parenthesized results remain token views. Only their
		// boundaries are needed to distinguish the containing field.
		if _, err := p.takeGroup(token.Parenthesis); err != nil {
			return Type{}, err
		}
		if p.group(token.Parenthesis) {
			p.pos++
		} else if typeStart(p.peek()) {
			if _, err := p.typ(); err != nil {
				return Type{}, err
			}
		}
	case p.accept("struct"):
		body, err := p.takeGroup(token.Brace)
		if err != nil {
			return Type{}, err
		}
		typ.structType = true
		typ.fields, err = parseFields(body, p.depth+1, false)
		if err != nil {
			return Type{}, err
		}
	case p.accept("interface"):
		if _, err := p.takeGroup(token.Brace); err != nil {
			return Type{}, err
		}
	case p.group(token.Parenthesis):
		group := p.peek()
		p.pos++
		if err := wholeType(group, p.depth); err != nil {
			return Type{}, err
		}
	default:
		return Type{}, p.failure("expected type")
	}
	for p.at("::") || p.at(".") {
		p.pos++
		if _, err := p.ident(); err != nil {
			return Type{}, err
		}
		if p.group(token.Bracket) {
			if err := p.typeArgs(); err != nil {
				return Type{}, err
			}
		}
		typ = Type{}
	}
	if p.accept("!") {
		if p.peek().Kind() != token.GroupKind {
			return Type{}, p.failure("expected macro argument group")
		}
		p.pos++
		typ = Type{}
	}
	typ.tokens = p.stream(start)
	return typ, nil
}

func wholeType(group token.Token, depth int) error {
	p := groupCursor(group, depth)
	if _, err := p.typ(); err != nil {
		return err
	}
	if !p.eof() {
		return p.failure("unexpected token after type")
	}
	return nil
}

func (p *cursor) typeArgs() error {
	group := p.peek()
	p.pos++
	args := groupCursor(group, p.depth)
	if args.eof() {
		return args.failure("expected a type argument")
	}
	for !args.eof() {
		if _, ok := identifier(args.peek()); ok && args.pos+1 < len(args.tokens) && args.tokens[args.pos+1].Text() == "=" {
			args.pos += 2
		}
		if _, err := args.typ(); err != nil {
			return err
		}
		if !args.accept(",") && !args.eof() {
			return args.failure("expected comma between type arguments")
		}
	}
	return nil
}

func (p *cursor) generics() (token.TokenStream, error) {
	start := p.pos
	group := p.peek()
	p.pos++
	params := groupCursor(group, p.depth+1)
	if params.eof() {
		return token.TokenStream{}, params.failure("expected a generic parameter")
	}
	for !params.eof() {
		if _, err := params.ident(); err != nil {
			return token.TokenStream{}, err
		}
		if typeStart(params.peek()) {
			if _, err := params.typ(); err != nil {
				return token.TokenStream{}, err
			}
		}
		if !params.accept(",") && !params.eof() {
			return token.TokenStream{}, params.failure("expected comma between generic parameters")
		}
	}
	return p.stream(start), nil
}

func (p *cursor) whereClause(generic bool) (token.TokenStream, error) {
	start := p.pos
	if !p.at("where") {
		return token.TokenStream{}, nil
	}
	if !generic {
		return token.TokenStream{}, p.failure("where clause requires generic parameters")
	}
	p.pos++
	for {
		if _, err := p.typeAtom(); err != nil {
			return token.TokenStream{}, err
		}
		if err := p.expect(":"); err != nil {
			return token.TokenStream{}, err
		}
		if _, err := p.typeAtom(); err != nil {
			return token.TokenStream{}, err
		}
		for p.accept("+") {
			if _, err := p.typeAtom(); err != nil {
				return token.TokenStream{}, err
			}
		}
		if !p.accept(",") || p.eof() || p.group(token.Brace) || p.peek().Kind() == token.SemicolonKind {
			break
		}
	}
	return p.stream(start), nil
}

func (p *cursor) namedArrayField() bool {
	if _, ok := identifier(p.peek()); !ok || p.pos+1 >= len(p.tokens) {
		return false
	}
	group := p.tokens[p.pos+1]
	return group.Kind() == token.GroupKind && group.Delimiter() == token.Bracket &&
		(group.Children().IsEmpty() || p.pos+2 < len(p.tokens) && typeStart(p.tokens[p.pos+2]))
}

// In a Go-style type declaration an array length wins over type parameters
// when both readings are possible: type A[N] T is an array, as is A[T * U] V.
// Only a candidate that starts with a parameter name needs this lookahead.
// Expressions stay opaque in the resulting Type; no text round trip is used.
func arrayLength(input token.TokenStream) bool {
	p := newCursor(input, 0)
	if _, ok := identifier(p.peek()); !ok {
		return true
	}
	return p.expression() && p.eof()
}

func (p *cursor) expression() bool {
	if !p.operand() {
		return false
	}
	for {
		switch p.peek().Text() {
		case "+", "-", "*", "/", "%", "&", "|", "^", "&^", "<<", ">>", "==", "!=", "<", "<=", ">", ">=", "&&", "||":
			p.pos++
			if !p.operand() {
				return false
			}
		default:
			return true
		}
	}
}

func (p *cursor) operand() bool {
	if p.depth >= token.MaxDepth || p.eof() {
		return false
	}
	p.depth++
	defer func() { p.depth-- }()
	switch p.peek().Text() {
	case "+", "-", "!", "^", "&", "*", "<-":
		p.pos++
		return p.operand()
	}
	t := p.peek()
	switch {
	case t.Kind() == token.IdentKind || t.Kind() == token.LiteralKind:
		p.pos++
	case t.Kind() == token.FragmentKind && (t.FragmentType() == token.ExprFragment || t.FragmentType() == token.IdentFragment || t.FragmentType() == token.PathFragment):
		p.pos++
	case p.group(token.Parenthesis):
		inner := groupCursor(t, p.depth)
		if !inner.expression() || !inner.eof() {
			return false
		}
		p.pos++
	case p.group(token.Bracket) || p.at("map") || p.at("struct"):
		if _, err := p.typ(); err != nil || !p.group(token.Brace) {
			return false
		}
		p.pos++
	default:
		return false
	}
	for {
		switch {
		case p.accept(".") || p.accept("::"):
			if _, err := p.ident(); err != nil {
				return false
			}
		case p.accept("!"):
			if p.peek().Kind() != token.GroupKind {
				return false
			}
			p.pos++
		case p.peek().Kind() == token.GroupKind:
			p.pos++ // call, index or composite literal; content remains opaque
		case p.accept("?"):
		default:
			return true
		}
	}
}
