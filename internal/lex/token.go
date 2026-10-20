// Package lex implements the lexical vocabulary, tokenization, and automatic
// semicolons of Goxide.
package lex

import "strconv"

type Kind uint16

const (
	ILLEGAL Kind = iota
	EOF
	COMMENT
	IDENT
	INT
	FLOAT
	IMAG
	CHAR
	STRING
	ADD
	SUB
	MUL
	QUO
	REM
	AND
	OR
	XOR
	SHL
	SHR
	AND_NOT
	ADD_ASSIGN
	SUB_ASSIGN
	MUL_ASSIGN
	QUO_ASSIGN
	REM_ASSIGN
	AND_ASSIGN
	OR_ASSIGN
	XOR_ASSIGN
	SHL_ASSIGN
	SHR_ASSIGN
	AND_NOT_ASSIGN
	LAND
	LOR
	ARROW
	INC
	DEC
	EQL
	LSS
	GTR
	ASSIGN
	NOT
	NEQ
	LEQ
	GEQ
	DEFINE
	ELLIPSIS
	LPAREN
	LBRACK
	LBRACE
	COMMA
	PERIOD
	RPAREN
	RBRACK
	RBRACE
	SEMICOLON
	COLON
	DCOLON
	FATARROW
	QUESTION
	HASH
	TILDE
	DOLLAR
	BREAK
	CASE
	CHAN
	CONST
	CONTINUE
	DEFAULT
	DEFER
	ELSE
	FALLTHROUGH
	FOR
	FUNC
	GO
	GOTO
	IF
	IMPORT
	INTERFACE
	MAP
	PACKAGE
	RANGE
	RETURN
	SELECT
	STRUCT
	SWITCH
	TYPE
	VAR
	ENUM
	MATCH
	TRAIT
	IMPL
	USE
	PUB
	MOD
	UNSAFE
	DECLARE
	SELF
)

var spellings = [...]string{
	ILLEGAL: "ILLEGAL", EOF: "EOF", COMMENT: "COMMENT", IDENT: "IDENT", INT: "INT", FLOAT: "FLOAT", IMAG: "IMAG", CHAR: "CHAR", STRING: "STRING",
	ADD: "+", SUB: "-", MUL: "*", QUO: "/", REM: "%", AND: "&", OR: "|", XOR: "^", SHL: "<<", SHR: ">>", AND_NOT: "&^",
	ADD_ASSIGN: "+=", SUB_ASSIGN: "-=", MUL_ASSIGN: "*=", QUO_ASSIGN: "/=", REM_ASSIGN: "%=", AND_ASSIGN: "&=", OR_ASSIGN: "|=", XOR_ASSIGN: "^=", SHL_ASSIGN: "<<=", SHR_ASSIGN: ">>=", AND_NOT_ASSIGN: "&^=",
	LAND: "&&", LOR: "||", ARROW: "<-", INC: "++", DEC: "--", EQL: "==", LSS: "<", GTR: ">", ASSIGN: "=", NOT: "!", NEQ: "!=", LEQ: "<=", GEQ: ">=", DEFINE: ":=", ELLIPSIS: "...",
	LPAREN: "(", LBRACK: "[", LBRACE: "{", COMMA: ",", PERIOD: ".", RPAREN: ")", RBRACK: "]", RBRACE: "}", SEMICOLON: ";", COLON: ":", DCOLON: "::", FATARROW: "=>", QUESTION: "?", HASH: "#", TILDE: "~", DOLLAR: "$",
	BREAK: "break", CASE: "case", CHAN: "chan", CONST: "const", CONTINUE: "continue", DEFAULT: "default", DEFER: "defer", ELSE: "else", FALLTHROUGH: "fallthrough", FOR: "for", FUNC: "func", GO: "go", GOTO: "goto", IF: "if", IMPORT: "import", INTERFACE: "interface", MAP: "map", PACKAGE: "package", RANGE: "range", RETURN: "return", SELECT: "select", STRUCT: "struct", SWITCH: "switch", TYPE: "type", VAR: "var",
	ENUM: "enum", MATCH: "match", TRAIT: "trait", IMPL: "impl", USE: "use", PUB: "pub", MOD: "mod", UNSAFE: "unsafe", DECLARE: "declare", SELF: "Self",
}

// Token offsets are byte offsets in the original source. Raw identifiers store
// the unescaped name in Text; all other tokens retain their original spelling.
type Token struct {
	Kind       Kind
	Text       string
	Start, End uint32
	Implicit   bool
	Raw        bool
}

func (k Kind) String() string {
	if int(k) < len(spellings) {
		return spellings[k]
	}
	return "token(" + strconv.Itoa(int(k)) + ")"
}

func (k Kind) IsLiteral() bool  { return k >= IDENT && k <= STRING }
func (k Kind) IsOperator() bool { return k >= ADD && k <= DOLLAR }
func (k Kind) IsKeyword() bool  { return k >= BREAK && k <= SELF }

// Precedence returns zero for tokens that are not binary operators.
func (k Kind) Precedence() int {
	switch k {
	case LOR:
		return 1
	case LAND:
		return 2
	case EQL, NEQ, LSS, LEQ, GTR, GEQ:
		return 3
	case ADD, SUB, OR, XOR:
		return 4
	case MUL, QUO, REM, SHL, SHR, AND, AND_NOT:
		return 5
	}
	return 0
}

var keywords = func() map[string]Kind {
	m := make(map[string]Kind)
	for k := BREAK; k <= SELF; k++ {
		m[k.String()] = k
	}
	return m
}()

// Lookup intentionally leaves contextual and future-reserved words as IDENT.
func Lookup(name string) Kind {
	if k, ok := keywords[name]; ok {
		return k
	}
	return IDENT
}

func IsHardReserved(name string) bool { return Lookup(name).IsKeyword() }

func IsContextual(name string) bool {
	switch name {
	case "mut", "ref", "as", "where", "self", "super", "in", "macro_rules":
		return true
	}
	return false
}

func IsFutureReserved(name string) bool {
	switch name {
	case "async", "await", "let", "fn", "loop", "while", "macro":
		return true
	}
	return false
}
