package lex

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Diagnostic struct {
	Start, End uint32
	Message    string
}

type delimiter struct {
	kind Kind
	attr bool
}
type scanner struct {
	src               []byte
	pos               int
	tokens            []Token
	diagnostics       []Diagnostic
	sourceDiagnostics []Diagnostic
	limitDiagnostic   *Diagnostic
	maxTokens         int
	stopped           bool
	semi              bool
	delims            []delimiter
	attrPrefix        int
}

// Lex includes comments, inserted semicolons, and a final EOF token. It always
// makes progress after malformed input and reports byte-based diagnostics.
func Lex(src []byte) ([]Token, []Diagnostic) {
	return LexLimited(src, 0)
}

// LexLimited stops when another token would exceed maxTokens. Comments,
// inserted semicolons, and EOF each count as a token. It also caps ordinary
// diagnostics at maxTokens; one extra diagnostic reports an exceeded budget.
// A truncated result need not end with EOF. Nonpositive limits disable budgets.
func LexLimited(src []byte, maxTokens int) ([]Token, []Diagnostic) {
	s := scanner{src: src, maxTokens: maxTokens}
	if maxTokens <= 0 {
		s.validateSource(0, len(src))
	}
	if len(src) >= 3 && string(src[:3]) == "\xef\xbb\xbf" {
		s.pos = 3
	}
	for s.pos < len(s.src) && !s.stopped {
		if maxTokens > 0 && len(s.tokens) >= maxTokens {
			s.stop(s.pos, s.pos, "token count budget exceeded")
			break
		}
		start := s.pos
		s.next()
		if maxTokens > 0 {
			s.validateSource(start, s.pos)
		}
	}
	if !s.stopped && s.semi {
		s.insertSemi(s.pos)
	}
	if !s.stopped {
		s.appendToken(Token{Kind: EOF, Start: uint32(s.pos), End: uint32(s.pos)})
	}
	diagnostics := append(s.sourceDiagnostics, s.diagnostics...)
	if s.limitDiagnostic != nil {
		diagnostics = append(diagnostics, *s.limitDiagnostic)
	}
	return s.tokens, diagnostics
}

func (s *scanner) validateSource(start, end int) {
	for p := start; p < end && !s.stopped; {
		r, n := utf8.DecodeRune(s.src[p:])
		if r == utf8.RuneError && n == 1 {
			s.sourceError(p, p+1, "invalid UTF-8 encoding")
		}
		if r == 0 {
			s.sourceError(p, p+n, "NUL is not permitted in source")
		}
		if r == '\ufeff' && p != 0 {
			s.sourceError(p, p+n, "byte order mark is only permitted at the start of a file")
		}
		p += n
	}
}

func (s *scanner) error(start, end int, message string) {
	s.addDiagnostic(&s.diagnostics, start, end, message)
}

func (s *scanner) sourceError(start, end int, message string) {
	s.addDiagnostic(&s.sourceDiagnostics, start, end, message)
}

func (s *scanner) addDiagnostic(target *[]Diagnostic, start, end int, message string) {
	if s.stopped {
		return
	}
	if s.maxTokens > 0 && len(s.diagnostics)+len(s.sourceDiagnostics) >= s.maxTokens {
		s.stop(start, end, "diagnostic count budget exceeded")
		return
	}
	*target = append(*target, Diagnostic{Start: uint32(start), End: uint32(end), Message: message})
}

func (s *scanner) stop(start, end int, message string) {
	if !s.stopped {
		s.stopped = true
		s.limitDiagnostic = &Diagnostic{Start: uint32(start), End: uint32(end), Message: message}
	}
}

func (s *scanner) appendToken(t Token) {
	if s.stopped {
		return
	}
	if s.maxTokens > 0 && len(s.tokens) >= s.maxTokens {
		s.stop(int(t.Start), int(t.End), "token count budget exceeded")
		return
	}
	s.tokens = append(s.tokens, t)
}

func (s *scanner) insertSemi(pos int) {
	s.appendToken(Token{Kind: SEMICOLON, Text: ";", Start: uint32(pos), End: uint32(pos), Implicit: true})
	s.semi = false
}

func (s *scanner) emit(kind Kind, start int, raw bool) {
	if s.stopped {
		return
	}
	if s.maxTokens > 0 && len(s.tokens) >= s.maxTokens {
		s.stop(start, s.pos, "token count budget exceeded")
		return
	}
	text := string(s.src[start:s.pos])
	if raw {
		text = text[2:]
	}
	s.appendToken(Token{Kind: kind, Text: text, Start: uint32(start), End: uint32(s.pos), Raw: raw})
	if kind == COMMENT {
		return
	}
	attrClose := false
	switch kind {
	case LPAREN, LBRACK, LBRACE:
		s.delims = append(s.delims, delimiter{kind: kind, attr: kind == LBRACK && s.attrPrefix != 0})
	case RPAREN, RBRACK, RBRACE:
		if n := len(s.delims); n > 0 {
			d := s.delims[n-1]
			if (d.kind == LPAREN && kind == RPAREN) || (d.kind == LBRACK && kind == RBRACK) || (d.kind == LBRACE && kind == RBRACE) {
				attrClose = d.attr
				s.delims = s.delims[:n-1]
			}
		}
	}
	if kind == HASH {
		s.attrPrefix = 1
	} else if kind == NOT && s.attrPrefix == 1 {
		s.attrPrefix = 2
	} else {
		s.attrPrefix = 0
	}
	s.semi = kind.IsLiteral() || kind == BREAK || kind == CONTINUE || kind == FALLTHROUGH || kind == RETURN || kind == INC || kind == DEC || kind == RPAREN || kind == RBRACK || kind == RBRACE || kind == QUESTION || kind == SELF
	if attrClose {
		s.semi = false
	}
}

func (s *scanner) next() {
	start := s.pos
	c := s.src[s.pos]
	switch c {
	case ' ', '\t', '\r':
		s.pos++
		return
	case '\n':
		if s.semi {
			s.insertSemi(s.pos)
		}
		s.pos++
		return
	}
	r, n := utf8.DecodeRune(s.src[s.pos:])
	if isLetter(r) {
		raw := c == 'r' && s.pos+2 < len(s.src) && s.src[s.pos+1] == '#' && startsIdent(s.src[s.pos+2:])
		if raw {
			s.pos += 2
			_, n = utf8.DecodeRune(s.src[s.pos:])
		}
		s.pos += n
		for s.pos < len(s.src) {
			r, n = utf8.DecodeRune(s.src[s.pos:])
			if !isLetter(r) && !unicode.IsDigit(r) {
				break
			}
			s.pos += n
		}
		kind := IDENT
		if !raw {
			kind = Lookup(string(s.src[start:s.pos]))
		}
		s.emit(kind, start, raw)
		return
	}
	if isDigit(c) || c == '.' && s.pos+1 < len(s.src) && isDigit(s.src[s.pos+1]) {
		kind := s.number()
		s.emit(kind, start, false)
		return
	}
	if c == '"' || c == '\'' || c == '`' {
		s.quoted(c)
		kind := STRING
		if c == '\'' {
			kind = CHAR
		}
		s.emit(kind, start, false)
		return
	}
	if c == '/' && s.pos+1 < len(s.src) {
		next := s.src[s.pos+1]
		if next == '/' {
			s.pos += 2
			for s.pos < len(s.src) && s.src[s.pos] != '\n' {
				s.pos++
			}
			s.emit(COMMENT, start, false)
			return
		}
		if next == '*' {
			s.pos += 2
			newline := -1
			closed := false
			for s.pos < len(s.src) {
				if s.src[s.pos] == '\n' && newline < 0 {
					newline = s.pos
				}
				if s.src[s.pos] == '*' && s.pos+1 < len(s.src) && s.src[s.pos+1] == '/' {
					s.pos += 2
					closed = true
					break
				}
				s.pos++
			}
			if !closed {
				s.error(start, s.pos, "unterminated block comment")
			}
			s.emit(COMMENT, start, false)
			if newline >= 0 && s.semi {
				s.insertSemi(newline)
			}
			return
		}
	}
	for width := 3; width >= 1; width-- {
		if s.pos+width > len(s.src) {
			continue
		}
		if kind, ok := operators[string(s.src[s.pos:s.pos+width])]; ok {
			s.pos += width
			s.emit(kind, start, false)
			return
		}
	}
	s.pos += n
	s.error(start, s.pos, "unexpected character "+strconv.QuoteRune(r))
	s.emit(ILLEGAL, start, false)
}

var operators = func() map[string]Kind {
	m := make(map[string]Kind)
	for k := ADD; k <= DOLLAR; k++ {
		m[k.String()] = k
	}
	return m
}()

func isLetter(r rune) bool        { return r == '_' || unicode.IsLetter(r) }
func startsIdent(src []byte) bool { r, _ := utf8.DecodeRune(src); return isLetter(r) }
func isDigit(c byte) bool         { return c >= '0' && c <= '9' }
func hexDigit(c byte) bool        { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func (s *scanner) quoted(quote byte) {
	start := s.pos
	s.pos++
	closed := false
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		if c == quote {
			s.pos++
			closed = true
			break
		}
		if c == '\n' && quote != '`' {
			break
		}
		if c == '\\' && quote != '`' {
			s.pos++
			if s.pos >= len(s.src) || s.src[s.pos] == '\n' {
				break
			}
		}
		_, n := utf8.DecodeRune(s.src[s.pos:])
		s.pos += n
	}
	if !closed {
		s.error(start, s.pos, "unterminated literal")
		return
	}
	value, err := strconv.Unquote(string(s.src[start:s.pos]))
	if err != nil || quote == '\'' && utf8.RuneCountInString(value) != 1 {
		s.error(start, s.pos, "invalid string or rune literal")
	}
}

func (s *scanner) number() Kind {
	start := s.pos
	base := 10
	prefix := false
	legacyOctal := false
	digits := 0
	invalidDigit := false
	if s.src[s.pos] == '0' {
		legacyOctal = true
		if s.pos+1 < len(s.src) {
			switch s.src[s.pos+1] {
			case 'x', 'X':
				base = 16
				prefix = true
			case 'o', 'O':
				base = 8
				prefix = true
			case 'b', 'B':
				base = 2
				prefix = true
			}
		}
		if prefix {
			s.pos += 2
			legacyOctal = false
		}
	}
	scanDigits := func() {
		for s.pos < len(s.src) {
			c := s.src[s.pos]
			if c == '_' {
				s.pos++
				continue
			}
			if !isDigit(c) && !(base == 16 && hexDigit(c)) {
				break
			}
			if isDigit(c) && int(c-'0') >= base {
				invalidDigit = true
			}
			digits++
			s.pos++
		}
	}
	scanDigits()
	fraction := false
	if s.pos < len(s.src) && s.src[s.pos] == '.' {
		fraction = true
		s.pos++
		scanDigits()
	}
	exponent := byte(0)
	if s.pos < len(s.src) {
		c := s.src[s.pos]
		if c == 'e' || c == 'E' || c == 'p' || c == 'P' {
			exponent = c
			s.pos++
			if s.pos < len(s.src) && (s.src[s.pos] == '+' || s.src[s.pos] == '-') {
				s.pos++
			}
			expDigits := 0
			for s.pos < len(s.src) && (isDigit(s.src[s.pos]) || s.src[s.pos] == '_') {
				if s.src[s.pos] != '_' {
					expDigits++
				}
				s.pos++
			}
			if expDigits == 0 {
				s.error(start, s.pos, "exponent has no digits")
			}
		}
	}
	kind := INT
	if fraction || exponent != 0 {
		kind = FLOAT
	}
	if s.pos < len(s.src) && s.src[s.pos] == 'i' {
		kind = IMAG
		s.pos++
	}
	if digits == 0 {
		s.error(start, s.pos, "numeric literal has no digits")
	}
	if legacyOctal && kind == INT {
		for _, c := range s.src[start:s.pos] {
			if c == '8' || c == '9' {
				invalidDigit = true
			}
		}
	}
	if invalidDigit {
		s.error(start, s.pos, "invalid digit in numeric literal")
	}
	if fraction && base != 10 && base != 16 {
		s.error(start, s.pos, "radix point requires decimal or hexadecimal mantissa")
	}
	if (exponent == 'e' || exponent == 'E') && base != 10 {
		s.error(start, s.pos, "decimal exponent requires decimal mantissa")
	}
	if (exponent == 'p' || exponent == 'P') && base != 16 {
		s.error(start, s.pos, "binary exponent requires hexadecimal mantissa")
	}
	if base == 16 && fraction && exponent == 0 {
		s.error(start, s.pos, "hexadecimal float requires a binary exponent")
	}
	lit := string(s.src[start:s.pos])
	if strings.Contains(lit, "_") && !validUnderscores(lit, prefix, base) {
		s.error(start, s.pos, "underscore must separate successive digits")
	}
	return kind
}

func validUnderscores(lit string, prefix bool, base int) bool {
	inExponent := false
	for i := 0; i < len(lit); i++ {
		c := lit[i]
		if c == 'p' || c == 'P' || base != 16 && (c == 'e' || c == 'E') {
			inExponent = true
		}
		if c != '_' {
			continue
		}
		digit := isDigit
		if base == 16 && !inExponent {
			digit = hexDigit
		}
		before := i > 0 && digit(lit[i-1]) || prefix && i == 2
		after := i+1 < len(lit) && digit(lit[i+1])
		if !before || !after {
			return false
		}
	}
	return true
}
