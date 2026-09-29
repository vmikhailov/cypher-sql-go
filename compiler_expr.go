package cyphersql

import (
	"fmt"
	"strings"
)

func (c *Compiler) visitExpression(expr Expression) (string, error) {
	if expr == nil {
		return "NULL", nil
	}

	switch e := expr.(type) {
	case IdentifierExpr:
		return c.visitIdentifier(e), nil
	case PropertyAccessExpr:
		return c.visitPropertyAccess(e), nil
	case LiteralExpr:
		return c.visitLiteral(e), nil
	case ParameterExpr:
		return "@" + e.Name, nil
	case BinaryExpr:
		return c.visitBinary(e)
	case UnaryExpr:
		return c.visitUnary(e)
	case FunctionCallExpr:
		return c.visitFunctionCall(e)
	case ListExpr:
		return c.visitList(e)
	case MapExpr:
		return c.visitMap(e)
	case WildcardExpr:
		return "*", nil
	case PatternExpr:
		return c.visitPatternExpr(e)
	case PatternComprehensionExpr:
		return c.visitPatternComprehension(e)
	case ListPredicateExpr:
		return c.visitListPredicate(e)
	case ListComprehensionExpr:
		return c.visitListComprehension(e)
	case CaseExpr:
		return c.visitCase(e)
	default:
		return "", fmt.Errorf("unsupported expression type: %T", expr)
	}
}

func (c *Compiler) visitIdentifier(id IdentifierExpr) string {
	if aliasSQL, ok := c.withAliases[id.Name]; ok {
		return aliasSQL
	}
	escaped := c.escapeVar(id.Name)
	if c.unwindVariables[id.Name] {
		return fmt.Sprintf("%s.value", escaped)
	}
	if c.declaredNodes[id.Name] {
		return fmt.Sprintf("json_object('id', %s.id, 'kind', %s.kind, 'properties', json(%s.properties))", escaped, escaped, escaped)
	}
	if c.declaredRels[id.Name] {
		return fmt.Sprintf("json_object('type', %s.kind, 'from', %s.from_id, 'to', %s.to_id, 'properties', json(%s.properties))", escaped, escaped, escaped, escaped)
	}
	return escaped
}

func (c *Compiler) visitPropertyAccess(prop PropertyAccessExpr) string {
	v := c.escapeVar(prop.Variable)
	propName := prop.Property

	if c.unwindVariables[prop.Variable] {
		if strings.EqualFold(propName, "id") {
			return fmt.Sprintf("COALESCE(json_extract(%s.value, '$.id'), %s.value)", v, v)
		}
		if strings.EqualFold(propName, "kind") {
			return fmt.Sprintf("COALESCE(json_extract(%s.value, '$.kind'), %s.value)", v, v)
		}
		return fmt.Sprintf("COALESCE(json_extract(%s.value, '$.properties.%s'), json_extract(%s.value, '$.%s'))", v, propName, v, propName)
	}

	if withSQL, ok := c.withAliases[prop.Variable]; ok {
		return fmt.Sprintf("COALESCE(json_extract(%s, '$.properties.%s'), json_extract(%s, '$.%s'))", withSQL, propName, withSQL, propName)
	}

	if c.declaredRels[prop.Variable] {
		switch strings.ToLower(propName) {
		case "id":
			return fmt.Sprintf("%s.rowid", v)
		case "kind", "type":
			return fmt.Sprintf("%s.kind", v)
		case "from", "from_id":
			return fmt.Sprintf("%s.from_id", v)
		case "to", "to_id":
			return fmt.Sprintf("%s.to_id", v)
		case "properties":
			return fmt.Sprintf("%s.properties", v)
		default:
			return fmt.Sprintf("json_extract(%s.properties, '$.%s')", v, propName)
		}
	}

	switch strings.ToLower(propName) {
	case "id":
		return fmt.Sprintf("%s.id", v)
	case "kind":
		return fmt.Sprintf("%s.kind", v)
	case "properties":
		return fmt.Sprintf("%s.properties", v)
	default:
		return fmt.Sprintf("json_extract(%s.properties, '$.%s')", v, propName)
	}
}

func (c *Compiler) visitLiteral(lit LiteralExpr) string {
	switch lit.Kind {
	case LiteralString:
		escaped := strings.ReplaceAll(lit.StrVal, "'", "''")
		return fmt.Sprintf("'%s'", escaped)
	case LiteralNumber:
		if lit.IsInteger {
			return fmt.Sprintf("%d", int64(lit.NumVal))
		}
		return fmt.Sprintf("%f", lit.NumVal)
	case LiteralBool:
		if lit.BoolVal {
			return "1"
		}
		return "0"
	case LiteralNull:
		return "NULL"
	default:
		return "NULL"
	}
}

func (c *Compiler) visitBinary(b BinaryExpr) (string, error) {
	leftSQL, err := c.visitExpression(b.Left)
	if err != nil {
		return "", err
	}
	rightSQL, err := c.visitExpression(b.Right)
	if err != nil {
		return "", err
	}

	switch b.Op {
	case OpEq:
		return fmt.Sprintf("(%s = %s)", leftSQL, rightSQL), nil
	case OpNeq:
		return fmt.Sprintf("(%s != %s)", leftSQL, rightSQL), nil
	case OpLt:
		return fmt.Sprintf("(%s < %s)", leftSQL, rightSQL), nil
	case OpLte:
		return fmt.Sprintf("(%s <= %s)", leftSQL, rightSQL), nil
	case OpGt:
		return fmt.Sprintf("(%s > %s)", leftSQL, rightSQL), nil
	case OpGte:
		return fmt.Sprintf("(%s >= %s)", leftSQL, rightSQL), nil
	case OpAnd:
		return fmt.Sprintf("(%s AND %s)", leftSQL, rightSQL), nil
	case OpOr:
		return fmt.Sprintf("(%s OR %s)", leftSQL, rightSQL), nil
	case OpAdd:
		return fmt.Sprintf("(%s + %s)", leftSQL, rightSQL), nil
	case OpSub:
		return fmt.Sprintf("(%s - %s)", leftSQL, rightSQL), nil
	case OpMul:
		return fmt.Sprintf("(%s * %s)", leftSQL, rightSQL), nil
	case OpDiv:
		return fmt.Sprintf("(%s / %s)", leftSQL, rightSQL), nil
	case OpMod:
		return fmt.Sprintf("(%s %% %s)", leftSQL, rightSQL), nil
	case OpIn:
		return fmt.Sprintf("(%s IN (SELECT value FROM json_each(%s)))", leftSQL, rightSQL), nil
	case OpStarts:
		return fmt.Sprintf("(%s LIKE (%s || '%%'))", leftSQL, rightSQL), nil
	case OpEnds:
		return fmt.Sprintf("(%s LIKE ('%%' || %s))", leftSQL, rightSQL), nil
	case OpContains:
		return fmt.Sprintf("(%s LIKE ('%%' || %s || '%%'))", leftSQL, rightSQL), nil
	case OpIs:
		return fmt.Sprintf("(%s IS %s)", leftSQL, rightSQL), nil
	case OpIsNot:
		return fmt.Sprintf("(%s IS NOT %s)", leftSQL, rightSQL), nil
	default:
		return fmt.Sprintf("(%s %s %s)", leftSQL, b.Op, rightSQL), nil
	}
}

func (c *Compiler) visitUnary(u UnaryExpr) (string, error) {
	opSQL, err := c.visitExpression(u.Operand)
	if err != nil {
		return "", err
	}
	switch u.Op {
	case OpNot:
		return fmt.Sprintf("(NOT (%s))", opSQL), nil
	case OpMinus:
		return fmt.Sprintf("(-(%s))", opSQL), nil
	case OpPlus:
		return fmt.Sprintf("(+(%s))", opSQL), nil
	default:
		return fmt.Sprintf("%s %s", u.Op, opSQL), nil
	}
}

func (c *Compiler) visitFunctionCall(fn FunctionCallExpr) (string, error) {
	lower := strings.ToLower(fn.Name)
	distinctStr := ""
	if fn.IsDistinct {
		distinctStr = "DISTINCT "
	}

	// 1. Check decomposed optional match aggregation
	if decomposed := c.tryCompileDecomposedAggregation(fn, distinctStr); decomposed != "" {
		return decomposed, nil
	}

	// 2. OpenCypher Relationship & Graph Introspection Functions
	if lower == "type" && len(fn.Args) == 1 {
		if id, ok := fn.Args[0].(IdentifierExpr); ok {
			if c.declaredRels[id.Name] {
				return fmt.Sprintf("%s.kind", c.escapeVar(id.Name)), nil
			}
			if aliasSQL, ok := c.withAliases[id.Name]; ok {
				return fmt.Sprintf("COALESCE(json_extract(%s, '$.type'), json_extract(%s, '$.kind'))", aliasSQL, aliasSQL), nil
			}
		}
		argSQL, _ := c.visitExpression(fn.Args[0])
		return fmt.Sprintf("json_extract(%s, '$.type')", argSQL), nil
	}

	if (lower == "startnode" || lower == "start_node") && len(fn.Args) == 1 {
		if id, ok := fn.Args[0].(IdentifierExpr); ok {
			if c.declaredRels[id.Name] {
				return fmt.Sprintf("%s.from_id", c.escapeVar(id.Name)), nil
			}
			if aliasSQL, ok := c.withAliases[id.Name]; ok {
				return fmt.Sprintf("COALESCE(json_extract(%s, '$.from'), json_extract(%s, '$.from_id'))", aliasSQL, aliasSQL), nil
			}
		}
		argSQL, _ := c.visitExpression(fn.Args[0])
		return fmt.Sprintf("COALESCE(json_extract(%s, '$.from'), json_extract(%s, '$.from_id'))", argSQL, argSQL), nil
	}

	if (lower == "endnode" || lower == "end_node") && len(fn.Args) == 1 {
		if id, ok := fn.Args[0].(IdentifierExpr); ok {
			if c.declaredRels[id.Name] {
				return fmt.Sprintf("%s.to_id", c.escapeVar(id.Name)), nil
			}
			if aliasSQL, ok := c.withAliases[id.Name]; ok {
				return fmt.Sprintf("COALESCE(json_extract(%s, '$.to'), json_extract(%s, '$.to_id'))", aliasSQL, aliasSQL), nil
			}
		}
		argSQL, _ := c.visitExpression(fn.Args[0])
		return fmt.Sprintf("COALESCE(json_extract(%s, '$.to'), json_extract(%s, '$.to_id'))", argSQL, argSQL), nil
	}

	if lower == "properties" && len(fn.Args) == 1 {
		if id, ok := fn.Args[0].(IdentifierExpr); ok {
			if c.declaredNodes[id.Name] || c.declaredRels[id.Name] {
				return fmt.Sprintf("json(%s.properties)", c.escapeVar(id.Name)), nil
			}
			if aliasSQL, ok := c.withAliases[id.Name]; ok {
				return fmt.Sprintf("COALESCE(json_extract(%s, '$.properties'), json(%s))", aliasSQL, aliasSQL), nil
			}
		}
		argSQL, _ := c.visitExpression(fn.Args[0])
		return fmt.Sprintf("json(%s)", argSQL), nil
	}

	if lower == "labels" && len(fn.Args) == 1 {
		if id, ok := fn.Args[0].(IdentifierExpr); ok {
			v := c.escapeVar(id.Name)
			return fmt.Sprintf("CASE WHEN %s.kind IN ('Service', 'App', 'FrontendApp', 'Library', 'SharedLibrary', 'Worker', 'CliTool') THEN json_array(%s.kind, 'Project') ELSE json_array(%s.kind) END", v, v, v), nil
		}
	}

	if lower == "exists" && len(fn.Args) == 1 {
		if pat, ok := fn.Args[0].(PatternExpr); ok {
			return c.visitPatternExpr(pat)
		}
		argSQL, _ := c.visitExpression(fn.Args[0])
		return fmt.Sprintf("(%s IS NOT NULL)", argSQL), nil
	}

	// 3. Aggregations
	if lower == "count" && len(fn.Args) == 1 {
		if _, ok := fn.Args[0].(WildcardExpr); ok {
			return fmt.Sprintf("COUNT(%s*)", distinctStr), nil
		}
		if id, ok := fn.Args[0].(IdentifierExpr); ok {
			if c.declaredNodes[id.Name] {
				return fmt.Sprintf("COUNT(%s%s.id)", distinctStr, c.escapeVar(id.Name)), nil
			}
			if c.declaredRels[id.Name] {
				return fmt.Sprintf("COUNT(%s%s.rowid)", distinctStr, c.escapeVar(id.Name)), nil
			}
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("COUNT(%s%s)", distinctStr, argSQL), nil
	}

	if lower == "collect" && len(fn.Args) == 1 {
		innerSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("json_group_array(%s%s) FILTER (WHERE %s IS NOT NULL)", distinctStr, innerSQL, innerSQL), nil
	}

	// 4. Scalar functions
	if lower == "coalesce" {
		var args []string
		for _, arg := range fn.Args {
			s, err := c.visitExpression(arg)
			if err != nil {
				return "", err
			}
			args = append(args, s)
		}
		return fmt.Sprintf("COALESCE(%s)", strings.Join(args, ", ")), nil
	}

	if lower == "tolower" && len(fn.Args) == 1 {
		argSQL, _ := c.visitExpression(fn.Args[0])
		return fmt.Sprintf("lower(%s)", argSQL), nil
	}
	if lower == "toupper" && len(fn.Args) == 1 {
		argSQL, _ := c.visitExpression(fn.Args[0])
		return fmt.Sprintf("upper(%s)", argSQL), nil
	}
	if lower == "tostring" && len(fn.Args) == 1 {
		argSQL, _ := c.visitExpression(fn.Args[0])
		return fmt.Sprintf("CAST(%s AS TEXT)", argSQL), nil
	}
	if lower == "length" && len(fn.Args) == 1 {
		argSQL, _ := c.visitExpression(fn.Args[0])
		return fmt.Sprintf("length(%s)", argSQL), nil
	}
	if lower == "size" && len(fn.Args) == 1 {
		argSQL, _ := c.visitExpression(fn.Args[0])
		return fmt.Sprintf("CASE WHEN json_valid(%s) THEN json_array_length(%s) ELSE length(%s) END", argSQL, argSQL, argSQL), nil
	}

	// Default function call
	var argStrings []string
	for _, arg := range fn.Args {
		s, err := c.visitExpression(arg)
		if err != nil {
			return "", err
		}
		argStrings = append(argStrings, s)
	}
	return fmt.Sprintf("%s(%s%s)", fn.Name, distinctStr, strings.Join(argStrings, ", ")), nil
}

func (c *Compiler) visitList(l ListExpr) (string, error) {
	var items []string
	for _, it := range l.Items {
		s, err := c.visitExpression(it)
		if err != nil {
			return "", err
		}
		items = append(items, s)
	}
	return fmt.Sprintf("json_array(%s)", strings.Join(items, ", ")), nil
}

func (c *Compiler) visitMap(m MapExpr) (string, error) {
	var pairs []string
	for k, v := range m.Entries {
		vSQL, err := c.visitExpression(v)
		if err != nil {
			return "", err
		}
		pairs = append(pairs, fmt.Sprintf("'%s', %s", k, vSQL))
	}
	return fmt.Sprintf("json_object(%s)", strings.Join(pairs, ", ")), nil
}

func (c *Compiler) visitPatternExpr(pat PatternExpr) (string, error) {
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

func (c *Compiler) visitPatternComprehension(pc PatternComprehensionExpr) (string, error) {
	fromJoins, conditions := c.buildSubqueryPath(pc.Path, "_pc")
	if pc.Filter != nil {
		fSQL, err := c.visitExpression(pc.Filter)
		if err == nil && fSQL != "" {
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
	return RenderCollectSubquery(&SubqueryModel{
		Projection: projSQL,
		FromJoins:  fromJoins,
		Where:      whereSQL,
	}), nil
}

func (c *Compiler) visitListPredicate(pred ListPredicateExpr) (string, error) {
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

	return RenderQuantifier(&QuantifierModel{
		Quantifier: pred.Quantifier,
		List:       listSQL,
		Var:        pred.Variable,
		Predicate:  whereSQL,
	}), nil
}

func (c *Compiler) visitListComprehension(comp ListComprehensionExpr) (string, error) {
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
	projSQL := fmt.Sprintf("%s.value", comp.Variable)
	if comp.Projection != nil {
		pSQL, err := c.visitExpression(comp.Projection)
		if err == nil {
			projSQL = pSQL
		}
	}
	delete(c.unwindVariables, comp.Variable)

	return RenderListComprehension(&ListCompModel{
		List:       listSQL,
		Var:        comp.Variable,
		Filter:     filterSQL,
		Projection: projSQL,
	}), nil
}

func (c *Compiler) visitCase(caseExpr CaseExpr) (string, error) {
	var sb strings.Builder
	sb.WriteString("CASE")
	if caseExpr.Test != nil {
		tSQL, err := c.visitExpression(caseExpr.Test)
		if err == nil {
			sb.WriteString(" " + tSQL)
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
		sb.WriteString(fmt.Sprintf(" WHEN %s THEN %s", wSQL, tSQL))
	}
	if caseExpr.Else != nil {
		eSQL, err := c.visitExpression(caseExpr.Else)
		if err != nil {
			return "", err
		}
		sb.WriteString(" ELSE " + eSQL)
	}
	sb.WriteString(" END")
	return sb.String(), nil
}

func (c *Compiler) buildSubqueryPath(path PathPattern, prefix string) (string, []string) {
	var conditions []string
	var fromJoins strings.Builder

	headVar := path.Head.Variable
	headIsOuter := headVar != "" && c.declaredNodes[headVar]

	actualHeadVar := headVar
	if !headIsOuter {
		c.varIndex++
		actualHeadVar = fmt.Sprintf("%s_h%d", prefix, c.varIndex)
		fromJoins.WriteString(fmt.Sprintf("nodes %s", actualHeadVar))
		c.addNodeFiltersToConditions(path.Head, actualHeadVar, &conditions)
	}

	prevVar := actualHeadVar
	for _, elem := range path.Chain {
		c.varIndex++
		relVar := fmt.Sprintf("%s_r%d", prefix, c.varIndex)
		targetNode := elem.Target
		targetVar := targetNode.Variable
		targetIsOuter := targetVar != "" && c.declaredNodes[targetVar]

		actualTargetVar := targetVar
		if !targetIsOuter {
			if targetVar == "" {
				c.varIndex++
				actualTargetVar = fmt.Sprintf("%s_t%d", prefix, c.varIndex)
			}
			if fromJoins.Len() == 0 {
				fromJoins.WriteString(fmt.Sprintf("edges %s CROSS JOIN nodes %s", relVar, actualTargetVar))
			} else {
				fromJoins.WriteString(fmt.Sprintf(" CROSS JOIN edges %s CROSS JOIN nodes %s", relVar, actualTargetVar))
			}
			c.addNodeFiltersToConditions(targetNode, actualTargetVar, &conditions)
		} else {
			if fromJoins.Len() == 0 {
				fromJoins.WriteString(fmt.Sprintf("edges %s", relVar))
			} else {
				fromJoins.WriteString(fmt.Sprintf(" CROSS JOIN edges %s", relVar))
			}
		}

		prevIDSrc := fmt.Sprintf("%s.id", prevVar)
		targetIDSrc := fmt.Sprintf("%s.id", actualTargetVar)

		switch elem.Relationship.Direction {
		case DirectionOutgoing:
			conditions = append(conditions, fmt.Sprintf("%s.from_id = %s", relVar, prevIDSrc))
			conditions = append(conditions, fmt.Sprintf("%s = %s.to_id", targetIDSrc, relVar))
		case DirectionIncoming:
			conditions = append(conditions, fmt.Sprintf("%s.to_id = %s", relVar, prevIDSrc))
			conditions = append(conditions, fmt.Sprintf("%s = %s.from_id", targetIDSrc, relVar))
		case DirectionUndirected:
			conditions = append(conditions, fmt.Sprintf("(%s.from_id = %s OR %s.to_id = %s)", relVar, prevIDSrc, relVar, prevIDSrc))
			conditions = append(conditions, fmt.Sprintf("%s = CASE WHEN %s.from_id = %s THEN %s.to_id ELSE %s.from_id END",
				targetIDSrc, relVar, prevIDSrc, relVar, relVar))
		}

		if len(elem.Relationship.Types) == 1 {
			conditions = append(conditions, fmt.Sprintf("%s.kind = '%s'", relVar, elem.Relationship.Types[0]))
		} else if len(elem.Relationship.Types) > 1 {
			var quoted []string
			for _, t := range elem.Relationship.Types {
				quoted = append(quoted, fmt.Sprintf("'%s'", t))
			}
			conditions = append(conditions, fmt.Sprintf("%s.kind IN (%s)", relVar, strings.Join(quoted, ", ")))
		}

		prevVar = actualTargetVar
	}

	return fromJoins.String(), conditions
}

func (c *Compiler) hasAggregation(expr Expression) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case FunctionCallExpr:
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
	case BinaryExpr:
		return c.hasAggregation(e.Left) || c.hasAggregation(e.Right)
	case UnaryExpr:
		return c.hasAggregation(e.Operand)
	default:
		return false
	}
}

func (c *Compiler) getReferencedIdentifiers(expr Expression) map[string]bool {
	set := make(map[string]bool)
	c.collectIdentifiers(expr, set)
	return set
}

func (c *Compiler) collectIdentifiers(expr Expression, set map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case IdentifierExpr:
		set[e.Name] = true
	case PropertyAccessExpr:
		set[e.Variable] = true
	case BinaryExpr:
		c.collectIdentifiers(e.Left, set)
		c.collectIdentifiers(e.Right, set)
	case UnaryExpr:
		c.collectIdentifiers(e.Operand, set)
	case FunctionCallExpr:
		for _, a := range e.Args {
			c.collectIdentifiers(a, set)
		}
	case ListExpr:
		for _, a := range e.Items {
			c.collectIdentifiers(a, set)
		}
	}
}
