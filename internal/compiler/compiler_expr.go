package compiler

import (
	"fmt"
	"github.com/vmikhailov/cypher-sql-go/internal/ast"
	"strconv"
	"strings"
)

func (c *Compiler) visitExpression(expr ast.Expression) (string, error) {
	if expr == nil {
		return "NULL", nil
	}

	switch e := expr.(type) {
	case ast.IdentifierExpr:
		return c.visitIdentifier(e), nil
	case ast.PropertyAccessExpr:
		return c.visitPropertyAccess(e), nil
	case ast.LiteralExpr:
		return c.visitLiteral(e), nil
	case ast.ParameterExpr:
		idx, exists := c.paramSlots[e.Name]
		if !exists {
			c.paramIndex++
			idx = c.paramIndex
			c.paramSlots[e.Name] = idx
			c.paramOrder = append(c.paramOrder, e.Name)
		}
		return c.schema.Dialect.ParamPlaceholder(e.Name, idx), nil
	case ast.BinaryExpr:
		return c.visitBinary(e)
	case ast.UnaryExpr:
		return c.visitUnary(e)
	case ast.FunctionCallExpr:
		return c.visitFunctionCall(e)
	case ast.ListExpr:
		return c.visitList(e)
	case ast.MapExpr:
		return c.visitMap(e)
	case ast.WildcardExpr:
		return "*", nil
	case ast.PatternExpr:
		return c.visitPatternExpr(e)
	case ast.PatternComprehensionExpr:
		return c.visitPatternComprehension(e)
	case ast.ListPredicateExpr:
		return c.visitListPredicate(e)
	case ast.ListComprehensionExpr:
		return c.visitListComprehension(e)
	case ast.CaseExpr:
		return c.visitCase(e)
	case ast.HasLabelExpr:
		return c.visitHasLabel(e)
	case ast.MapProjectionExpr:
		return c.visitMapProjection(e)
	case ast.ReduceExpr:
		return c.visitReduce(e)
	case ast.ListSliceExpr:
		return c.visitListSlice(e)
	case ast.ExistsSubqueryExpr:
		return c.visitExistsSubquery(e)
	case ast.CountSubqueryExpr:
		return c.visitCountSubquery(e)
	default:
		return "", fmt.Errorf("unsupported expression type: %T", expr)
	}
}

func (c *Compiler) visitIdentifier(id ast.IdentifierExpr) string {
	if aliasSQL, ok := c.withAliases[id.Name]; ok {
		return aliasSQL
	}
	escaped := c.escapeVar(id.Name)
	if c.unwindVariables[id.Name] {
		if c.schema.Dialect.Name() == "clickhouse" {
			return escaped
		}
		return escaped + ".value"
	}
	if c.declaredNodes[id.Name] {
		return c.schema.Dialect.JSONObject([]KeyValuePair{
			{Key: "id", Value: c.getNodeIDSQL(id.Name)},
			{Key: "kind", Value: c.getNodeKindSQL(id.Name)},
			{Key: "properties", Value: c.renderEntityProperties(c.getNodePropsSQL(id.Name))},
		})
	}
	if c.varLenRels[id.Name] {
		return c.schema.Dialect.JSONObject([]KeyValuePair{
			{Key: "from", Value: escaped + ".start_id"},
			{Key: "to", Value: escaped + ".end_id"},
			{Key: "depth", Value: escaped + ".depth"},
		})
	}
	if c.declaredRels[id.Name] {
		return c.schema.Dialect.JSONObject([]KeyValuePair{
			{Key: "type", Value: escaped + "." + c.schema.EdgeKindCol},
			{Key: "from", Value: escaped + "." + c.schema.EdgeFromCol},
			{Key: "to", Value: escaped + "." + c.schema.EdgeToCol},
			{Key: "properties", Value: c.renderEntityProperties(escaped + "." + c.schema.EdgePropsCol)},
		})
	}
	return escaped
}

func (c *Compiler) visitPropertyAccess(prop ast.PropertyAccessExpr) string {
	return c.visitPropertyAccessTyped(prop, false)
}

func (c *Compiler) visitPropertyAccessTyped(prop ast.PropertyAccessExpr, numeric bool) string {
	v := c.escapeVar(prop.Variable)
	propName := prop.Property
	extractFn := c.schema.Dialect.JSONExtract
	if numeric {
		extractFn = c.schema.Dialect.JSONExtractNumber
	}

	if c.unwindVariables[prop.Variable] {
		valCol := v + ".value"
		if c.schema.Dialect.Name() == "clickhouse" {
			valCol = v
		}
		if strings.EqualFold(propName, "id") {
			return "COALESCE(" + c.schema.Dialect.JSONExtract(valCol, "id") + ", " + valCol + ")"
		}
		if strings.EqualFold(propName, "kind") {
			return "COALESCE(" + c.schema.Dialect.JSONExtract(valCol, "kind") + ", " + valCol + ")"
		}
		return "COALESCE(" + extractFn(c.schema.Dialect.JSONExtract(valCol, "properties"), propName) + ", " +
			extractFn(valCol, propName) + ")"
	}

	if withSQL, ok := c.withAliases[prop.Variable]; ok {
		return "COALESCE(" + extractFn(c.schema.Dialect.JSONExtract(withSQL, "properties"), propName) + ", " +
			extractFn(withSQL, propName) + ")"
	}

	if c.varLenRels[prop.Variable] {
		switch strings.ToLower(propName) {
		case "from", "from_id", "start_id":
			return v + ".start_id"
		case "to", "to_id", "end_id":
			return v + ".end_id"
		case "depth", "length":
			return v + ".depth"
		default:
			return v + ".depth"
		}
	}

	if c.declaredRels[prop.Variable] {
		switch strings.ToLower(propName) {
		case "id":
			if c.schema.Dialect.Name() == "postgres" || c.schema.Dialect.Name() == "clickhouse" {
				return v + "." + c.schema.EdgeFromCol + " || ':' || " + v + "." + c.schema.EdgeKindCol + " || ':' || " + v + "." + c.schema.EdgeToCol
			}
			return v + ".rowid"
		case "kind", "type":
			return v + "." + c.schema.EdgeKindCol
		case "from", "from_id":
			return v + "." + c.schema.EdgeFromCol
		case "to", "to_id":
			return v + "." + c.schema.EdgeToCol
		case "properties":
			return v + "." + c.schema.EdgePropsCol
		default:
			return extractFn(v+"."+c.schema.EdgePropsCol, propName)
		}
	}

	switch strings.ToLower(propName) {
	case "id":
		return c.getNodeIDSQL(prop.Variable)
	case "kind":
		return c.getNodeKindSQL(prop.Variable)
	case "properties":
		return c.getNodePropsSQL(prop.Variable)
	default:
		return extractFn(c.getNodePropsSQL(prop.Variable), propName)
	}
}

func (c *Compiler) isNumericExpr(expr ast.Expression) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case ast.LiteralExpr:
		return e.Kind == ast.LiteralNumber
	case ast.UnaryExpr:
		return e.Op == ast.OpMinus || e.Op == ast.OpPlus
	case ast.BinaryExpr:
		switch e.Op {
		case ast.OpAdd, ast.OpSub, ast.OpMul, ast.OpDiv, ast.OpMod:
			return true
		}
	case ast.FunctionCallExpr:
		switch strings.ToLower(e.Name) {
		case "count", "sum", "avg", "size", "length", "tointeger", "toint", "tofloat":
			return true
		}
	}
	return false
}

func (c *Compiler) visitExpressionContextual(expr ast.Expression, numeric bool) (string, error) {
	if numeric {
		if prop, ok := expr.(ast.PropertyAccessExpr); ok {
			return c.visitPropertyAccessTyped(prop, true), nil
		}
	}
	return c.visitExpression(expr)
}

func (c *Compiler) visitLiteral(lit ast.LiteralExpr) string {
	switch lit.Kind {
	case ast.LiteralString:
		escaped := strings.ReplaceAll(lit.StrVal, "'", "''")
		return "'" + escaped + "'"
	case ast.LiteralNumber:
		if lit.IsInteger {
			return strconv.FormatInt(int64(lit.NumVal), 10)
		}
		return strconv.FormatFloat(lit.NumVal, 'f', -1, 64)
	case ast.LiteralBool:
		if lit.BoolVal {
			return "1"
		}
		return "0"
	case ast.LiteralNull:
		return "NULL"
	default:
		return "NULL"
	}
}

func (c *Compiler) visitBinary(b ast.BinaryExpr) (string, error) {
	isArith := b.Op == ast.OpAdd || b.Op == ast.OpSub || b.Op == ast.OpMul || b.Op == ast.OpDiv || b.Op == ast.OpMod
	isComp := b.Op == ast.OpEq || b.Op == ast.OpNeq || b.Op == ast.OpLt || b.Op == ast.OpLte || b.Op == ast.OpGt || b.Op == ast.OpGte

	leftNumeric := isArith || (isComp && c.isNumericExpr(b.Right))
	rightNumeric := isArith || (isComp && c.isNumericExpr(b.Left))

	leftSQL, err := c.visitExpressionContextual(b.Left, leftNumeric)
	if err != nil {
		return "", err
	}
	rightSQL, err := c.visitExpressionContextual(b.Right, rightNumeric)
	if err != nil {
		return "", err
	}

	switch b.Op {
	case ast.OpEq:
		return "(" + leftSQL + " = " + rightSQL + ")", nil
	case ast.OpNeq:
		return "(" + leftSQL + " != " + rightSQL + ")", nil
	case ast.OpLt:
		return "(" + leftSQL + " < " + rightSQL + ")", nil
	case ast.OpLte:
		return "(" + leftSQL + " <= " + rightSQL + ")", nil
	case ast.OpGt:
		return "(" + leftSQL + " > " + rightSQL + ")", nil
	case ast.OpGte:
		return "(" + leftSQL + " >= " + rightSQL + ")", nil
	case ast.OpAnd:
		return "(" + leftSQL + " AND " + rightSQL + ")", nil
	case ast.OpOr:
		return "(" + leftSQL + " OR " + rightSQL + ")", nil
	case ast.OpAdd:
		return "(" + leftSQL + " + " + rightSQL + ")", nil
	case ast.OpSub:
		return "(" + leftSQL + " - " + rightSQL + ")", nil
	case ast.OpMul:
		return "(" + leftSQL + " * " + rightSQL + ")", nil
	case ast.OpDiv:
		return "(" + leftSQL + " / " + rightSQL + ")", nil
	case ast.OpMod:
		return "(" + leftSQL + " % " + rightSQL + ")", nil
	case ast.OpIn:
		return "(" + c.schema.Dialect.InArray(leftSQL, rightSQL) + ")", nil
	case ast.OpStarts:
		pattern := c.compileLikePattern(b.Right, rightSQL)
		return "(" + leftSQL + " LIKE (" + pattern + " || '%') ESCAPE '\\')", nil
	case ast.OpEnds:
		pattern := c.compileLikePattern(b.Right, rightSQL)
		return "(" + leftSQL + " LIKE ('%' || " + pattern + ") ESCAPE '\\')", nil
	case ast.OpContains:
		pattern := c.compileLikePattern(b.Right, rightSQL)
		return "(" + leftSQL + " LIKE ('%' || " + pattern + " || '%') ESCAPE '\\')", nil
	case ast.OpIs:
		return "(" + leftSQL + " IS " + rightSQL + ")", nil
	case ast.OpIsNot:
		return "(" + leftSQL + " IS NOT " + rightSQL + ")", nil
	default:
		return "(" + leftSQL + " " + string(b.Op) + " " + rightSQL + ")", nil
	}
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func (c *Compiler) compileLikePattern(expr ast.Expression, defaultSQL string) string {
	if lit, ok := expr.(ast.LiteralExpr); ok && lit.Kind == ast.LiteralString {
		return "'" + SanitizeSQLLiteral(likeEscaper.Replace(lit.StrVal)) + "'"
	}
	return defaultSQL
}

func (c *Compiler) visitUnary(u ast.UnaryExpr) (string, error) {
	opSQL, err := c.visitExpression(u.Operand)
	if err != nil {
		return "", err
	}
	switch u.Op {
	case ast.OpNot:
		return "(NOT (" + opSQL + "))", nil
	case ast.OpMinus:
		return "(-(" + opSQL + "))", nil
	case ast.OpPlus:
		return "(+(" + opSQL + "))", nil
	default:
		return string(u.Op) + " " + opSQL, nil
	}
}

func (c *Compiler) visitList(l ast.ListExpr) (string, error) {
	var items []string
	for _, it := range l.Items {
		s, err := c.visitExpression(it)
		if err != nil {
			return "", err
		}
		items = append(items, s)
	}
	return c.schema.Dialect.ArrayLiteral(items...), nil
}

func (c *Compiler) visitMap(m ast.MapExpr) (string, error) {
	var pairs []KeyValuePair
	for k, v := range m.Entries {
		vSQL, err := c.visitExpression(v)
		if err != nil {
			return "", err
		}
		pairs = append(pairs, KeyValuePair{Key: SanitizeSQLLiteral(k), Value: vSQL})
	}
	return c.schema.Dialect.JSONObject(pairs), nil
}

func (c *Compiler) visitPatternExpr(pat ast.PatternExpr) (string, error) {
	fromJoins, conditions := c.buildSubqueryPath(pat.Path, "_pe")
	whereSQL := ""
	if len(conditions) > 0 {
		whereSQL = strings.Join(conditions, " AND ")
	}
	return RenderExistsSubquery(&SubqueryModel{
		FromJoins: fromJoins,
		Where:     whereSQL,
	}), nil
}

func (c *Compiler) visitPatternComprehension(pc ast.PatternComprehensionExpr) (string, error) {
	fromJoins, conditions := c.buildSubqueryPath(pc.Path, "_pc")

	var addedNodes []string
	var addedRels []string
	if pc.Path.Head.Variable != "" && !c.declaredNodes[pc.Path.Head.Variable] {
		c.declaredNodes[pc.Path.Head.Variable] = true
		addedNodes = append(addedNodes, pc.Path.Head.Variable)
	}
	for _, elem := range pc.Path.Chain {
		if elem.Relationship.Variable != "" && !c.declaredRels[elem.Relationship.Variable] {
			c.declaredRels[elem.Relationship.Variable] = true
			addedRels = append(addedRels, elem.Relationship.Variable)
		}
		if elem.Target.Variable != "" && !c.declaredNodes[elem.Target.Variable] {
			c.declaredNodes[elem.Target.Variable] = true
			addedNodes = append(addedNodes, elem.Target.Variable)
		}
	}
	defer func() {
		for _, n := range addedNodes {
			delete(c.declaredNodes, n)
		}
		for _, r := range addedRels {
			delete(c.declaredRels, r)
		}
	}()

	if pc.Filter != nil {
		fSQL, err := c.visitExpression(pc.Filter)
		if err != nil {
			return "", err
		}
		if fSQL != "" {
			conditions = append(conditions, fSQL)
		}
	}
	projSQL, err := c.visitExpression(pc.Projection)
	if err != nil {
		return "", err
	}
	whereSQL := ""
	if len(conditions) > 0 {
		whereSQL = strings.Join(conditions, " AND ")
	}
	return c.schema.Dialect.RenderCollectSubquery(&SubqueryModel{
		Projection: projSQL,
		FromJoins:  fromJoins,
		Where:      whereSQL,
	}), nil
}

func (c *Compiler) visitListPredicate(pred ast.ListPredicateExpr) (string, error) {
	listSQL, err := c.visitExpression(pred.List)
	if err != nil {
		return "", err
	}

	c.unwindVariables[pred.Variable] = true
	whereSQL, err := c.visitExpression(pred.Predicate)
	delete(c.unwindVariables, pred.Variable)
	if err != nil {
		return "", err
	}

	return c.schema.Dialect.RenderQuantifier(&QuantifierModel{
		Quantifier: pred.Quantifier,
		List:       listSQL,
		Var:        pred.Variable,
		Predicate:  whereSQL,
	}), nil
}

func (c *Compiler) visitListComprehension(comp ast.ListComprehensionExpr) (string, error) {
	listSQL, err := c.visitExpression(comp.List)
	if err != nil {
		return "", err
	}

	c.unwindVariables[comp.Variable] = true
	var filterSQL string
	if comp.Filter != nil {
		fSQL, err := c.visitExpression(comp.Filter)
		if err == nil && fSQL != "" {
			filterSQL = fSQL
		}
	}
	projSQL := comp.Variable + ".value"
	if c.schema.Dialect.Name() == "clickhouse" {
		projSQL = comp.Variable
	}
	if comp.Projection != nil {
		pSQL, err := c.visitExpression(comp.Projection)
		if err == nil {
			projSQL = pSQL
		}
	}
	delete(c.unwindVariables, comp.Variable)

	return c.schema.Dialect.RenderListComprehension(&ListCompModel{
		List:       listSQL,
		Var:        comp.Variable,
		Filter:     filterSQL,
		Projection: projSQL,
	}), nil
}

func (c *Compiler) visitCase(caseExpr ast.CaseExpr) (string, error) {
	var sb strings.Builder
	sb.WriteString("CASE")
	if caseExpr.Test != nil {
		tSQL, err := c.visitExpression(caseExpr.Test)
		if err == nil {
			sb.WriteString(" ")
			sb.WriteString(tSQL)
		}
	}
	for _, branch := range caseExpr.WhenBranches {
		wSQL, err := c.visitExpression(branch.When)
		if err != nil {
			return "", err
		}
		tSQL, err := c.visitExpression(branch.Then)
		if err != nil {
			return "", err
		}
		sb.WriteString(" WHEN ")
		sb.WriteString(wSQL)
		sb.WriteString(" THEN ")
		sb.WriteString(tSQL)
	}
	if caseExpr.Else != nil {
		eSQL, err := c.visitExpression(caseExpr.Else)
		if err != nil {
			return "", err
		}
		sb.WriteString(" ELSE ")
		sb.WriteString(eSQL)
	}
	sb.WriteString(" END")
	return sb.String(), nil
}

func (c *Compiler) visitHasLabel(hl ast.HasLabelExpr) (string, error) {
	varName := ""
	if id, ok := hl.Node.(ast.IdentifierExpr); ok {
		varName = id.Name
	} else {
		s, err := c.visitExpression(hl.Node)
		if err != nil {
			return "", err
		}
		varName = s
	}
	nVar := c.escapeVar(varName)
	return c.compileNodeLabelPredicate(nVar, hl.Label), nil
}

func (c *Compiler) visitMapProjection(mp ast.MapProjectionExpr) (string, error) {
	baseVar := ""
	isDeclaredNode := false
	rawVar := ""
	if id, ok := mp.Base.(ast.IdentifierExpr); ok {
		rawVar = id.Name
		baseVar = c.escapeVar(id.Name)
		isDeclaredNode = c.declaredNodes[id.Name]
	} else {
		s, err := c.visitExpression(mp.Base)
		if err != nil {
			return "", err
		}
		baseVar = s
	}

	var pairs []KeyValuePair
	for _, elem := range mp.Elements {
		if elem.IsAllProps {
			continue
		}
		if elem.Value != nil {
			valSQL, err := c.visitExpression(elem.Value)
			if err != nil {
				return "", err
			}
			pairs = append(pairs, KeyValuePair{Key: elem.PropertyName, Value: valSQL})
		} else {
			propSQL := ""
			switch strings.ToLower(elem.PropertyName) {
			case strings.ToLower(c.schema.NodeIDCol), "id":
				if isDeclaredNode {
					propSQL = c.getNodeIDSQL(rawVar)
				} else {
					propSQL = baseVar + "." + c.schema.NodeIDCol
				}
			case strings.ToLower(c.schema.NodeKindCol), "kind":
				if isDeclaredNode {
					propSQL = c.getNodeKindSQL(rawVar)
				} else {
					propSQL = baseVar + "." + c.schema.NodeKindCol
				}
			default:
				if isDeclaredNode {
					propSQL = c.schema.Dialect.JSONExtract(c.getNodePropsSQL(rawVar), elem.PropertyName)
				} else {
					propSQL = c.schema.Dialect.JSONExtract(baseVar+"."+c.schema.NodePropsCol, elem.PropertyName)
				}
			}
			pairs = append(pairs, KeyValuePair{Key: elem.PropertyName, Value: propSQL})
		}
	}
	return c.schema.Dialect.JSONObject(pairs), nil
}

func (c *Compiler) visitReduce(red ast.ReduceExpr) (string, error) {
	listSQL, err := c.visitExpression(red.List)
	if err != nil {
		return "", err
	}
	initSQL, err := c.visitExpression(red.Initial)
	if err != nil {
		return "", err
	}
	if c.schema.Dialect.Name() == "clickhouse" {
		trimmed := strings.TrimSpace(listSQL)
		if strings.HasPrefix(trimmed, "[") {
			return "(" + initSQL + " + arraySum(" + listSQL + "))", nil
		}
		return "(" + initSQL + " + arraySum(JSONExtract(" + listSQL + ", 'Array(Float64)')))", nil
	}
	return "(" + initSQL + " + COALESCE((SELECT sum(value) FROM json_each(" + listSQL + ")), 0))", nil
}

func tryGetIntLiteral(expr ast.Expression) (int64, bool) {
	if expr == nil {
		return 0, false
	}
	if lit, ok := expr.(ast.LiteralExpr); ok && lit.Kind == ast.LiteralNumber && lit.IsInteger {
		return int64(lit.NumVal), true
	}
	if u, ok := expr.(ast.UnaryExpr); ok && u.Op == ast.OpMinus {
		if lit, ok := u.Operand.(ast.LiteralExpr); ok && lit.Kind == ast.LiteralNumber && lit.IsInteger {
			return -int64(lit.NumVal), true
		}
	}
	return 0, false
}

func (c *Compiler) visitListSlice(slice ast.ListSliceExpr) (string, error) {
	listSQL, err := c.visitExpression(slice.List)
	if err != nil {
		return "", err
	}

	if c.schema.Dialect.Name() == "postgres" {
		var conds []string
		if slice.From != nil {
			fromSQL, err := c.visitExpression(slice.From)
			if err != nil {
				return "", err
			}
			if fromInt, ok := tryGetIntLiteral(slice.From); ok {
				if fromInt < 0 {
					conds = append(conds, fmt.Sprintf("(_ord - 1) >= (jsonb_array_length((%s)::jsonb) + (%d))", listSQL, fromInt))
				} else {
					conds = append(conds, fmt.Sprintf("(_ord - 1) >= %d", fromInt))
				}
			} else {
				conds = append(conds, fmt.Sprintf("(_ord - 1) >= (CASE WHEN (%s) < 0 THEN jsonb_array_length((%s)::jsonb) + (%s) ELSE (%s) END)", fromSQL, listSQL, fromSQL, fromSQL))
			}
		}
		if slice.To != nil {
			toSQL, err := c.visitExpression(slice.To)
			if err != nil {
				return "", err
			}
			if toInt, ok := tryGetIntLiteral(slice.To); ok {
				if toInt < 0 {
					conds = append(conds, fmt.Sprintf("(_ord - 1) < (jsonb_array_length((%s)::jsonb) + (%d))", listSQL, toInt))
				} else {
					conds = append(conds, fmt.Sprintf("(_ord - 1) < %d", toInt))
				}
			} else {
				conds = append(conds, fmt.Sprintf("(_ord - 1) < (CASE WHEN (%s) < 0 THEN jsonb_array_length((%s)::jsonb) + (%s) ELSE (%s) END)", toSQL, listSQL, toSQL, toSQL))
			}
		}
		whereClause := ""
		if len(conds) > 0 {
			whereClause = " WHERE " + strings.Join(conds, " AND ")
		}
		return fmt.Sprintf("COALESCE((SELECT jsonb_agg(_val ORDER BY _ord) FROM jsonb_array_elements((%s)::jsonb) WITH ORDINALITY AS _s(_val, _ord)%s), '[]'::jsonb)", listSQL, whereClause), nil
	}

	var conds []string
	if slice.From != nil {
		fromSQL, err := c.visitExpression(slice.From)
		if err != nil {
			return "", err
		}
		if fromInt, ok := tryGetIntLiteral(slice.From); ok {
			if fromInt < 0 {
				conds = append(conds, fmt.Sprintf("CAST(key AS INTEGER) >= (json_array_length(%s) + (%d))", listSQL, fromInt))
			} else {
				conds = append(conds, fmt.Sprintf("CAST(key AS INTEGER) >= %d", fromInt))
			}
		} else {
			conds = append(conds, fmt.Sprintf("CAST(key AS INTEGER) >= (CASE WHEN (%s) < 0 THEN json_array_length(%s) + (%s) ELSE (%s) END)", fromSQL, listSQL, fromSQL, fromSQL))
		}
	}
	if slice.To != nil {
		toSQL, err := c.visitExpression(slice.To)
		if err != nil {
			return "", err
		}
		if toInt, ok := tryGetIntLiteral(slice.To); ok {
			if toInt < 0 {
				conds = append(conds, fmt.Sprintf("CAST(key AS INTEGER) < (json_array_length(%s) + (%d))", listSQL, toInt))
			} else {
				conds = append(conds, fmt.Sprintf("CAST(key AS INTEGER) < %d", toInt))
			}
		} else {
			conds = append(conds, fmt.Sprintf("CAST(key AS INTEGER) < (CASE WHEN (%s) < 0 THEN json_array_length(%s) + (%s) ELSE (%s) END)", toSQL, listSQL, toSQL, toSQL))
		}
	}
	whereClause := ""
	if len(conds) > 0 {
		whereClause = " WHERE " + strings.Join(conds, " AND ")
	}
	return fmt.Sprintf("COALESCE((SELECT json_group_array(value) FROM json_each(%s)%s), json_array())", listSQL, whereClause), nil
}

func (c *Compiler) visitExistsSubquery(exists ast.ExistsSubqueryExpr) (string, error) {
	return c.compileCorrelatedSubquery(exists.FullQuery, exists.Matches, exists.Where, true)
}

func (c *Compiler) visitCountSubquery(count ast.CountSubqueryExpr) (string, error) {
	return c.compileCorrelatedSubquery(count.FullQuery, count.Matches, count.Where, false)
}

func (c *Compiler) compileCorrelatedSubquery(subQuery *ast.Query, matches []ast.MatchClause, whereExpr ast.Expression, isExists bool) (string, error) {
	if subQuery == nil {
		var fromParts []string
		var allConds []string
		var addedNodes []string
		var addedRels []string

		for _, m := range matches {
			for _, p := range m.Paths {
				fj, conds := c.buildSubqueryPath(p, "_sq")
				if fj != "" {
					fromParts = append(fromParts, fj)
				}
				allConds = append(allConds, conds...)

				if p.Head.Variable != "" && !c.declaredNodes[p.Head.Variable] {
					c.declaredNodes[p.Head.Variable] = true
					addedNodes = append(addedNodes, p.Head.Variable)
				}
				for _, elem := range p.Chain {
					if elem.Relationship.Variable != "" && !c.declaredRels[elem.Relationship.Variable] {
						c.declaredRels[elem.Relationship.Variable] = true
						addedRels = append(addedRels, elem.Relationship.Variable)
					}
					if elem.Target.Variable != "" && !c.declaredNodes[elem.Target.Variable] {
						c.declaredNodes[elem.Target.Variable] = true
						addedNodes = append(addedNodes, elem.Target.Variable)
					}
				}
			}
			if m.Where != nil {
				wSQL, err := c.visitExpression(m.Where)
				if err != nil {
					for _, n := range addedNodes {
						delete(c.declaredNodes, n)
					}
					for _, r := range addedRels {
						delete(c.declaredRels, r)
					}
					return "", err
				}
				if wSQL != "" {
					allConds = append(allConds, wSQL)
				}
			}
		}

		if whereExpr != nil {
			wSQL, err := c.visitExpression(whereExpr)
			if err != nil {
				for _, n := range addedNodes {
					delete(c.declaredNodes, n)
				}
				for _, r := range addedRels {
					delete(c.declaredRels, r)
				}
				return "", err
			}
			if wSQL != "" {
				allConds = append(allConds, wSQL)
			}
		}

		for _, n := range addedNodes {
			delete(c.declaredNodes, n)
		}
		for _, r := range addedRels {
			delete(c.declaredRels, r)
		}

		fromJoins := strings.Join(fromParts, " CROSS JOIN ")
		whereSQL := ""
		if len(allConds) > 0 {
			whereSQL = strings.Join(allConds, " AND ")
		}

		if isExists {
			return RenderExistsSubquery(&SubqueryModel{
				FromJoins: fromJoins,
				Where:     whereSQL,
			}), nil
		}
		return RenderCountSubquery(&SubqueryModel{
			Target:    "*",
			FromJoins: fromJoins,
			Where:     whereSQL,
		}), nil
	}

	qCopy := *subQuery
	if len(qCopy.Return.Items) == 0 {
		qCopy.Return = ast.ReturnClause{
			Items: []ast.ProjectionItem{
				{
					Expression: ast.LiteralExpr{Kind: ast.LiteralNumber, NumVal: 1, IsInteger: true},
				},
			},
		}
	}

	subCompiler := NewCompilerWithOptions(&qCopy, c.initialParams, c.schema)
	subCompiler.varIndex = c.varIndex + 100
	subCompiler.paramIndex = c.paramIndex
	subCompiler.paramSlots = c.paramSlots
	subCompiler.paramOrder = c.paramOrder
	for k, v := range c.declaredNodes {
		subCompiler.declaredNodes[k] = v
	}
	for k, v := range c.declaredRels {
		subCompiler.declaredRels[k] = v
	}
	for k, v := range c.varLenRels {
		subCompiler.varLenRels[k] = v
	}
	for k, v := range c.unwindVariables {
		subCompiler.unwindVariables[k] = v
	}
	for k, v := range c.withAliases {
		subCompiler.withAliases[k] = v
	}
	for k, v := range c.varAliases {
		subCompiler.varAliases[k] = v
	}
	for k, v := range c.nodeIDSource {
		subCompiler.nodeIDSource[k] = v
	}
	for k, v := range c.nodeKindSource {
		subCompiler.nodeKindSource[k] = v
	}
	for k, v := range c.nodePropsSource {
		subCompiler.nodePropsSource[k] = v
	}

	compiledSub, err := subCompiler.Compile()
	if err != nil {
		return "", err
	}
	c.varIndex = subCompiler.varIndex
	c.paramIndex = subCompiler.paramIndex
	c.paramOrder = subCompiler.paramOrder

	if isExists {
		return "EXISTS (" + compiledSub.SQL + ")", nil
	}
	return "(SELECT COUNT(*) FROM (" + compiledSub.SQL + ") _sq)", nil
}

func (c *Compiler) buildSubqueryPath(path ast.PathPattern, prefix string) (string, []string) {
	var conditions []string
	var fromJoins strings.Builder

	rawHeadVar := path.Head.Variable
	headIsOuter := rawHeadVar != "" && c.declaredNodes[rawHeadVar]

	var actualHeadVar string
	if rawHeadVar != "" {
		actualHeadVar = c.escapeVar(rawHeadVar)
	} else {
		c.varIndex++
		actualHeadVar = prefix + "_h" + strconv.Itoa(c.varIndex)
	}

	if !headIsOuter {
		fromJoins.WriteString(c.schema.NodesTable)
		fromJoins.WriteString(" ")
		fromJoins.WriteString(actualHeadVar)
		c.addNodeFiltersToConditions(path.Head, actualHeadVar, &conditions)
	} else if len(path.Chain) == 0 {
		c.varIndex++
		innerHead := prefix + "_h" + strconv.Itoa(c.varIndex)
		fromJoins.WriteString(c.schema.NodesTable)
		fromJoins.WriteString(" ")
		fromJoins.WriteString(innerHead)
		conditions = append(conditions, innerHead+"."+c.schema.NodeIDCol+" = "+c.getNodeIDSQL(rawHeadVar))
		c.addNodeFiltersToConditions(path.Head, innerHead, &conditions)
	}

	prevVar := actualHeadVar
	prevRawVar := rawHeadVar
	prevIsOuter := headIsOuter

	for _, elem := range path.Chain {
		rawRelVar := elem.Relationship.Variable
		var relVar string
		if rawRelVar != "" && !c.declaredRels[rawRelVar] {
			relVar = c.escapeVar(rawRelVar)
		} else {
			c.varIndex++
			relVar = prefix + "_r" + strconv.Itoa(c.varIndex)
		}

		targetNode := elem.Target
		rawTargetVar := targetNode.Variable
		targetIsOuter := rawTargetVar != "" && c.declaredNodes[rawTargetVar]

		var actualTargetVar string
		if rawTargetVar != "" {
			actualTargetVar = c.escapeVar(rawTargetVar)
		} else {
			c.varIndex++
			actualTargetVar = prefix + "_t" + strconv.Itoa(c.varIndex)
		}

		if !targetIsOuter {
			if fromJoins.Len() == 0 {
				fromJoins.WriteString(c.schema.EdgesTable)
				fromJoins.WriteString(" ")
				fromJoins.WriteString(relVar)
				fromJoins.WriteString(" CROSS JOIN ")
				fromJoins.WriteString(c.schema.NodesTable)
				fromJoins.WriteString(" ")
				fromJoins.WriteString(actualTargetVar)
			} else {
				fromJoins.WriteString(" CROSS JOIN ")
				fromJoins.WriteString(c.schema.EdgesTable)
				fromJoins.WriteString(" ")
				fromJoins.WriteString(relVar)
				fromJoins.WriteString(" CROSS JOIN ")
				fromJoins.WriteString(c.schema.NodesTable)
				fromJoins.WriteString(" ")
				fromJoins.WriteString(actualTargetVar)
			}
			c.addNodeFiltersToConditions(targetNode, actualTargetVar, &conditions)
		} else {
			if fromJoins.Len() == 0 {
				fromJoins.WriteString(c.schema.EdgesTable)
				fromJoins.WriteString(" ")
				fromJoins.WriteString(relVar)
			} else {
				fromJoins.WriteString(" CROSS JOIN ")
				fromJoins.WriteString(c.schema.EdgesTable)
				fromJoins.WriteString(" ")
				fromJoins.WriteString(relVar)
			}
			c.addNodeFiltersToConditions(targetNode, actualTargetVar, &conditions)
		}

		prevIDSrc := prevVar + "." + c.schema.NodeIDCol
		if prevIsOuter && prevRawVar != "" {
			prevIDSrc = c.getNodeIDSQL(prevRawVar)
		}
		targetIDSrc := actualTargetVar + "." + c.schema.NodeIDCol
		if targetIsOuter && rawTargetVar != "" {
			targetIDSrc = c.getNodeIDSQL(rawTargetVar)
		}

		switch elem.Relationship.Direction {
		case ast.DirectionOutgoing:
			conditions = append(conditions,
				relVar+"."+c.schema.EdgeFromCol+" = "+prevIDSrc)
			conditions = append(conditions,
				targetIDSrc+" = "+relVar+"."+c.schema.EdgeToCol)
		case ast.DirectionIncoming:
			conditions = append(conditions,
				relVar+"."+c.schema.EdgeToCol+" = "+prevIDSrc)
			conditions = append(conditions,
				targetIDSrc+" = "+relVar+"."+c.schema.EdgeFromCol)
		case ast.DirectionUndirected:
			conditions = append(conditions,
				"("+relVar+"."+c.schema.EdgeFromCol+" = "+prevIDSrc+" OR "+
					relVar+"."+c.schema.EdgeToCol+" = "+prevIDSrc+")")
			conditions = append(conditions,
				targetIDSrc+" = CASE WHEN "+relVar+"."+c.schema.EdgeFromCol+" = "+prevIDSrc+
					" THEN "+relVar+"."+c.schema.EdgeToCol+" ELSE "+relVar+"."+c.schema.EdgeFromCol+" END")
		}

		if len(elem.Relationship.Types) == 1 {
			conditions = append(conditions,
				relVar+"."+c.schema.EdgeKindCol+" = '"+SanitizeSQLLiteral(elem.Relationship.Types[0])+"'")
		} else if len(elem.Relationship.Types) > 1 {
			var escapedTypes []string
			for _, t := range elem.Relationship.Types {
				escapedTypes = append(escapedTypes, SanitizeSQLLiteral(t))
			}
			conditions = append(conditions,
				relVar+"."+c.schema.EdgeKindCol+" IN ('"+strings.Join(escapedTypes, "', '")+"')")
		}

		if elem.Relationship.Properties != nil {
			for k, v := range elem.Relationship.Properties {
				valSQL, err := c.visitExpression(v)
				if err == nil {
					switch strings.ToLower(k) {
					case strings.ToLower(c.schema.EdgeKindCol), "kind", "type":
						conditions = append(conditions, relVar+"."+c.schema.EdgeKindCol+" = "+valSQL)
					case strings.ToLower(c.schema.EdgeFromCol), "from", "from_id":
						conditions = append(conditions, relVar+"."+c.schema.EdgeFromCol+" = "+valSQL)
					case strings.ToLower(c.schema.EdgeToCol), "to", "to_id":
						conditions = append(conditions, relVar+"."+c.schema.EdgeToCol+" = "+valSQL)
					default:
						conditions = append(conditions,
							c.schema.Dialect.JSONExtract(relVar+"."+c.schema.EdgePropsCol, k)+" = "+valSQL)
					}
				}
			}
		}

		prevVar = actualTargetVar
		prevRawVar = rawTargetVar
		prevIsOuter = targetIsOuter
	}

	return fromJoins.String(), conditions
}

func (c *Compiler) hasAggregation(expr ast.Expression) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case ast.FunctionCallExpr:
		lower := strings.ToLower(e.Name)
		if lower == "count" || lower == "collect" || lower == "sum" || lower == "avg" || lower == "min" || lower == "max" {
			return true
		}
		for _, arg := range e.Args {
			if c.hasAggregation(arg) {
				return true
			}
		}
		return false
	case ast.BinaryExpr:
		return c.hasAggregation(e.Left) || c.hasAggregation(e.Right)
	case ast.UnaryExpr:
		return c.hasAggregation(e.Operand)
	case ast.ListExpr:
		for _, item := range e.Items {
			if c.hasAggregation(item) {
				return true
			}
		}
		return false
	case ast.ListSliceExpr:
		return c.hasAggregation(e.List) || c.hasAggregation(e.From) || c.hasAggregation(e.To)
	case ast.CaseExpr:
		if c.hasAggregation(e.Test) || c.hasAggregation(e.Else) {
			return true
		}
		for _, w := range e.WhenBranches {
			if c.hasAggregation(w.When) || c.hasAggregation(w.Then) {
				return true
			}
		}
		return false
	case ast.ListComprehensionExpr:
		return c.hasAggregation(e.List) || c.hasAggregation(e.Filter) || c.hasAggregation(e.Projection)
	case ast.ListPredicateExpr:
		return c.hasAggregation(e.List) || c.hasAggregation(e.Predicate)
	case ast.ReduceExpr:
		return c.hasAggregation(e.Initial) || c.hasAggregation(e.List) || c.hasAggregation(e.Expression)
	case ast.MapExpr:
		for _, v := range e.Entries {
			if c.hasAggregation(v) {
				return true
			}
		}
		return false
	case ast.MapProjectionExpr:
		if c.hasAggregation(e.Base) {
			return true
		}
		for _, el := range e.Elements {
			if c.hasAggregation(el.Value) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func (c *Compiler) getReferencedIdentifiers(expr ast.Expression) map[string]bool {
	set := make(map[string]bool)
	c.collectIdentifiers(expr, set)
	return set
}

func (c *Compiler) collectIdentifiers(expr ast.Expression, set map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case ast.IdentifierExpr:
		set[e.Name] = true
	case ast.PropertyAccessExpr:
		set[e.Variable] = true
	case ast.BinaryExpr:
		c.collectIdentifiers(e.Left, set)
		c.collectIdentifiers(e.Right, set)
	case ast.UnaryExpr:
		c.collectIdentifiers(e.Operand, set)
	case ast.FunctionCallExpr:
		for _, a := range e.Args {
			c.collectIdentifiers(a, set)
		}
	case ast.ListExpr:
		for _, a := range e.Items {
			c.collectIdentifiers(a, set)
		}
	case ast.ListSliceExpr:
		c.collectIdentifiers(e.List, set)
		c.collectIdentifiers(e.From, set)
		c.collectIdentifiers(e.To, set)
	case ast.CaseExpr:
		c.collectIdentifiers(e.Test, set)
		for _, w := range e.WhenBranches {
			c.collectIdentifiers(w.When, set)
			c.collectIdentifiers(w.Then, set)
		}
		c.collectIdentifiers(e.Else, set)
	case ast.ListComprehensionExpr:
		c.collectIdentifiers(e.List, set)
		c.collectIdentifiers(e.Filter, set)
		c.collectIdentifiers(e.Projection, set)
	case ast.ListPredicateExpr:
		c.collectIdentifiers(e.List, set)
		c.collectIdentifiers(e.Predicate, set)
	case ast.ReduceExpr:
		c.collectIdentifiers(e.Initial, set)
		c.collectIdentifiers(e.List, set)
		c.collectIdentifiers(e.Expression, set)
	case ast.MapExpr:
		for _, v := range e.Entries {
			c.collectIdentifiers(v, set)
		}
	case ast.MapProjectionExpr:
		c.collectIdentifiers(e.Base, set)
		for _, el := range e.Elements {
			c.collectIdentifiers(el.Value, set)
		}
	case ast.HasLabelExpr:
		c.collectIdentifiers(e.Node, set)
	case ast.PatternComprehensionExpr:
		if e.Path.Head.Variable != "" {
			set[e.Path.Head.Variable] = true
		}
		for _, elem := range e.Path.Chain {
			if elem.Relationship.Variable != "" {
				set[elem.Relationship.Variable] = true
			}
			if elem.Target.Variable != "" {
				set[elem.Target.Variable] = true
			}
		}
		c.collectIdentifiers(e.Filter, set)
		c.collectIdentifiers(e.Projection, set)
	case ast.ExistsSubqueryExpr:
		c.collectSubqueryIdentifiers(e.FullQuery, e.Matches, e.Where, set)
	case ast.CountSubqueryExpr:
		c.collectSubqueryIdentifiers(e.FullQuery, e.Matches, e.Where, set)
	}
}

func (c *Compiler) collectSubqueryIdentifiers(subQuery *ast.Query, matches []ast.MatchClause, whereExpr ast.Expression, set map[string]bool) {
	for _, m := range matches {
		for _, p := range m.Paths {
			if p.Head.Variable != "" {
				set[p.Head.Variable] = true
			}
			for _, elem := range p.Chain {
				if elem.Relationship.Variable != "" {
					set[elem.Relationship.Variable] = true
				}
				if elem.Target.Variable != "" {
					set[elem.Target.Variable] = true
				}
			}
		}
		c.collectIdentifiers(m.Where, set)
	}
	c.collectIdentifiers(whereExpr, set)
	if subQuery != nil {
		for _, m := range subQuery.Matches {
			for _, p := range m.Paths {
				if p.Head.Variable != "" {
					set[p.Head.Variable] = true
				}
				for _, elem := range p.Chain {
					if elem.Relationship.Variable != "" {
						set[elem.Relationship.Variable] = true
					}
					if elem.Target.Variable != "" {
						set[elem.Target.Variable] = true
					}
				}
			}
			c.collectIdentifiers(m.Where, set)
		}
		c.collectIdentifiers(subQuery.Where, set)
	}
}
