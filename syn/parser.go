package syn

import "github.com/goxide-lang/syntax/token"

type cursor struct {
	tokens []token.Token
	pos    int
	depth  int
	end    token.Span
}

func newCursor(input token.TokenStream, depth int) *cursor {
	tokens := input.Tokens()
	p := &cursor{tokens: tokens, depth: depth}
	if len(tokens) != 0 {
		p.end = tokens[len(tokens)-1].Span()
	}
	return p
}

func groupCursor(group token.Token, depth int) *cursor {
	p := newCursor(group.Children(), depth)
	_, p.end = group.DelimiterSpans()
	return p
}

func (p *cursor) eof() bool { return p.pos == len(p.tokens) }
func (p *cursor) peek() token.Token {
	if p.eof() {
		return token.Token{}
	}
	return p.tokens[p.pos]
}
func (p *cursor) at(text string) bool {
	t := p.peek()
	return t.Kind() != token.FragmentKind && !t.Raw() && t.Text() == text
}
func (p *cursor) group(delimiter token.Delimiter) bool {
	t := p.peek()
	return t.Kind() == token.GroupKind && t.Delimiter() == delimiter
}
func (p *cursor) accept(text string) bool {
	if !p.at(text) {
		return false
	}
	p.pos++
	return true
}
func (p *cursor) semis() {
	for !p.eof() && p.peek().Kind() == token.SemicolonKind {
		p.pos++
	}
}
func (p *cursor) stream(start int) token.TokenStream { return token.New(p.tokens[start:p.pos]...) }
func (p *cursor) failure(message string) error {
	span := p.end
	if !p.eof() {
		span = p.peek().Span()
	}
	return &ParseError{Message: message, At: span}
}
func (p *cursor) expect(text string) error {
	if !p.accept(text) {
		return p.failure("expected " + text)
	}
	return nil
}
func (p *cursor) takeGroup(delimiter token.Delimiter) (token.Token, error) {
	if !p.group(delimiter) {
		return token.Token{}, p.failure("expected " + string(delimiter) + " group")
	}
	t := p.peek()
	p.pos++
	return t, nil
}

func identifier(t token.Token) (Ident, bool) {
	view := t
	if t.Kind() == token.FragmentKind && t.FragmentType() == token.IdentFragment {
		children := t.Children().Tokens()
		if len(children) != 1 {
			return Ident{}, false
		}
		view = children[0]
	}
	if view.Kind() != token.IdentKind {
		return Ident{}, false
	}
	return Ident{node: node{token.New(t)}, view: view}, true
}

func (p *cursor) ident() (Ident, error) {
	id, ok := identifier(p.peek())
	if !ok {
		return Ident{}, p.failure("expected identifier")
	}
	p.pos++
	return id, nil
}

func (p *cursor) attributes() ([]Attribute, error) {
	var result []Attribute
	for p.at("#") {
		start := p.pos
		p.pos++
		if p.at("!") {
			return nil, p.failure("inner attributes are not supported on a declaration")
		}
		group, err := p.takeGroup(token.Bracket)
		if err != nil {
			return nil, err
		}
		args := groupCursor(group, p.depth+1)
		if args.peek().Kind() == token.FragmentKind && args.peek().FragmentType() == token.PathFragment {
			args.pos++
		} else if err := args.attributeName(); err != nil {
			return nil, err
		}
		for args.accept("::") {
			if err := args.attributeName(); err != nil {
				return nil, err
			}
		}
		result = append(result, Attribute{
			node: node{p.stream(start)}, path: token.New(args.tokens[:args.pos]...),
			arguments: token.New(args.tokens[args.pos:]...),
		})
	}
	return result, nil
}

func (p *cursor) attributeName() error {
	if p.accept("default") {
		return nil
	}
	_, err := p.ident()
	return err
}

func (p *cursor) visibility() token.TokenStream {
	start := p.pos
	p.accept("pub")
	return p.stream(start)
}

func parseItem(input token.TokenStream, depth int) (Item, error) {
	p := newCursor(input, depth)
	if depth > token.MaxDepth {
		return Item{}, p.failure("syntax nesting budget exceeded")
	}
	p.semis()
	if t := p.peek(); t.Kind() == token.FragmentKind {
		if t.FragmentType() != token.ItemFragment {
			return Item{}, p.failure("expected an item fragment")
		}
		p.pos++
		p.semis()
		if !p.eof() {
			return Item{}, p.failure("expected a single declaration")
		}
		item, err := parseItem(t.Children(), depth+1)
		item.tokens = input
		return item, err
	}
	item := Item{node: node{input}}
	var err error
	item.attributes, err = p.attributes()
	if err != nil {
		return Item{}, err
	}
	item.visibility = p.visibility()
	switch {
	case p.accept("type"):
		item.kind = TypeItem
		if p.group(token.Parenthesis) {
			return Item{}, p.failure("grouped type declarations are not supported; pass a single named declaration")
		}
	case p.accept("struct"):
		item.kind = StructItem
	case p.accept("enum"):
		item.kind = EnumItem
	default:
		return Item{}, p.failure("expected a type, struct or enum declaration")
	}
	item.name, err = p.ident()
	if err != nil {
		return Item{}, err
	}
	genericAlias := p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind() == token.PunctKind && p.tokens[p.pos+1].Text() == "="
	if p.group(token.Bracket) && (item.kind != TypeItem || genericAlias || !arrayLength(p.peek().Children())) {
		item.generics, err = p.generics()
		if err != nil {
			return Item{}, err
		}
	}
	if item.kind != TypeItem {
		item.where, err = p.whereClause(!item.generics.IsEmpty())
		if err != nil {
			return Item{}, err
		}
	}
	switch item.kind {
	case TypeItem:
		item.alias = p.accept("=")
		item.typ, err = p.typ()
		if err == nil {
			item.where, err = p.whereClause(!item.generics.IsEmpty())
		}
	case StructItem:
		start := p.pos
		var body token.Token
		body, err = p.takeGroup(token.Brace)
		if err == nil {
			item.typ = Type{node: node{p.stream(start)}, structType: true}
			item.typ.fields, err = parseFields(body, p.depth+1, false)
		}
	case EnumItem:
		var body token.Token
		body, err = p.takeGroup(token.Brace)
		if err == nil {
			item.variants, err = parseVariants(body, p.depth+1)
		}
	}
	if err != nil {
		return Item{}, err
	}
	p.semis()
	if !p.eof() {
		return Item{}, p.failure("expected a single declaration with no trailing tokens")
	}
	return item, nil
}

func parseFields(body token.Token, depth int, namedVariant bool) ([]Field, error) {
	p := groupCursor(body, depth)
	if depth > token.MaxDepth {
		return nil, p.failure("syntax nesting budget exceeded")
	}
	var fields []Field
	for {
		p.semis()
		if p.eof() {
			return fields, nil
		}
		start := p.pos
		field := Field{}
		var err error
		field.attributes, err = p.attributes()
		if err != nil {
			return nil, err
		}
		if !namedVariant {
			field.visibility = p.visibility()
		}
		var first Type
		if namedVariant || p.namedArrayField() {
			name, err := p.ident()
			if err != nil {
				return nil, err
			}
			field.names = append(field.names, name)
		} else {
			first, err = p.typ()
			if err != nil {
				return nil, err
			}
			if first.bare != nil && (p.at(",") || typeStart(p.peek())) {
				field.names = append(field.names, *first.bare)
			}
		}
		if len(field.names) != 0 {
			for p.accept(",") {
				name, err := p.ident()
				if err != nil {
					return nil, err
				}
				field.names = append(field.names, name)
			}
			field.typ, err = p.typ()
			if err != nil {
				return nil, err
			}
		} else {
			field.typ = first
		}
		if !namedVariant && p.peek().Kind() == token.LiteralKind && p.peek().LiteralType() == token.String {
			field.tag, field.hasTag = p.peek(), true
			p.pos++
		}
		field.tokens = p.stream(start)
		fields = append(fields, field)
		if namedVariant && p.accept(",") {
			continue
		}
		if !p.eof() && p.peek().Kind() != token.SemicolonKind {
			return nil, p.failure("expected a separator after field")
		}
	}
}

func parseVariants(body token.Token, depth int) ([]Variant, error) {
	p := groupCursor(body, depth)
	if depth > token.MaxDepth {
		return nil, p.failure("syntax nesting budget exceeded")
	}
	var variants []Variant
	for !p.eof() {
		start := p.pos
		variant := Variant{kind: UnitVariant}
		var err error
		variant.attributes, err = p.attributes()
		if err != nil {
			return nil, err
		}
		variant.name, err = p.ident()
		if err != nil {
			return nil, err
		}
		if p.group(token.Parenthesis) {
			variant.kind = TupleVariant
			variant.fields, err = parseTuple(p.peek(), depth+1)
			p.pos++
		} else if p.group(token.Brace) {
			variant.kind = StructVariant
			variant.fields, err = parseFields(p.peek(), depth+1, true)
			p.pos++
		}
		if err != nil {
			return nil, err
		}
		variant.tokens = p.stream(start)
		variants = append(variants, variant)
		if !p.accept(",") && !p.eof() {
			return nil, p.failure("expected comma between enum variants")
		}
	}
	return variants, nil
}

func parseTuple(body token.Token, depth int) ([]Field, error) {
	p := groupCursor(body, depth)
	var fields []Field
	for !p.eof() {
		start := p.pos
		field := Field{}
		var err error
		field.attributes, err = p.attributes()
		if err == nil {
			field.typ, err = p.typ()
		}
		if err != nil {
			return nil, err
		}
		field.tokens = p.stream(start)
		fields = append(fields, field)
		if !p.accept(",") && !p.eof() {
			return nil, p.failure("expected comma between tuple fields")
		}
	}
	return fields, nil
}
