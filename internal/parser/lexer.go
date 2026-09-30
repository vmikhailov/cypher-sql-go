package parser

import (
	"fmt"
	"strings"
	"unicode"
)

var keywords = map[string]TokenType{
	"MATCH":    TokenMatch,
	"OPTIONAL": TokenOptional,
	"WHERE":    TokenWhere,
	"WITH":     TokenWith,
	"RETURN":   TokenReturn,
	"ORDER":    TokenOrder,
	"BY":       TokenBy,
	"SKIP":     TokenSkip,
	"OFFSET":   TokenSkip,
	"LIMIT":    TokenLimit,
	"DISTINCT": TokenDistinct,
	"AS":       TokenAs,
	"AND":      TokenAnd,
	"OR":       TokenOr,
	"NOT":      TokenNot,
	"IN":       TokenIn,
	"IS":       TokenIs,
	"NULL":     TokenNull,
	"TRUE":     TokenTrue,
	"FALSE":    TokenFalse,
	"CASE":     TokenCase,
	"WHEN":     TokenWhen,
	"THEN":     TokenThen,
	"ELSE":     TokenElse,
	"END":      TokenEnd,
	"EXISTS":   TokenExists,
	"STARTS":   TokenStarts,
	"ENDS":     TokenEnds,
	"CONTAINS": TokenContains,
	"ASC":      TokenAsc,
	"DESC":     TokenDesc,
}

// Lexer tokenizes Cypher source text.
type Lexer struct {
	input  string
	pos    int
	read   int
	ch     rune
	line   int
	column int
}

// NewLexer creates a new Lexer instance.
func NewLexer(input string) *Lexer {
	l := &Lexer{
		input:  input,
		line:   1,
		column: 0,
	}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.read >= len(l.input) {
		l.ch = 0
		l.pos = l.read
	} else {
		l.ch = rune(l.input[l.read])
		l.pos = l.read
		l.read++
	}
	l.column++
}

func (l *Lexer) peekChar() rune {
	if l.read >= len(l.input) {
		return 0
	}
	return rune(l.input[l.read])
}

// NextToken returns the next token from input.
func (l *Lexer) NextToken() Token {
	l.skipWhitespaceAndComments()

	line := l.line
	col := l.column

	if l.ch == 0 {
		return Token{Type: TokenEOF, Value: "", Line: line, Column: col}
	}

	switch l.ch {
	case '(':
		l.readChar()
		return Token{Type: TokenLParen, Value: "(", Line: line, Column: col}
	case ')':
		l.readChar()
		return Token{Type: TokenRParen, Value: ")", Line: line, Column: col}
	case '[':
		l.readChar()
		return Token{Type: TokenLBracket, Value: "[", Line: line, Column: col}
	case ']':
		l.readChar()
		return Token{Type: TokenRBracket, Value: "]", Line: line, Column: col}
	case '{':
		l.readChar()
		return Token{Type: TokenLBrace, Value: "{", Line: line, Column: col}
	case '}':
		l.readChar()
		return Token{Type: TokenRBrace, Value: "}", Line: line, Column: col}
	case ':':
		l.readChar()
		return Token{Type: TokenColon, Value: ":", Line: line, Column: col}
	case ',':
		l.readChar()
		return Token{Type: TokenComma, Value: ",", Line: line, Column: col}
	case '.':
		if l.peekChar() == '.' {
			// Range .. e.g. *1..3
			l.readChar()
			l.readChar()
			return Token{Type: TokenIdent, Value: "..", Line: line, Column: col}
		}
		l.readChar()
		return Token{Type: TokenDot, Value: ".", Line: line, Column: col}
	case '|':
		l.readChar()
		return Token{Type: TokenPipe, Value: "|", Line: line, Column: col}
	case '+':
		l.readChar()
		return Token{Type: TokenPlus, Value: "+", Line: line, Column: col}
	case '*':
		l.readChar()
		return Token{Type: TokenAsterisk, Value: "*", Line: line, Column: col}
	case '/':
		l.readChar()
		return Token{Type: TokenSlash, Value: "/", Line: line, Column: col}
	case '%':
		l.readChar()
		return Token{Type: TokenPercent, Value: "%", Line: line, Column: col}
	case '^':
		l.readChar()
		return Token{Type: TokenCaret, Value: "^", Line: line, Column: col}
	case '=':
		l.readChar()
		return Token{Type: TokenEqual, Value: "=", Line: line, Column: col}
	case '!':
		if l.peekChar() == '=' {
			l.readChar()
			l.readChar()
			return Token{Type: TokenNotEqual, Value: "!=", Line: line, Column: col}
		}
		l.readChar()
		return Token{Type: TokenError, Value: "!", Line: line, Column: col}
	case '<':
		if l.peekChar() == '>' {
			l.readChar()
			l.readChar()
			return Token{Type: TokenNotEqual, Value: "<>", Line: line, Column: col}
		}
		if l.peekChar() == '=' {
			l.readChar()
			l.readChar()
			return Token{Type: TokenLte, Value: "<=", Line: line, Column: col}
		}
		if l.peekChar() == '-' {
			l.readChar()
			l.readChar()
			return Token{Type: TokenArrowL, Value: "<-", Line: line, Column: col}
		}
		l.readChar()
		return Token{Type: TokenLt, Value: "<", Line: line, Column: col}
	case '>':
		if l.peekChar() == '=' {
			l.readChar()
			l.readChar()
			return Token{Type: TokenGte, Value: ">=", Line: line, Column: col}
		}
		l.readChar()
		return Token{Type: TokenGt, Value: ">", Line: line, Column: col}
	case '-':
		if l.peekChar() == '>' {
			l.readChar()
			l.readChar()
			return Token{Type: TokenArrowR, Value: "->", Line: line, Column: col}
		}
		l.readChar()
		return Token{Type: TokenDash, Value: "-", Line: line, Column: col}
	case '\'', '"':
		str, err := l.readString(l.ch)
		if err != nil {
			return Token{Type: TokenError, Value: err.Error(), Line: line, Column: col}
		}
		return Token{Type: TokenString, Value: str, Line: line, Column: col}
	case '`':
		// Escaped identifier `column name`
		ident, err := l.readEscapedIdent()
		if err != nil {
			return Token{Type: TokenError, Value: err.Error(), Line: line, Column: col}
		}
		return Token{Type: TokenIdent, Value: ident, Line: line, Column: col}
	case '$', '@':
		l.readChar()
		ident := l.readIdentifier()
		return Token{Type: TokenParameter, Value: ident, Line: line, Column: col}
	default:
		if isDigit(l.ch) {
			num := l.readNumber()
			return Token{Type: TokenNumber, Value: num, Line: line, Column: col}
		}
		if isIdentStart(l.ch) {
			ident := l.readIdentifier()
			upper := strings.ToUpper(ident)
			if kwType, ok := keywords[upper]; ok {
				return Token{Type: kwType, Value: ident, Line: line, Column: col}
			}
			return Token{Type: TokenIdent, Value: ident, Line: line, Column: col}
		}

		ch := l.ch
		l.readChar()
		return Token{Type: TokenError, Value: fmt.Sprintf("unexpected character %q", ch), Line: line, Column: col}
	}
}

func (l *Lexer) skipWhitespaceAndComments() {
	for {
		if l.ch == ' ' || l.ch == '\t' || l.ch == '\r' {
			l.readChar()
		} else if l.ch == '\n' {
			l.line++
			l.column = 0
			l.readChar()
		} else if l.ch == '/' && l.peekChar() == '/' {
			// Single-line comment
			for l.ch != '\n' && l.ch != 0 {
				l.readChar()
			}
		} else {
			break
		}
	}
}

func (l *Lexer) readString(quote rune) (string, error) {
	l.readChar() // skip open quote
	var b strings.Builder
	for l.ch != quote && l.ch != 0 {
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '\\':
				b.WriteByte('\\')
			case '\'':
				b.WriteByte('\'')
			case '"':
				b.WriteByte('"')
			default:
				b.WriteRune(l.ch)
			}
		} else {
			b.WriteRune(l.ch)
		}
		l.readChar()
	}
	if l.ch == 0 {
		return "", fmt.Errorf("unterminated string literal")
	}
	l.readChar() // skip closing quote
	return b.String(), nil
}

func (l *Lexer) readEscapedIdent() (string, error) {
	l.readChar() // skip open `
	var b strings.Builder
	for l.ch != '`' && l.ch != 0 {
		b.WriteRune(l.ch)
		l.readChar()
	}
	if l.ch == 0 {
		return "", fmt.Errorf("unterminated escaped identifier")
	}
	l.readChar() // skip closing `
	return b.String(), nil
}

func (l *Lexer) readIdentifier() string {
	start := l.pos
	for isIdentPart(l.ch) {
		l.readChar()
	}
	return l.input[start:l.pos]
}

func (l *Lexer) readNumber() string {
	start := l.pos
	for isDigit(l.ch) {
		l.readChar()
	}
	if l.ch == '.' && isDigit(l.peekChar()) {
		l.readChar()
		for isDigit(l.ch) {
			l.readChar()
		}
	}
	return l.input[start:l.pos]
}

func isDigit(ch rune) bool {
	return '0' <= ch && ch <= '9'
}

func isIdentStart(ch rune) bool {
	return unicode.IsLetter(ch) || ch == '_'
}

func isIdentPart(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_'
}
