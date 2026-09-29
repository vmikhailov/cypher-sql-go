package cyphersql

import "fmt"

// TokenType represents a lexical token type.
type TokenType int

const (
	TokenEOF TokenType = iota
	TokenError
	TokenIdent
	TokenString
	TokenNumber
	TokenParameter

	// Keywords
	TokenMatch
	TokenOptional
	TokenWhere
	TokenWith
	TokenReturn
	TokenOrder
	TokenBy
	TokenSkip
	TokenLimit
	TokenDistinct
	TokenAs
	TokenAnd
	TokenOr
	TokenNot
	TokenIn
	TokenIs
	TokenNull
	TokenTrue
	TokenFalse
	TokenCase
	TokenWhen
	TokenThen
	TokenElse
	TokenEnd
	TokenExists
	TokenStarts
	TokenEnds
	TokenContains
	TokenAsc
	TokenDesc

	// Punctuation & Operators
	TokenLParen   // (
	TokenRParen   // )
	TokenLBracket // [
	TokenRBracket // ]
	TokenLBrace   // {
	TokenRBrace   // }
	TokenColon    // :
	TokenComma    // ,
	TokenDot      // .
	TokenPipe     // |
	TokenArrowR   // ->
	TokenArrowL   // <-
	TokenDash     // -
	TokenEqual    // =
	TokenNotEqual // <> or !=
	TokenLt       // <
	TokenLte      // <=
	TokenGt       // >
	TokenGte      // >=
	TokenPlus     // +
	TokenAsterisk // *
	TokenSlash    // /
	TokenPercent  // %
	TokenCaret    // ^
)

var tokenNames = map[TokenType]string{
	TokenEOF:       "EOF",
	TokenError:     "Error",
	TokenIdent:     "Identifier",
	TokenString:    "String",
	TokenNumber:    "Number",
	TokenParameter: "Parameter",

	TokenMatch:    "MATCH",
	TokenOptional: "OPTIONAL",
	TokenWhere:    "WHERE",
	TokenWith:     "WITH",
	TokenReturn:   "RETURN",
	TokenOrder:    "ORDER",
	TokenBy:       "BY",
	TokenSkip:     "SKIP",
	TokenLimit:    "LIMIT",
	TokenDistinct: "DISTINCT",
	TokenAs:       "AS",
	TokenAnd:      "AND",
	TokenOr:       "OR",
	TokenNot:      "NOT",
	TokenIn:       "IN",
	TokenIs:       "IS",
	TokenNull:     "NULL",
	TokenTrue:     "TRUE",
	TokenFalse:    "FALSE",
	TokenCase:     "CASE",
	TokenWhen:     "WHEN",
	TokenThen:     "THEN",
	TokenElse:     "ELSE",
	TokenEnd:      "END",
	TokenExists:   "EXISTS",
	TokenStarts:   "STARTS",
	TokenEnds:     "ENDS",
	TokenContains: "CONTAINS",
	TokenAsc:      "ASC",
	TokenDesc:     "DESC",

	TokenLParen:   "(",
	TokenRParen:   ")",
	TokenLBracket: "[",
	TokenRBracket: "]",
	TokenLBrace:   "{",
	TokenRBrace:   "}",
	TokenColon:    ":",
	TokenComma:    ",",
	TokenDot:      ".",
	TokenPipe:     "|",
	TokenArrowR:   "->",
	TokenArrowL:   "<-",
	TokenDash:     "-",
	TokenEqual:    "=",
	TokenNotEqual: "!=",
	TokenLt:       "<",
	TokenLte:      "<=",
	TokenGt:       ">",
	TokenGte:      ">=",
	TokenPlus:     "+",
	TokenAsterisk: "*",
	TokenSlash:    "/",
	TokenPercent:  "%",
	TokenCaret:    "^",
}

func (t TokenType) String() string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return fmt.Sprintf("Token(%d)", int(t))
}

// Token holds a single token's metadata.
type Token struct {
	Type   TokenType
	Value  string
	Line   int
	Column int
}

func (t Token) String() string {
	return fmt.Sprintf("%s(%q) at %d:%d", t.Type, t.Value, t.Line, t.Column)
}
