package kry

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type TokenKind int

const (
	EOF TokenKind = iota
	ID
	INT
	FLOAT
	STRING
	FN
	LET
	MUT
	IF
	ELSE
	WHILE
	RETURN
	BREAK
	CONTINUE
	TRUE
	FALSE
	NIL
	PUB
	IMPORT
	STRUCT
	ENUM
	MATCH
	FOR
	IN
	DEFER
	UNSAFE
	IMPL
	LPAREN
	RPAREN
	LBRACE
	RBRACE
	LBRACKET
	RBRACKET
	COMMA
	COLON
	SEMICOLON
	ARROW
	FATARROW
	DCOLON
	DOT
	PLUS
	MINUS
	STAR
	SLASH
	PERCENT
	BANG
	EQUAL
	EQEQ
	NEQ
	LESS
	LEQ
	GREATER
	GEQ
	AND
	OR
	QUESTION
	PIPE
)

type Token struct {
	Kind          TokenKind
	Start, Length int
	Line, Column  int
	Source        *Source
}

var words = map[string]TokenKind{"fn": FN, "let": LET, "mut": MUT, "if": IF, "else": ELSE, "while": WHILE, "return": RETURN, "break": BREAK, "continue": CONTINUE, "true": TRUE, "false": FALSE, "nil": NIL, "pub": PUB, "import": IMPORT, "struct": STRUCT, "enum": ENUM, "match": MATCH, "for": FOR, "in": IN, "defer": DEFER, "unsafe": UNSAFE, "impl": IMPL}

func validUTF8(b []byte) bool { return utf8.Valid(b) }
func isIDStart(r rune) bool   { return r == '_' || unicode.IsLetter(r) || r >= utf8.RuneSelf }
func isIDPart(r rune) bool    { return isIDStart(r) || unicode.IsDigit(r) }
func (t Token) Text() string  { return t.Source.Text[t.Start : t.Start+t.Length] }
func Lex(src *Source, lim Limits) ([]Token, *Diagnostic) {
	if len(src.Text) > lim.MaxSourceBytes {
		return nil, Diag(CatResource, src, 1, 1, "source exceeds configured input size limit")
	}
	if !validUTF8([]byte(src.Text)) {
		return nil, Diag(CatLex, src, 1, 1, "source is not valid UTF-8")
	}
	var out []Token
	i, line, col := 0, 1, 1
	depth := 0
	push := func(k TokenKind, s, l, ln, cl int) {
		if len(out) >= lim.MaxTokens {
			return
		}
		out = append(out, Token{k, s, l, ln, cl, src})
	}
	for i < len(src.Text) {
		if len(out) >= lim.MaxTokens {
			return nil, Diag(CatResource, src, line, col, "token limit exceeded (%d)", lim.MaxTokens)
		}
		r, w := utf8.DecodeRuneInString(src.Text[i:])
		if r == utf8.RuneError && w == 1 {
			return nil, Diag(CatLex, src, line, col, "invalid UTF-8 sequence")
		}
		if r == ' ' || r == '\t' || r == '\r' {
			i += w
			col++
			continue
		}
		if r == '\n' {
			i += w
			line++
			col = 1
			continue
		}
		if r == '/' && i+1 < len(src.Text) && src.Text[i+1] == '/' {
			i += 2
			col += 2
			for i < len(src.Text) && src.Text[i] != '\n' {
				_, ww := utf8.DecodeRuneInString(src.Text[i:])
				i += ww
				col++
			}
			continue
		}
		if r == '/' && i+1 < len(src.Text) && src.Text[i+1] == '*' {
			startLine, startCol := line, col
			i += 2
			col += 2
			depth = 1
			for i < len(src.Text) && depth > 0 {
				if i+1 < len(src.Text) && src.Text[i:i+2] == "/*" {
					depth++
					if depth > lim.MaxNesting {
						return nil, Diag(CatResource, src, line, col, "comment nesting limit exceeded")
					}
					i += 2
					col += 2
				} else if i+1 < len(src.Text) && src.Text[i:i+2] == "*/" {
					depth--
					i += 2
					col += 2
				} else {
					rr, ww := utf8.DecodeRuneInString(src.Text[i:])
					i += ww
					if rr == '\n' {
						line++
						col = 1
					} else {
						col++
					}
				}
			}
			if depth > 0 {
				return nil, Diag(CatLex, src, startLine, startCol, "unterminated block comment")
			}
			continue
		}
		ln, cl := line, col
		if isIDStart(r) {
			s := i
			i += w
			col++
			for i < len(src.Text) {
				rr, ww := utf8.DecodeRuneInString(src.Text[i:])
				if !isIDPart(rr) {
					break
				}
				i += ww
				col++
			}
			text := src.Text[s:i]
			k := ID
			if x, ok := words[text]; ok {
				k = x
			}
			push(k, s, i-s, ln, cl)
			continue
		}
		if unicode.IsDigit(r) {
			s := i
			i += w
			col++
			for i < len(src.Text) {
				rr, ww := utf8.DecodeRuneInString(src.Text[i:])
				if !unicode.IsDigit(rr) {
					break
				}
				i += ww
				col++
			}
			k := INT
			if i+1 < len(src.Text) && src.Text[i] == '.' && unicode.IsDigit(rune(src.Text[i+1])) {
				k = FLOAT
				i++
				col++
				for i < len(src.Text) && unicode.IsDigit(rune(src.Text[i])) {
					i++
					col++
				}
			}
			push(k, s, i-s, ln, cl)
			continue
		}
		if r == '"' {
			s := i
			i += w
			col++
			closed := false
			for i < len(src.Text) {
				rr, ww := utf8.DecodeRuneInString(src.Text[i:])
				if rr == '\n' {
					return nil, Diag(CatLex, src, line, col, "newline in string literal")
				}
				if rr == '"' {
					i += ww
					col++
					closed = true
					break
				}
				if rr == '\\' {
					if i+1 >= len(src.Text) {
						break
					}
					n := src.Text[i+1]
					if n == 'x' {
						if i+3 >= len(src.Text) || !isHex(src.Text[i+2]) || !isHex(src.Text[i+3]) {
							return nil, Diag(CatLex, src, ln, cl, "invalid hex escape in string literal")
						}
						i += 4
						col += 4
					} else {
						if !strings.ContainsRune("nrt\\\"", rune(n)) {
							return nil, Diag(CatLex, src, ln, cl, "unsupported escape sequence")
						}
						i += 2
						col += 2
					}
				} else {
					i += ww
					col++
				}
			}
			if !closed {
				return nil, Diag(CatLex, src, ln, cl, "unterminated string literal")
			}
			push(STRING, s, i-s, ln, cl)
			continue
		}
		k, wid := EOF, 1
		if i+1 < len(src.Text) {
			two := src.Text[i : i+2]
			switch two {
			case "->":
				k = ARROW
				wid = 2
			case "=>":
				k = FATARROW
				wid = 2
			case "::":
				k = DCOLON
				wid = 2
			case "==":
				k = EQEQ
				wid = 2
			case "!=":
				k = NEQ
				wid = 2
			case "<=":
				k = LEQ
				wid = 2
			case ">=":
				k = GEQ
				wid = 2
			case "&&":
				k = AND
				wid = 2
			case "||":
				k = OR
				wid = 2
			}
		}
		if k == EOF {
			switch r {
			case '(':
				k = LPAREN
			case ')':
				k = RPAREN
			case '{':
				k = LBRACE
			case '}':
				k = RBRACE
			case '[':
				k = LBRACKET
			case ']':
				k = RBRACKET
			case ',':
				k = COMMA
			case ':':
				k = COLON
			case ';':
				k = SEMICOLON
			case '.':
				k = DOT
			case '+':
				k = PLUS
			case '-':
				k = MINUS
			case '*':
				k = STAR
			case '/':
				k = SLASH
			case '%':
				k = PERCENT
			case '!':
				k = BANG
			case '=':
				k = EQUAL
			case '<':
				k = LESS
			case '>':
				k = GREATER
			case '?':
				k = QUESTION
			case '|':
				k = PIPE

			default:
				return nil, Diag(CatLex, src, ln, cl, "unexpected character %q", r)
			}
		}
		push(k, i, wid, ln, cl)
		i += wid
		col += wid
	}
	push(EOF, len(src.Text), 0, line, col)
	return out, nil
}
func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}
func DecodeString(t Token) (string, error) {
	raw := t.Text()
	if len(raw) < 2 {
		return "", strconv.ErrSyntax
	}
	var b strings.Builder
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			b.WriteByte(raw[i])
			continue
		}
		i++
		switch raw[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case 'x':
			if i+2 >= len(raw)-1 {
				return "", strconv.ErrSyntax
			}
			v, err := strconv.ParseUint(raw[i+1:i+3], 16, 8)
			if err != nil {
				return "", err
			}
			b.WriteByte(byte(v))
			i += 2
		default:
			return "", strconv.ErrSyntax
		}
	}
	s := b.String()
	if !validUTF8([]byte(s)) {
		return "", strconv.ErrSyntax
	}
	return s, nil
}
