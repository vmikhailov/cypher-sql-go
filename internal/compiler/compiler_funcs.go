package compiler

import (
	"fmt"
	"strings"

	"github.com/vmikhailov/cypher-sql-go/internal/ast"
)

// visitFunctionCall dispatches a function call expression to the appropriate specialized compiler.
func (c *Compiler) visitFunctionCall(fn ast.FunctionCallExpr) (string, error) {
	distinctStr := ""
	if fn.IsDistinct {
		distinctStr = "DISTINCT "
	}

	// 1. Decomposed optional match aggregation (avoids cartesian product)
	if decomposed := c.tryCompileDecomposedAggregation(fn, distinctStr); decomposed != "" {
		return decomposed, nil
	}

	lower := strings.ToLower(fn.Name)

	// 2. OpenCypher graph & relationship introspection functions
	if sql, handled, err := c.compileGraphFunction(lower, fn); handled {
		return sql, err
	}

	// 3. Aggregate functions (count, collect)
	if sql, handled, err := c.compileAggregateFunction(lower, fn, distinctStr); handled {
		return sql, err
	}

	// 4. Scalar functions (coalesce, toLower, toUpper, toString, size, length)
	if sql, handled, err := c.compileScalarFunction(lower, fn); handled {
		return sql, err
	}

	// 5. Default fallback to native SQL function
	return c.compileDefaultFunctionCall(fn, distinctStr)
}

// compileGraphFunction handles openCypher graph introspection functions.
func (c *Compiler) compileGraphFunction(lower string, fn ast.FunctionCallExpr) (string, bool, error) {
	switch lower {
	case "id", "elementid":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("%s() takes exactly 1 argument, got %d", fn.Name, len(fn.Args))
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			return c.visitPropertyAccess(ast.PropertyAccessExpr{
				Variable: id.Name,
				Property: "id",
			}), true, nil
		}
		return "", true, fmt.Errorf("%s() expects a node or relationship variable, got %T", fn.Name, fn.Args[0])

	case "type":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("type() takes exactly 1 argument, got %d", len(fn.Args))
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			if c.declaredRels[id.Name] {
				return c.visitPropertyAccess(ast.PropertyAccessExpr{
					Variable: id.Name,
					Property: "type",
				}), true, nil
			}
			if aliasSQL, ok := c.withAliases[id.Name]; ok {
				return "COALESCE(" + c.schema.Dialect.JSONExtract(aliasSQL, "type") + ", " +
					c.schema.Dialect.JSONExtract(aliasSQL, "kind") + ")", true, nil
			}
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return c.schema.Dialect.JSONExtract(argSQL, "type"), true, nil

	case "startnode", "start_node":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("%s() takes exactly 1 argument, got %d", fn.Name, len(fn.Args))
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			if c.declaredRels[id.Name] {
				return c.visitPropertyAccess(ast.PropertyAccessExpr{
					Variable: id.Name,
					Property: "from_id",
				}), true, nil
			}
			if aliasSQL, ok := c.withAliases[id.Name]; ok {
				return "COALESCE(" + c.schema.Dialect.JSONExtract(aliasSQL, "from") + ", " +
					c.schema.Dialect.JSONExtract(aliasSQL, "from_id") + ")", true, nil
			}
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return "COALESCE(" + c.schema.Dialect.JSONExtract(argSQL, "from") + ", " +
			c.schema.Dialect.JSONExtract(argSQL, "from_id") + ")", true, nil

	case "endnode", "end_node":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("%s() takes exactly 1 argument, got %d", fn.Name, len(fn.Args))
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			if c.declaredRels[id.Name] {
				return c.visitPropertyAccess(ast.PropertyAccessExpr{
					Variable: id.Name,
					Property: "to_id",
				}), true, nil
			}
			if aliasSQL, ok := c.withAliases[id.Name]; ok {
				return "COALESCE(" + c.schema.Dialect.JSONExtract(aliasSQL, "to") + ", " +
					c.schema.Dialect.JSONExtract(aliasSQL, "to_id") + ")", true, nil
			}
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return "COALESCE(" + c.schema.Dialect.JSONExtract(argSQL, "to") + ", " +
			c.schema.Dialect.JSONExtract(argSQL, "to_id") + ")", true, nil

	case "properties":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("properties() takes exactly 1 argument, got %d", len(fn.Args))
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			if c.declaredNodes[id.Name] {
				return c.renderEntityProperties(c.escapeVar(id.Name) + "." + c.schema.NodePropsCol), true, nil
			}
			if c.declaredRels[id.Name] {
				return c.renderEntityProperties(c.escapeVar(id.Name) + "." + c.schema.EdgePropsCol), true, nil
			}
			if aliasSQL, ok := c.withAliases[id.Name]; ok {
				return "COALESCE(" + c.schema.Dialect.JSONExtract(aliasSQL, "properties") + ", " + aliasSQL + ")", true, nil
			}
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return c.renderEntityProperties(argSQL), true, nil

	case "labels":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("labels() takes exactly 1 argument, got %d", len(fn.Args))
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			v := c.escapeVar(id.Name)
			return c.schema.Dialect.ArrayLiteral(v + "." + c.schema.NodeKindCol), true, nil
		}
		return "", true, fmt.Errorf("labels() expects a node variable, got %T", fn.Args[0])

	case "exists":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("exists() takes exactly 1 argument, got %d", len(fn.Args))
		}
		if pat, ok := fn.Args[0].(ast.PatternExpr); ok {
			sql, err := c.visitPatternExpr(pat)
			return sql, true, err
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return "(" + argSQL + " IS NOT NULL)", true, nil

	case "keys":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("keys() takes exactly 1 argument, got %d", len(fn.Args))
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			v := c.escapeVar(id.Name)
			if c.declaredNodes[id.Name] {
				return "(SELECT json_group_array(key) FROM json_each(" + v + "." + c.schema.NodePropsCol + "))", true, nil
			}
			if c.declaredRels[id.Name] {
				return "(SELECT json_group_array(key) FROM json_each(" + v + "." + c.schema.EdgePropsCol + "))", true, nil
			}
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return "(SELECT json_group_array(key) FROM json_each(" + argSQL + "))", true, nil

	case "nodes":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("nodes() takes exactly 1 argument, got %d", len(fn.Args))
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			v := c.escapeVar(id.Name)
			return "json_array(" + v + ".start_id, " + v + ".end_id)", true, nil
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return argSQL, true, nil

	default:
		return "", false, nil
	}
}

// compileAggregateFunction handles Cypher aggregate functions.
func (c *Compiler) compileAggregateFunction(lower string, fn ast.FunctionCallExpr, distinctStr string) (string, bool, error) {
	switch lower {
	case "count":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("count() takes exactly 1 argument, got %d", len(fn.Args))
		}
		if _, ok := fn.Args[0].(ast.WildcardExpr); ok {
			return "COUNT(" + distinctStr + "*)", true, nil
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			if c.declaredNodes[id.Name] {
				return "COUNT(" + distinctStr + c.escapeVar(id.Name) + "." + c.schema.NodeIDCol + ")", true, nil
			}
			if c.declaredRels[id.Name] {
				return "COUNT(" + distinctStr + c.escapeVar(id.Name) + ".rowid)", true, nil
			}
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return "COUNT(" + distinctStr + argSQL + ")", true, nil

	case "collect":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("collect() takes exactly 1 argument, got %d", len(fn.Args))
		}
		innerSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return c.schema.Dialect.ArrayAgg(innerSQL, innerSQL+" IS NOT NULL", fn.IsDistinct), true, nil

	default:
		return "", false, nil
	}
}

// compileScalarFunction handles built-in scalar type conversion and utility functions.
func (c *Compiler) compileScalarFunction(lower string, fn ast.FunctionCallExpr) (string, bool, error) {
	switch lower {
	case "coalesce":
		var args []string
		for _, arg := range fn.Args {
			s, err := c.visitExpression(arg)
			if err != nil {
				return "", true, err
			}
			args = append(args, s)
		}
		return "COALESCE(" + strings.Join(args, ", ") + ")", true, nil

	case "tolower":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("toLower() takes exactly 1 argument, got %d", len(fn.Args))
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return "lower(" + argSQL + ")", true, nil

	case "toupper":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("toUpper() takes exactly 1 argument, got %d", len(fn.Args))
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return "upper(" + argSQL + ")", true, nil

	case "tostring":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("toString() takes exactly 1 argument, got %d", len(fn.Args))
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return "CAST(" + argSQL + " AS TEXT)", true, nil

	case "length":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("length() takes exactly 1 argument, got %d", len(fn.Args))
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			v := c.escapeVar(id.Name)
			if c.declaredRels[id.Name] || (c.varAliases != nil && c.varAliases[id.Name] != "") {
				return v + ".depth", true, nil
			}
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return "length(" + argSQL + ")", true, nil

	case "size":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("size() takes exactly 1 argument, got %d", len(fn.Args))
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return c.schema.Dialect.ArrayLength(argSQL), true, nil

	default:
		return "", false, nil
	}
}

// compileDefaultFunctionCall compiles a generic SQL function call.
func (c *Compiler) compileDefaultFunctionCall(fn ast.FunctionCallExpr, distinctStr string) (string, error) {
	var argStrings []string
	for _, arg := range fn.Args {
		s, err := c.visitExpression(arg)
		if err != nil {
			return "", err
		}
		argStrings = append(argStrings, s)
	}
	return fn.Name + "(" + distinctStr + strings.Join(argStrings, ", ") + ")", nil
}

func (c *Compiler) renderEntityProperties(expr string) string {
	if c.schema.Dialect.Name() == "sqlite" {
		return "json(" + expr + ")"
	}
	return expr
}
