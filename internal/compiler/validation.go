package compiler

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/vmikhailov/cypher-sql-go/internal/ast"
)

var safeIdentifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// IsSafeIdentifier checks if an identifier contains only valid alphanumeric and underscore chars.
func IsSafeIdentifier(s string) bool {
	return safeIdentifierRegex.MatchString(s)
}

// ValidateIdentifier returns an error if the identifier contains invalid or potentially unsafe characters.
func ValidateIdentifier(kind, name string) error {
	if !IsSafeIdentifier(name) {
		return fmt.Errorf("invalid %s identifier %q: only alphanumeric characters and underscores are permitted",
			kind, name)
	}
	return nil
}

// SanitizeSQLLiteral escapes single quotes in SQL string literals.
func SanitizeSQLLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func (c *Compiler) validateQuery(q *ast.Query) error {
	if q == nil {
		return nil
	}

	for _, m := range q.Matches {
		if err := c.validateMatch(m); err != nil {
			return err
		}
	}

	for _, w := range q.WithClauses {
		if err := c.validateWith(w); err != nil {
			return err
		}
	}

	if q.Where != nil {
		if err := c.validateExpr(q.Where); err != nil {
			return err
		}
	}

	for _, item := range q.Return.Items {
		if item.Alias != "" {
			if err := ValidateIdentifier("alias", item.Alias); err != nil {
				return err
			}
		}
		if err := c.validateExpr(item.Expression); err != nil {
			return err
		}
	}

	for _, ob := range q.OrderBy {
		if err := c.validateExpr(ob.Expression); err != nil {
			return err
		}
	}

	if q.Skip != nil {
		if err := c.validateExpr(q.Skip); err != nil {
			return err
		}
	}
	if q.Limit != nil {
		if err := c.validateExpr(q.Limit); err != nil {
			return err
		}
	}

	return nil
}

func (c *Compiler) validateMatch(m ast.MatchClause) error {
	for _, p := range m.Paths {
		if err := c.validatePathPattern(p); err != nil {
			return err
		}
	}
	if m.Where != nil {
		if err := c.validateExpr(m.Where); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) validateWith(w ast.WithClause) error {
	for _, item := range w.Items {
		if item.Alias != "" {
			if err := ValidateIdentifier("alias", item.Alias); err != nil {
				return err
			}
		}
		if err := c.validateExpr(item.Expression); err != nil {
			return err
		}
	}
	if w.Where != nil {
		if err := c.validateExpr(w.Where); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) validatePathPattern(p ast.PathPattern) error {
	if err := c.validateNodePattern(p.Head); err != nil {
		return err
	}
	for _, elem := range p.Chain {
		if err := c.validateRelPattern(elem.Relationship); err != nil {
			return err
		}
		if err := c.validateNodePattern(elem.Target); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) validateNodePattern(node ast.NodePattern) error {
	if node.Variable != "" {
		if err := ValidateIdentifier("variable", node.Variable); err != nil {
			return err
		}
	}
	for _, l := range node.Labels {
		if err := ValidateIdentifier("label", l); err != nil {
			return err
		}
	}
	for k, v := range node.Properties {
		if err := ValidateIdentifier("property", k); err != nil {
			return err
		}
		if err := c.validateExpr(v); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) validateRelPattern(rel ast.RelationshipPattern) error {
	if rel.MinHops != nil || rel.MaxHops != nil {
		return fmt.Errorf("variable-length relationships are not yet supported")
	}
	if rel.Variable != "" {
		if err := ValidateIdentifier("variable", rel.Variable); err != nil {
			return err
		}
	}
	for _, t := range rel.Types {
		if err := ValidateIdentifier("relationship type", t); err != nil {
			return err
		}
	}
	for k, v := range rel.Properties {
		if err := ValidateIdentifier("property", k); err != nil {
			return err
		}
		if err := c.validateExpr(v); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) validateExpr(expr ast.Expression) error {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case ast.IdentifierExpr:
		return ValidateIdentifier("variable", e.Name)
	case ast.PropertyAccessExpr:
		if e.Variable != "" {
			if err := ValidateIdentifier("variable", e.Variable); err != nil {
				return err
			}
		}
		return ValidateIdentifier("property", e.Property)
	case ast.ParameterExpr:
		return ValidateIdentifier("parameter", e.Name)
	case ast.FunctionCallExpr:
		if err := ValidateIdentifier("function", e.Name); err != nil {
			return err
		}
		for _, arg := range e.Args {
			if err := c.validateExpr(arg); err != nil {
				return err
			}
		}
		return nil
	case ast.BinaryExpr:
		if err := c.validateExpr(e.Left); err != nil {
			return err
		}
		return c.validateExpr(e.Right)
	case ast.UnaryExpr:
		return c.validateExpr(e.Operand)
	case ast.ListExpr:
		for _, item := range e.Items {
			if err := c.validateExpr(item); err != nil {
				return err
			}
		}
		return nil
	case ast.MapExpr:
		for k, v := range e.Entries {
			if err := ValidateIdentifier("property", k); err != nil {
				return err
			}
			if err := c.validateExpr(v); err != nil {
				return err
			}
		}
		return nil
	case ast.PatternExpr:
		return c.validatePathPattern(e.Path)
	case ast.PatternComprehensionExpr:
		if err := c.validatePathPattern(e.Path); err != nil {
			return err
		}
		if err := c.validateExpr(e.Filter); err != nil {
			return err
		}
		return c.validateExpr(e.Projection)
	case ast.ListPredicateExpr:
		if e.Variable != "" {
			if err := ValidateIdentifier("variable", e.Variable); err != nil {
				return err
			}
		}
		if err := c.validateExpr(e.List); err != nil {
			return err
		}
		return c.validateExpr(e.Predicate)
	case ast.ListComprehensionExpr:
		if e.Variable != "" {
			if err := ValidateIdentifier("variable", e.Variable); err != nil {
				return err
			}
		}
		if err := c.validateExpr(e.List); err != nil {
			return err
		}
		if err := c.validateExpr(e.Filter); err != nil {
			return err
		}
		return c.validateExpr(e.Projection)
	case ast.CaseExpr:
		if err := c.validateExpr(e.Test); err != nil {
			return err
		}
		for _, b := range e.WhenBranches {
			if err := c.validateExpr(b.When); err != nil {
				return err
			}
			if err := c.validateExpr(b.Then); err != nil {
				return err
			}
		}
		return c.validateExpr(e.Else)
	case ast.LiteralExpr, ast.WildcardExpr:
		return nil
	default:
		return nil
	}
}
