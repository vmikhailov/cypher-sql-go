package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vmikhailov/cypher-sql-go/internal/ast"
)

// Parser parses tokens into an AST.
type Parser struct {
	lexer   *Lexer
	current Token
	peek    Token
}

// NewParser creates a new Parser instance.
func NewParser(input string) *Parser {
	l := NewLexer(input)
	p := &Parser{lexer: l}
	p.nextToken()
	p.nextToken()
	return p
}

func (p *Parser) nextToken() {
	p.current = p.peek
	p.peek = p.lexer.NextToken()
}

func (p *Parser) errorf(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("parse error at %d:%d (token %s): %s", p.current.Line, p.current.Column, p.current.Type, msg)
}

func (p *Parser) isNameToken() bool {
	return p.current.Type == TokenIdent || (p.current.Type >= TokenMatch && p.current.Type <= TokenDesc)
}

func (p *Parser) parseName() (string, error) {
	if p.isNameToken() || p.current.Type == TokenString {
		val := p.current.Value
		p.nextToken()
		return val, nil
	}
	return "", p.errorf("expected name or identifier, got %s", p.current.Type)
}

func (p *Parser) expect(t TokenType) error {
	if p.current.Type != t {
		return p.errorf("expected %s, got %s", t, p.current.Type)
	}
	p.nextToken()
	return nil
}

// Parse parses the entire Cypher query.
func (p *Parser) Parse() (*ast.Query, error) {
	q := &ast.Query{}

	// 1. Matches and intermediate WITH clauses
	for p.current.Type == TokenMatch || p.current.Type == TokenOptional || p.current.Type == TokenWith {
		if p.current.Type == TokenMatch || p.current.Type == TokenOptional {
			match, err := p.parseMatch()
			if err != nil {
				return nil, err
			}
			q.Matches = append(q.Matches, *match)
		} else if p.current.Type == TokenWith {
			with, err := p.parseWith()
			if err != nil {
				return nil, err
			}
			q.WithClauses = append(q.WithClauses, *with)
		}
	}

	// 2. Optional top-level WHERE
	if p.current.Type == TokenWhere {
		p.nextToken() // skip WHERE
		cond, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		q.Where = cond
	}

	// 3. RETURN clause
	if p.current.Type != TokenReturn {
		return nil, p.errorf("expected RETURN clause, got %s", p.current.Type)
	}
	ret, err := p.parseReturn()
	if err != nil {
		return nil, err
	}
	q.Return = *ret

	// Optional GROUP BY
	if p.current.Type == TokenGroup {
		p.nextToken() // skip GROUP
		if err := p.expect(TokenBy); err != nil {
			return nil, err
		}
		for {
			if _, err := p.parseExpression(); err != nil {
				return nil, err
			}
			if p.current.Type == TokenComma {
				p.nextToken()
			} else {
				break
			}
		}
	}

	// 4. ORDER BY
	if p.current.Type == TokenOrder {
		p.nextToken() // skip ORDER
		if err := p.expect(TokenBy); err != nil {
			return nil, err
		}
		for {
			item, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			isDesc := false
			if p.current.Type == TokenDesc {
				isDesc = true
				p.nextToken()
			} else if p.current.Type == TokenAsc {
				p.nextToken()
			}
			q.OrderBy = append(q.OrderBy, ast.OrderByItem{Expression: item, IsDesc: isDesc})
			if p.current.Type == TokenComma {
				p.nextToken()
			} else {
				break
			}
		}
	}

	// 5. SKIP & LIMIT in any order
	for p.current.Type == TokenSkip || p.current.Type == TokenLimit {
		if p.current.Type == TokenSkip {
			p.nextToken()
			skipExpr, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			q.Skip = skipExpr
		} else if p.current.Type == TokenLimit {
			p.nextToken()
			limitExpr, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			q.Limit = limitExpr
		}
	}

	if p.current.Type != TokenEOF {
		if p.current.Type == TokenError {
			return nil, p.errorf("%s", p.current.Value)
		}
		return nil, p.errorf("unexpected trailing token %s (%q)", p.current.Type, p.current.Value)
	}

	return q, nil
}

func (p *Parser) parseMatch() (*ast.MatchClause, error) {
	isOpt := false
	if p.current.Type == TokenOptional {
		isOpt = true
		p.nextToken() // skip OPTIONAL
	}
	if err := p.expect(TokenMatch); err != nil {
		return nil, err
	}

	var paths []ast.PathPattern
	for {
		path, err := p.parsePathPattern()
		if err != nil {
			return nil, err
		}
		paths = append(paths, *path)
		if p.current.Type == TokenComma {
			p.nextToken()
		} else {
			break
		}
	}

	var whereExpr ast.Expression
	if p.current.Type == TokenWhere {
		p.nextToken()
		w, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		whereExpr = w
	}

	return &ast.MatchClause{
		IsOptional: isOpt,
		Paths:      paths,
		Where:      whereExpr,
	}, nil
}

func (p *Parser) parseWith() (*ast.WithClause, error) {
	p.nextToken() // skip WITH
	isDistinct := false
	if p.current.Type == TokenDistinct {
		isDistinct = true
		p.nextToken()
	}

	var items []ast.ProjectionItem
	for {
		item, err := p.parseProjectionItem()
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
		if p.current.Type == TokenComma {
			p.nextToken()
		} else {
			break
		}
	}

	var whereExpr ast.Expression
	if p.current.Type == TokenWhere {
		p.nextToken()
		w, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		whereExpr = w
	}

	return &ast.WithClause{
		IsDistinct: isDistinct,
		Items:      items,
		Where:      whereExpr,
	}, nil
}

func (p *Parser) parseReturn() (*ast.ReturnClause, error) {
	p.nextToken() // skip RETURN
	isDistinct := false
	if p.current.Type == TokenDistinct {
		isDistinct = true
		p.nextToken()
	}

	var items []ast.ProjectionItem
	for {
		item, err := p.parseProjectionItem()
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
		if p.current.Type == TokenComma {
			p.nextToken()
		} else {
			break
		}
	}

	return &ast.ReturnClause{
		IsDistinct: isDistinct,
		Items:      items,
	}, nil
}

func (p *Parser) parseProjectionItem() (*ast.ProjectionItem, error) {
	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	alias := ""
	if p.current.Type == TokenAs {
		p.nextToken()
		al, err := p.parseName()
		if err != nil {
			return nil, err
		}
		alias = al
	}

	return &ast.ProjectionItem{Expression: expr, Alias: alias}, nil
}

func (p *Parser) parsePathPattern() (*ast.PathPattern, error) {
	head, err := p.parseNodePattern()
	if err != nil {
		return nil, err
	}

	var chain []ast.PathElement
	for p.current.Type == TokenDash || p.current.Type == TokenArrowL {
		elem, err := p.parsePathElement()
		if err != nil {
			return nil, err
		}
		chain = append(chain, *elem)
	}

	return &ast.PathPattern{Head: *head, Chain: chain}, nil
}

func (p *Parser) parseNodePattern() (*ast.NodePattern, error) {
	if err := p.expect(TokenLParen); err != nil {
		return nil, err
	}

	node := &ast.NodePattern{}
	if p.isNameToken() {
		v, err := p.parseName()
		if err != nil {
			return nil, err
		}
		node.Variable = v
	}

	for p.current.Type == TokenColon {
		p.nextToken()
		lbl, err := p.parseName()
		if err != nil {
			return nil, err
		}
		node.Labels = append(node.Labels, lbl)
	}

	if p.current.Type == TokenLBrace {
		props, err := p.parseMapLiteralEntries()
		if err != nil {
			return nil, err
		}
		node.Properties = props
	}

	if err := p.expect(TokenRParen); err != nil {
		return nil, err
	}
	return node, nil
}

func (p *Parser) parsePathElement() (*ast.PathElement, error) {
	rel := ast.RelationshipPattern{Direction: ast.DirectionUndirected}

	if p.current.Type == TokenArrowL {
		rel.Direction = ast.DirectionIncoming
		p.nextToken() // skip <-
		if err := p.expect(TokenLBracket); err != nil {
			return nil, err
		}
		if err := p.parseRelationshipDetails(&rel); err != nil {
			return nil, err
		}
		if err := p.expect(TokenRBracket); err != nil {
			return nil, err
		}
		if err := p.expect(TokenDash); err != nil {
			return nil, err
		}
	} else if p.current.Type == TokenDash {
		p.nextToken() // skip -
		if p.current.Type == TokenLBracket {
			p.nextToken() // skip [
			if err := p.parseRelationshipDetails(&rel); err != nil {
				return nil, err
			}
			if err := p.expect(TokenRBracket); err != nil {
				return nil, err
			}
			if p.current.Type == TokenArrowR {
				rel.Direction = ast.DirectionOutgoing
				p.nextToken()
			} else if p.current.Type == TokenDash {
				rel.Direction = ast.DirectionUndirected
				p.nextToken()
			} else {
				return nil, p.errorf("expected '->' or '-' after relationship, got %s", p.current.Type)
			}
		} else if p.current.Type == TokenArrowR {
			rel.Direction = ast.DirectionOutgoing
			p.nextToken()
		} else {
			return nil, p.errorf("expected '[' or '->' after '-', got %s", p.current.Type)
		}
	}

	target, err := p.parseNodePattern()
	if err != nil {
		return nil, err
	}

	return &ast.PathElement{Relationship: rel, Target: *target}, nil
}

func (p *Parser) parseRelationshipDetails(rel *ast.RelationshipPattern) error {
	if p.isNameToken() && p.current.Type != TokenColon &&
		p.current.Type != TokenAsterisk && p.current.Type != TokenLBrace {
		v, err := p.parseName()
		if err != nil {
			return err
		}
		rel.Variable = v
	}

	if p.current.Type == TokenColon {
		p.nextToken()
		for {
			tName, err := p.parseName()
			if err != nil {
				return err
			}
			rel.Types = append(rel.Types, tName)
			if p.current.Type == TokenPipe {
				p.nextToken()
			} else {
				break
			}
		}
	}

	// VarLen: *1..3 or *..3 or *1.. or * or *2
	if p.current.Type == TokenAsterisk {
		p.nextToken()
		min := 1
		max := 10 // default max hops
		hasMin := false
		hasDots := false
		hasMax := false

		if p.current.Type == TokenNumber {
			n, err := strconv.Atoi(p.current.Value)
			if err != nil {
				return p.errorf("invalid number %q for hops: %v", p.current.Value, err)
			}
			if n < 0 {
				return p.errorf("hops cannot be negative: %d", n)
			}
			min = n
			max = n
			hasMin = true
			p.nextToken()
		}

		if p.current.Type == TokenIdent && p.current.Value == ".." {
			hasDots = true
			p.nextToken()
			if p.current.Type == TokenNumber {
				n, err := strconv.Atoi(p.current.Value)
				if err != nil {
					return p.errorf("invalid number %q for max hops: %v", p.current.Value, err)
				}
				if n < 0 {
					return p.errorf("max hops cannot be negative: %d", n)
				}
				max = n
				hasMax = true
				p.nextToken()
			} else {
				// e.g. *1.. or *..
				max = 10
			}
		} else if !hasMin {
			// [*] without numbers and without dots -> equivalent to *1..10
			min = 1
			max = 10
		}

		if hasDots && hasMax && max < min {
			return p.errorf("max hops (%d) cannot be less than min hops (%d)", max, min)
		}

		rel.MinHops = &min
		rel.MaxHops = &max
	}

	if p.current.Type == TokenLBrace {
		props, err := p.parseMapLiteralEntries()
		if err != nil {
			return err
		}
		rel.Properties = props
	}

	return nil
}

func (p *Parser) parseMapLiteralEntries() (map[string]ast.Expression, error) {
	if err := p.expect(TokenLBrace); err != nil {
		return nil, err
	}
	entries := make(map[string]ast.Expression)
	if p.current.Type == TokenRBrace {
		p.nextToken()
		return entries, nil
	}

	for {
		key, err := p.parseName()
		if err != nil {
			return nil, err
		}
		if err := p.expect(TokenColon); err != nil {
			return nil, err
		}
		val, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		entries[key] = val
		if p.current.Type == TokenComma {
			p.nextToken()
		} else {
			break
		}
	}

	if err := p.expect(TokenRBrace); err != nil {
		return nil, err
	}
	return entries, nil
}

// ast.Expression parsing with Precedence Climbing

func (p *Parser) parseExpression() (ast.Expression, error) {
	return p.parseOr()
}

func (p *Parser) parseOr() (ast.Expression, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.current.Type == TokenOr {
		p.nextToken()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = ast.BinaryExpr{Left: left, Op: ast.OpOr, Right: right}
	}
	return left, nil
}

func (p *Parser) parseAnd() (ast.Expression, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.current.Type == TokenAnd {
		p.nextToken()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = ast.BinaryExpr{Left: left, Op: ast.OpAnd, Right: right}
	}
	return left, nil
}

func (p *Parser) parseNot() (ast.Expression, error) {
	if p.current.Type == TokenNot {
		p.nextToken()
		operand, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return ast.UnaryExpr{Op: ast.OpNot, Operand: operand}, nil
	}
	return p.parseComparison()
}

func (p *Parser) parseComparison() (ast.Expression, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}

	for {
		switch p.current.Type {
		case TokenEqual:
			p.nextToken()
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = ast.BinaryExpr{Left: left, Op: ast.OpEq, Right: right}
		case TokenNotEqual:
			p.nextToken()
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = ast.BinaryExpr{Left: left, Op: ast.OpNeq, Right: right}
		case TokenLt:
			p.nextToken()
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = ast.BinaryExpr{Left: left, Op: ast.OpLt, Right: right}
		case TokenLte:
			p.nextToken()
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = ast.BinaryExpr{Left: left, Op: ast.OpLte, Right: right}
		case TokenGt:
			p.nextToken()
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = ast.BinaryExpr{Left: left, Op: ast.OpGt, Right: right}
		case TokenGte:
			p.nextToken()
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = ast.BinaryExpr{Left: left, Op: ast.OpGte, Right: right}
		case TokenIn:
			p.nextToken()
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = ast.BinaryExpr{Left: left, Op: ast.OpIn, Right: right}
		case TokenStarts:
			p.nextToken()
			if err := p.expect(TokenWith); err != nil {
				return nil, err
			}
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = ast.BinaryExpr{Left: left, Op: ast.OpStarts, Right: right}
		case TokenEnds:
			p.nextToken()
			if err := p.expect(TokenWith); err != nil {
				return nil, err
			}
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = ast.BinaryExpr{Left: left, Op: ast.OpEnds, Right: right}
		case TokenContains:
			p.nextToken()
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = ast.BinaryExpr{Left: left, Op: ast.OpContains, Right: right}
		case TokenIs:
			p.nextToken()
			if p.current.Type == TokenNot {
				p.nextToken()
				if err := p.expect(TokenNull); err != nil {
					return nil, err
				}
				left = ast.BinaryExpr{Left: left, Op: ast.OpIsNot, Right: ast.LiteralExpr{Kind: ast.LiteralNull, Raw: "NULL"}}
			} else if p.current.Type == TokenNull {
				p.nextToken()
				left = ast.BinaryExpr{Left: left, Op: ast.OpIs, Right: ast.LiteralExpr{Kind: ast.LiteralNull, Raw: "NULL"}}
			} else {
				return nil, p.errorf("expected NULL or NOT NULL after IS")
			}
		default:
			return left, nil
		}
	}
}

func (p *Parser) parseAdditive() (ast.Expression, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.current.Type == TokenPlus || p.current.Type == TokenDash {
		op := ast.OpAdd
		if p.current.Type == TokenDash {
			op = ast.OpSub
		}
		p.nextToken()
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = ast.BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left, nil
}

func (p *Parser) parseMultiplicative() (ast.Expression, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.current.Type == TokenAsterisk || p.current.Type == TokenSlash || p.current.Type == TokenPercent {
		op := ast.OpMul
		if p.current.Type == TokenSlash {
			op = ast.OpDiv
		} else if p.current.Type == TokenPercent {
			op = ast.OpMod
		}
		p.nextToken()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = ast.BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left, nil
}

func (p *Parser) parseUnary() (ast.Expression, error) {
	if p.current.Type == TokenDash {
		p.nextToken()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return ast.UnaryExpr{Op: ast.OpMinus, Operand: operand}, nil
	}
	if p.current.Type == TokenPlus {
		p.nextToken()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return ast.UnaryExpr{Op: ast.OpPlus, Operand: operand}, nil
	}
	return p.parsePostfix()
}

func (p *Parser) parsePostfix() (ast.Expression, error) {
	expr, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	for {
		if p.current.Type == TokenDot {
			p.nextToken()
			prop, err := p.parseName()
			if err != nil {
				return nil, err
			}

			if id, ok := expr.(ast.IdentifierExpr); ok {
				expr = ast.PropertyAccessExpr{Variable: id.Name, Property: prop}
			} else if pa, ok := expr.(ast.PropertyAccessExpr); ok {
				expr = ast.PropertyAccessExpr{Variable: pa.Variable + "." + pa.Property, Property: prop}
			} else {
				expr = ast.PropertyAccessExpr{Variable: fmt.Sprintf("%v", expr), Property: prop}
			}
		} else if p.current.Type == TokenColon {
			// Label check: e.g. p:Service or ep:Endpoint
			p.nextToken()
			label, err := p.parseName()
			if err != nil {
				return nil, err
			}
			expr = ast.BinaryExpr{
				Left:  expr,
				Op:    ast.OpEq,
				Right: ast.LiteralExpr{Kind: ast.LiteralString, StrVal: label, Raw: label},
			}
		} else {
			break
		}
	}
	return expr, nil
}

func (p *Parser) parsePrimary() (ast.Expression, error) {
	switch p.current.Type {
	case TokenAsterisk:
		p.nextToken()
		return ast.WildcardExpr{}, nil

	case TokenLParen:
		// Check if this is a ast.PatternExpr (n)-[:REL]->(m) or parenthesized expr
		return p.parseParenOrPattern()

	case TokenLBracket:
		// Pattern comprehension [(p)-[...] | ...], list comprehension [x IN list | ...], or list literal [1, 2]
		return p.parseBracketExpression()

	case TokenLBrace:
		entries, err := p.parseMapLiteralEntries()
		if err != nil {
			return nil, err
		}
		return ast.MapExpr{Entries: entries}, nil

	case TokenString:
		val := p.current.Value
		p.nextToken()
		return ast.LiteralExpr{Kind: ast.LiteralString, StrVal: val, Raw: val}, nil

	case TokenNumber:
		val := p.current.Value
		p.nextToken()
		isInteger := !strings.Contains(val, ".")
		num, _ := strconv.ParseFloat(val, 64)
		return ast.LiteralExpr{Kind: ast.LiteralNumber, NumVal: num, IsInteger: isInteger, Raw: val}, nil

	case TokenTrue:
		p.nextToken()
		return ast.LiteralExpr{Kind: ast.LiteralBool, BoolVal: true, Raw: "true"}, nil

	case TokenFalse:
		p.nextToken()
		return ast.LiteralExpr{Kind: ast.LiteralBool, BoolVal: false, Raw: "false"}, nil

	case TokenNull:
		p.nextToken()
		return ast.LiteralExpr{Kind: ast.LiteralNull, Raw: "null"}, nil

	case TokenParameter:
		name := p.current.Value
		p.nextToken()
		return ast.ParameterExpr{Name: name}, nil

	case TokenCase:
		return p.parseCase()

	case TokenExists:
		p.nextToken()
		if err := p.expect(TokenLParen); err != nil {
			return nil, err
		}
		inner, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if err := p.expect(TokenRParen); err != nil {
			return nil, err
		}
		return ast.FunctionCallExpr{Name: "exists", Args: []ast.Expression{inner}}, nil

	case TokenIdent:
		ident := p.current.Value
		lower := strings.ToLower(ident)

		// Check for quantifier predicates: any(x IN list WHERE pred), all(), none(), single()
		if (lower == "any" || lower == "all" || lower == "none" || lower == "single") && p.peek.Type == TokenLParen {
			p.nextToken() // skip quantifier
			p.nextToken() // skip (
			if p.current.Type != TokenIdent {
				return nil, p.errorf("expected variable name in quantifier %s, got %s", lower, p.current.Type)
			}
			varName := p.current.Value
			p.nextToken()
			if err := p.expect(TokenIn); err != nil {
				return nil, err
			}
			listExpr, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			if err := p.expect(TokenWhere); err != nil {
				return nil, err
			}
			predExpr, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			if err := p.expect(TokenRParen); err != nil {
				return nil, err
			}
			return ast.ListPredicateExpr{
				Quantifier: lower,
				Variable:   varName,
				List:       listExpr,
				Predicate:  predExpr,
			}, nil
		}

		p.nextToken()

		// Function call: ident(arg1, arg2)
		if p.current.Type == TokenLParen {
			p.nextToken() // skip (
			isDistinct := false
			if p.current.Type == TokenDistinct {
				isDistinct = true
				p.nextToken()
			}
			var args []ast.Expression
			if p.current.Type != TokenRParen {
				for {
					arg, err := p.parseExpression()
					if err != nil {
						return nil, err
					}
					args = append(args, arg)
					if p.current.Type == TokenComma {
						p.nextToken()
					} else {
						break
					}
				}
			}
			if err := p.expect(TokenRParen); err != nil {
				return nil, err
			}
			return ast.FunctionCallExpr{Name: ident, IsDistinct: isDistinct, Args: args}, nil
		}

		return ast.IdentifierExpr{Name: ident}, nil

	default:
		return nil, p.errorf("unexpected token in primary expression: %s (%q)", p.current.Type, p.current.Value)
	}
}

func (p *Parser) parseParenOrPattern() (ast.Expression, error) {
	// Try parsing as ast.PatternExpr (n)-[:REL]->(m)
	// A pattern must have at least one chain element (i.e. a relationship)
	// We can tentatively parse
	if p.peek.Type == TokenColon || p.peek.Type == TokenRParen || p.peek.Type == TokenIdent || p.peek.Type == TokenLBrace {
		// Attempt to parse pattern
		savedLexerPos := p.lexer.pos
		savedLexerRead := p.lexer.read
		savedLexerCh := p.lexer.ch
		savedLexerLine := p.lexer.line
		savedLexerCol := p.lexer.column
		savedCurrent := p.current
		savedPeek := p.peek

		path, err := p.parsePathPattern()
		if err == nil && len(path.Chain) > 0 {
			return ast.PatternExpr{Path: *path}, nil
		}

		// Rollback if not a pattern
		p.lexer.pos = savedLexerPos
		p.lexer.read = savedLexerRead
		p.lexer.ch = savedLexerCh
		p.lexer.line = savedLexerLine
		p.lexer.column = savedLexerCol
		p.current = savedCurrent
		p.peek = savedPeek
	}

	// Normal parenthesized expression: (expr)
	p.nextToken() // skip (
	inner, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if err := p.expect(TokenRParen); err != nil {
		return nil, err
	}
	return inner, nil
}

func (p *Parser) parseBracketExpression() (ast.Expression, error) {
	p.nextToken() // skip [

	if p.current.Type == TokenRBracket {
		p.nextToken()
		return ast.ListExpr{Items: nil}, nil
	}

	// Check if this is a list comprehension: [x IN list WHERE ... | expr]
	if p.current.Type == TokenIdent && p.peek.Type == TokenIn {
		varName := p.current.Value
		p.nextToken() // skip varName
		p.nextToken() // skip IN

		listExpr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}

		var filterExpr ast.Expression
		if p.current.Type == TokenWhere {
			p.nextToken()
			f, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			filterExpr = f
		}

		var projExpr ast.Expression
		if p.current.Type == TokenPipe {
			p.nextToken()
			pr, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			projExpr = pr
		}

		if err := p.expect(TokenRBracket); err != nil {
			return nil, err
		}
		return ast.ListComprehensionExpr{
			Variable:   varName,
			List:       listExpr,
			Filter:     filterExpr,
			Projection: projExpr,
		}, nil
	}

	// Check if this is a pattern comprehension: [(n)-[:REL]->(m) WHERE ... | expr]
	if p.current.Type == TokenLParen {
		savedLexerPos := p.lexer.pos
		savedLexerRead := p.lexer.read
		savedLexerCh := p.lexer.ch
		savedLexerLine := p.lexer.line
		savedLexerCol := p.lexer.column
		savedCurrent := p.current
		savedPeek := p.peek

		path, err := p.parsePathPattern()
		if err == nil && len(path.Chain) > 0 {
			var filterExpr ast.Expression
			if p.current.Type == TokenWhere {
				p.nextToken()
				f, err := p.parseExpression()
				if err != nil {
					return nil, err
				}
				filterExpr = f
			}

			if p.current.Type == TokenPipe {
				p.nextToken()
				projExpr, err := p.parseExpression()
				if err != nil {
					return nil, err
				}
				if err := p.expect(TokenRBracket); err != nil {
					return nil, err
				}
				return ast.PatternComprehensionExpr{
					Path:       *path,
					Filter:     filterExpr,
					Projection: projExpr,
				}, nil
			}
		}

		// Rollback to parse as normal list literal [expr, ...]
		p.lexer.pos = savedLexerPos
		p.lexer.read = savedLexerRead
		p.lexer.ch = savedLexerCh
		p.lexer.line = savedLexerLine
		p.lexer.column = savedLexerCol
		p.current = savedCurrent
		p.peek = savedPeek
	}

	// Normal list literal [elem1, elem2, ...]
	var items []ast.Expression
	for {
		item, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if p.current.Type == TokenComma {
			p.nextToken()
		} else {
			break
		}
	}
	if err := p.expect(TokenRBracket); err != nil {
		return nil, err
	}
	return ast.ListExpr{Items: items}, nil
}

func (p *Parser) parseCase() (ast.Expression, error) {
	p.nextToken() // skip CASE

	var testExpr ast.Expression
	if p.current.Type != TokenWhen {
		t, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		testExpr = t
	}

	var whenBranches []ast.CaseWhen
	for p.current.Type == TokenWhen {
		p.nextToken() // skip WHEN
		w, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if err := p.expect(TokenThen); err != nil {
			return nil, err
		}
		th, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		whenBranches = append(whenBranches, ast.CaseWhen{When: w, Then: th})
	}

	var elseExpr ast.Expression
	if p.current.Type == TokenElse {
		p.nextToken() // skip ELSE
		el, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		elseExpr = el
	}

	if err := p.expect(TokenEnd); err != nil {
		return nil, err
	}

	return ast.CaseExpr{
		Test:         testExpr,
		WhenBranches: whenBranches,
		Else:         elseExpr,
	}, nil
}
