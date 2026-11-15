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
				return c.renderEntityProperties(c.getNodePropsSQL(id.Name)), true, nil
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
			if c.declaredNodes[id.Name] {
				return c.schema.Dialect.ArrayLiteral(c.getNodeKindSQL(id.Name)), true, nil
			}
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
				propsSQL := c.getNodePropsSQL(id.Name)
				if c.schema.Dialect.Name() == "clickhouse" {
					return "JSONExtractKeys(" + propsSQL + ")", true, nil
				}
				return "(SELECT json_group_array(key) FROM json_each(" + propsSQL + "))", true, nil
			}
			if c.declaredRels[id.Name] {
				if c.schema.Dialect.Name() == "clickhouse" {
					return "JSONExtractKeys(" + v + "." + c.schema.EdgePropsCol + ")", true, nil
				}
				return "(SELECT json_group_array(key) FROM json_each(" + v + "." + c.schema.EdgePropsCol + "))", true, nil
			}
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		if c.schema.Dialect.Name() == "clickhouse" {
			return "JSONExtractKeys(" + argSQL + ")", true, nil
		}
		return "(SELECT json_group_array(key) FROM json_each(" + argSQL + "))", true, nil

	case "nodes":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("nodes() takes exactly 1 argument, got %d", len(fn.Args))
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			v := c.escapeVar(id.Name)
			return c.schema.Dialect.ArrayLiteral(v+".start_id", v+".end_id"), true, nil
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
				return "COUNT(" + distinctStr + c.getNodeIDSQL(id.Name) + ")", true, nil
			}
			if c.declaredRels[id.Name] {
				if c.varLenRels[id.Name] {
					return "COUNT(" + distinctStr + c.escapeVar(id.Name) + ".depth)", true, nil
				}
				if c.schema.Dialect.Name() == "clickhouse" {
					return "COUNT(" + distinctStr + c.escapeVar(id.Name) + "." + c.schema.EdgeFromCol + ")", true, nil
				}
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

	case "sum", "avg", "min", "max":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("%s() takes exactly 1 argument, got %d", fn.Name, len(fn.Args))
		}
		numeric := lower == "sum" || lower == "avg"
		argSQL, err := c.visitExpressionContextual(fn.Args[0], numeric)
		if err != nil {
			return "", true, err
		}
		return fn.Name + "(" + distinctStr + argSQL + ")", true, nil

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
		if c.schema.Dialect.Name() == "clickhouse" {
			return "toString(" + argSQL + ")", true, nil
		}
		return "CAST(" + argSQL + " AS TEXT)", true, nil

	case "tointeger", "toint":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("%s() takes exactly 1 argument, got %d", fn.Name, len(fn.Args))
		}
		argSQL, err := c.visitExpressionContextual(fn.Args[0], true)
		if err != nil {
			return "", true, err
		}
		if c.schema.Dialect.Name() == "clickhouse" {
			return "toInt64OrZero(" + argSQL + ")", true, nil
		}
		return "CAST(" + argSQL + " AS INTEGER)", true, nil

	case "tofloat":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("toFloat() takes exactly 1 argument, got %d", len(fn.Args))
		}
		argSQL, err := c.visitExpressionContextual(fn.Args[0], true)
		if err != nil {
			return "", true, err
		}
		if c.schema.Dialect.Name() == "clickhouse" {
			return "toFloat64OrZero(" + argSQL + ")", true, nil
		}
		return "CAST(" + argSQL + " AS REAL)", true, nil

	case "length":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("length() takes exactly 1 argument, got %d", len(fn.Args))
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			v := c.escapeVar(id.Name)
			if c.varLenRels[id.Name] || c.varLenRels[v] || c.declaredRels[id.Name] || (c.varAliases != nil && c.varAliases[id.Name] != "") {
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
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			v := c.escapeVar(id.Name)
			if c.varLenRels[id.Name] || c.varLenRels[v] {
				return v + ".depth", true, nil
			}
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		return c.schema.Dialect.ArrayLength(argSQL), true, nil

	case "item_at":
		if len(fn.Args) != 2 {
			return "", true, fmt.Errorf("item_at() takes exactly 2 arguments, got %d", len(fn.Args))
		}
		if propLit, ok := fn.Args[1].(ast.LiteralExpr); ok && propLit.Kind == ast.LiteralString {
			propName := propLit.StrVal
			if innerFn, ok := fn.Args[0].(ast.FunctionCallExpr); ok && len(innerFn.Args) == 1 {
				innerLower := strings.ToLower(innerFn.Name)
				if innerLower == "startnode" || innerLower == "start_node" || innerLower == "endnode" || innerLower == "end_node" {
					if relId, ok := innerFn.Args[0].(ast.IdentifierExpr); ok && c.declaredRels[relId.Name] {
						colName := c.schema.EdgeFromCol
						if innerLower == "endnode" || innerLower == "end_node" {
							colName = c.schema.EdgeToCol
						}
						nodeIDExpr := c.escapeVar(relId.Name) + "." + colName
						switch strings.ToLower(propName) {
						case "id", strings.ToLower(c.schema.NodeIDCol):
							return nodeIDExpr, true, nil
						case "kind", "label", strings.ToLower(c.schema.NodeKindCol):
							return fmt.Sprintf("(SELECT _sn.%s FROM %s _sn WHERE _sn.%s = %s)",
								c.schema.NodeKindCol, c.schema.NodesTable, c.schema.NodeIDCol, nodeIDExpr), true, nil
						case "properties", strings.ToLower(c.schema.NodePropsCol):
							return fmt.Sprintf("(SELECT _sn.%s FROM %s _sn WHERE _sn.%s = %s)",
								c.schema.NodePropsCol, c.schema.NodesTable, c.schema.NodeIDCol, nodeIDExpr), true, nil
						default:
							return fmt.Sprintf("(SELECT %s FROM %s _sn WHERE _sn.%s = %s)",
								c.schema.Dialect.JSONExtract("_sn."+c.schema.NodePropsCol, propName),
								c.schema.NodesTable, c.schema.NodeIDCol, nodeIDExpr), true, nil
						}
					}
				}
			}
			if varId, ok := fn.Args[0].(ast.IdentifierExpr); ok && (c.declaredNodes[varId.Name] || c.declaredRels[varId.Name]) {
				return c.visitPropertyAccess(ast.PropertyAccessExpr{
					Variable: varId.Name,
					Property: propName,
				}), true, nil
			}
			containerSQL, err := c.visitExpression(fn.Args[0])
			if err != nil {
				return "", true, err
			}
			return c.schema.Dialect.JSONExtract(containerSQL, propName), true, nil
		}

		containerSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		if c.schema.Dialect.Name() == "clickhouse" {
			idxSQL, err := c.visitExpression(fn.Args[1])
			if err != nil {
				return "", true, err
			}
			return fmt.Sprintf("arrayElement(%s, (%s) + 1)", containerSQL, idxSQL), true, nil
		}
		if idxInt, ok := tryGetIntLiteral(fn.Args[1]); ok {
			if idxInt < 0 {
				return fmt.Sprintf("json_extract(%s, '$[#%d]')", containerSQL, idxInt), true, nil
			}
			return fmt.Sprintf("json_extract(%s, '$[%d]')", containerSQL, idxInt), true, nil
		}
		idxSQL, err := c.visitExpression(fn.Args[1])
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("json_extract(%s, '$[' || (%s) || ']')", containerSQL, idxSQL), true, nil

	case "head":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("head() takes exactly 1 argument, got %d", len(fn.Args))
		}
		listSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		if c.schema.Dialect.Name() == "clickhouse" {
			return "arrayElement(" + listSQL + ", 1)", true, nil
		}
		return "json_extract(" + listSQL + ", '$[0]')", true, nil

	case "last":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("last() takes exactly 1 argument, got %d", len(fn.Args))
		}
		listSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", true, err
		}
		if c.schema.Dialect.Name() == "clickhouse" {
			return "arrayElement(" + listSQL + ", -1)", true, nil
		}
		return "json_extract(" + listSQL + ", '$[#-1]')", true, nil

	case "tail":
		if len(fn.Args) != 1 {
			return "", true, fmt.Errorf("tail() takes exactly 1 argument, got %d", len(fn.Args))
		}
		sql, err := c.visitListSlice(ast.ListSliceExpr{
			List: fn.Args[0],
			From: ast.LiteralExpr{Kind: ast.LiteralNumber, NumVal: 1, IsInteger: true},
		})
		return sql, true, err

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
