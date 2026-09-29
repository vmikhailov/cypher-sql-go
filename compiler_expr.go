package cyphersql

import (
	"fmt"
	"strconv"
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
		return escaped + ".value"
	}
	if c.declaredNodes[id.Name] {
		return "json_object('id', " + escaped + ".id, 'kind', " + escaped + ".kind, " +
			"'properties', json(" + escaped + ".properties))"
	}
	if c.declaredRels[id.Name] {
		return "json_object('type', " + escaped + ".kind, 'from', " + escaped + ".from_id, " +
			"'to', " + escaped + ".to_id, 'properties', json(" + escaped + ".properties))"
	}
	return escaped
}

func (c *Compiler) visitPropertyAccess(prop PropertyAccessExpr) string {
	v := c.escapeVar(prop.Variable)
	propName := prop.Property

	if c.unwindVariables[prop.Variable] {
		if strings.EqualFold(propName, "id") {
			return "COALESCE(json_extract(" + v + ".value, '$.id'), " + v + ".value)"
		}
		if strings.EqualFold(propName, "kind") {
			return "COALESCE(json_extract(" + v + ".value, '$.kind'), " + v + ".value)"
		}
		return "COALESCE(json_extract(" + v + ".value, '$.properties." + propName + "'), " +
			"json_extract(" + v + ".value, '$." + propName + "'))"
	}

	if withSQL, ok := c.withAliases[prop.Variable]; ok {
		return "COALESCE(json_extract(" + withSQL + ", '$.properties." + propName + "'), " +
			"json_extract(" + withSQL + ", '$." + propName + "'))"
	}

	if c.declaredRels[prop.Variable] {
		switch strings.ToLower(propName) {
		case "id":
			return v + ".rowid"
		case "kind", "type":
			return v + ".kind"
		case "from", "from_id":
			return v + ".from_id"
		case "to", "to_id":
			return v + ".to_id"
		case "properties":
			return v + ".properties"
		default:
			return "json_extract(" + v + ".properties, '$." + propName + "')"
		}
	}

	switch strings.ToLower(propName) {
	case "id":
		return v + ".id"
	case "kind":
		return v + ".kind"
	case "properties":
		return v + ".properties"
	default:
		return "json_extract(" + v + ".properties, '$." + propName + "')"
	}
}

func (c *Compiler) visitLiteral(lit LiteralExpr) string {
	switch lit.Kind {
	case LiteralString:
		escaped := strings.ReplaceAll(lit.StrVal, "'", "''")
		return "'" + escaped + "'"
	case LiteralNumber:
		if lit.IsInteger {
			return strconv.FormatInt(int64(lit.NumVal), 10)
		}
		return strconv.FormatFloat(lit.NumVal, 'f', -1, 64)
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
		return "(" + leftSQL + " = " + rightSQL + ")", nil
	case OpNeq:
		return "(" + leftSQL + " != " + rightSQL + ")", nil
	case OpLt:
		return "(" + leftSQL + " < " + rightSQL + ")", nil
	case OpLte:
		return "(" + leftSQL + " <= " + rightSQL + ")", nil
	case OpGt:
		return "(" + leftSQL + " > " + rightSQL + ")", nil
	case OpGte:
		return "(" + leftSQL + " >= " + rightSQL + ")", nil
	case OpAnd:
		return "(" + leftSQL + " AND " + rightSQL + ")", nil
	case OpOr:
		return "(" + leftSQL + " OR " + rightSQL + ")", nil
	case OpAdd:
		return "(" + leftSQL + " + " + rightSQL + ")", nil
	case OpSub:
		return "(" + leftSQL + " - " + rightSQL + ")", nil
	case OpMul:
		return "(" + leftSQL + " * " + rightSQL + ")", nil
	case OpDiv:
		return "(" + leftSQL + " / " + rightSQL + ")", nil
	case OpMod:
		return "(" + leftSQL + " % " + rightSQL + ")", nil
	case OpIn:
		return "(" + leftSQL + " IN (SELECT value FROM json_each(" + rightSQL + ")))", nil
	case OpStarts:
		return "(" + leftSQL + " LIKE (" + rightSQL + " || '%'))", nil
	case OpEnds:
		return "(" + leftSQL + " LIKE ('%' || " + rightSQL + "))", nil
	case OpContains:
		return "(" + leftSQL + " LIKE ('%' || " + rightSQL + " || '%'))", nil
	case OpIs:
		return "(" + leftSQL + " IS " + rightSQL + ")", nil
	case OpIsNot:
		return "(" + leftSQL + " IS NOT " + rightSQL + ")", nil
	default:
		return "(" + leftSQL + " " + string(b.Op) + " " + rightSQL + ")", nil
	}
}

func (c *Compiler) visitUnary(u UnaryExpr) (string, error) {
	opSQL, err := c.visitExpression(u.Operand)
	if err != nil {
		return "", err
	}
	switch u.Op {
	case OpNot:
		return "(NOT (" + opSQL + "))", nil
	case OpMinus:
		return "(-(" + opSQL + "))", nil
	case OpPlus:
		return "(+(" + opSQL + "))", nil
	default:
		return string(u.Op) + " " + opSQL, nil
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
				return c.escapeVar(id.Name) + ".kind", nil
			}
			if aliasSQL, ok := c.withAliases[id.Name]; ok {
				return "COALESCE(json_extract(" + aliasSQL + ", '$.type'), json_extract(" + aliasSQL + ", '$.kind'))", nil
			}
		}
		argSQL, _ := c.visitExpression(fn.Args[0])
		return "json_extract(" + argSQL + ", '$.type')", nil
	}

	if (lower == "startnode" || lower == "start_node") && len(fn.Args) == 1 {
		if id, ok := fn.Args[0].(IdentifierExpr); ok {
			if c.declaredRels[id.Name] {
				return c.escapeVar(id.Name) + ".from_id", nil
			}
			if aliasSQL, ok := c.withAliases[id.Name]; ok {
				return "COALESCE(json_extract(" + aliasSQL + ", '$.from'), json_extract(" + aliasSQL + ", '$.from_id'))", nil
			}
		}
		argSQL, _ := c.visitExpression(fn.Args[0])
		return "COALESCE(json_extract(" + argSQL + ", '$.from'), json_extract(" + argSQL + ", '$.from_id'))", nil
	}

	if (lower == "endnode" || lower == "end_node") && len(fn.Args) == 1 {
		if id, ok := fn.Args[0].(IdentifierExpr); ok {
			if c.declaredRels[id.Name] {
				return c.escapeVar(id.Name) + ".to_id", nil
			}
			if aliasSQL, ok := c.withAliases[id.Name]; ok {
				return "COALESCE(json_extract(" + aliasSQL + ", '$.to'), json_extract(" + aliasSQL + ", '$.to_id'))", nil
			}
		}
		argSQL, _ := c.visitExpression(fn.Args[0])
		return "COALESCE(json_extract(" + argSQL + ", '$.to'), json_extract(" + argSQL + ", '$.to_id'))", nil
	}

	if lower == "properties" && len(fn.Args) == 1 {
		if id, ok := fn.Args[0].(IdentifierExpr); ok {
			if c.declaredNodes[id.Name] || c.declaredRels[id.Name] {
				return "json(" + c.escapeVar(id.Name) + ".properties)", nil
			}
			if aliasSQL, ok := c.withAliases[id.Name]; ok {
				return "COALESCE(json_extract(" + aliasSQL + ", '$.properties'), json(" + aliasSQL + "))", nil
			}
		}
		argSQL, _ := c.visitExpression(fn.Args[0])
		return "json(" + argSQL + ")", nil
	}

	if lower == "labels" && len(fn.Args) == 1 {
		if id, ok := fn.Args[0].(IdentifierExpr); ok {
			v := c.escapeVar(id.Name)
			return "CASE WHEN " + v + ".kind IN (" +
				"'Service', 'App', 'FrontendApp', 'Library', 'SharedLibrary', 'Worker', 'CliTool') " +
				"THEN json_array(" + v + ".kind, 'Project') ELSE json_array(" + v + ".kind) END", nil
		}
	}

	if lower == "exists" && len(fn.Args) == 1 {
		if pat, ok := fn.Args[0].(PatternExpr); ok {
			return c.visitPatternExpr(pat)
		}
		argSQL, _ := c.visitExpression(fn.Args[0])
		return "(" + argSQL + " IS NOT NULL)", nil
	}

	// 3. Aggregations
	if lower == "count" && len(fn.Args) == 1 {
		if _, ok := fn.Args[0].(WildcardExpr); ok {
			return "COUNT(" + distinctStr + "*)", nil
		}
		if id, ok := fn.Args[0].(IdentifierExpr); ok {
			if c.declaredNodes[id.Name] {
				return "COUNT(" + distinctStr + c.escapeVar(id.Name) + ".id)", nil
			}
			if c.declaredRels[id.Name] {
				return "COUNT(" + distinctStr + c.escapeVar(id.Name) + ".rowid)", nil
			}
		}
		argSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", err
		}
		return "COUNT(" + distinctStr + argSQL + ")", nil
	}

	if lower == "collect" && len(fn.Args) == 1 {
		innerSQL, err := c.visitExpression(fn.Args[0])
		if err != nil {
			return "", err
		}
		return "json_group_array(" + distinctStr + innerSQL + ") FILTER (WHERE " + innerSQL + " IS NOT NULL)", nil
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
		return "COALESCE(" + strings.Join(args, ", ") + ")", nil
	}

	if lower == "tolower" && len(fn.Args) == 1 {
		argSQL, _ := c.visitExpression(fn.Args[0])
		return "lower(" + argSQL + ")", nil
	}
	if lower == "toupper" && len(fn.Args) == 1 {
		argSQL, _ := c.visitExpression(fn.Args[0])
		return "upper(" + argSQL + ")", nil
	}
	if lower == "tostring" && len(fn.Args) == 1 {
		argSQL, _ := c.visitExpression(fn.Args[0])
		return "CAST(" + argSQL + " AS TEXT)", nil
	}
	if lower == "length" && len(fn.Args) == 1 {
		argSQL, _ := c.visitExpression(fn.Args[0])
		return "length(" + argSQL + ")", nil
	}
	if lower == "size" && len(fn.Args) == 1 {
		argSQL, _ := c.visitExpression(fn.Args[0])
		return "CASE WHEN json_valid(" + argSQL + ") THEN json_array_length(" + argSQL + ") " +
			"ELSE length(" + argSQL + ") END", nil
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
	return fn.Name + "(" + distinctStr + strings.Join(argStrings, ", ") + ")", nil
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
	return "json_array(" + strings.Join(items, ", ") + ")", nil
}

func (c *Compiler) visitMap(m MapExpr) (string, error) {
	var pairs []string
	for k, v := range m.Entries {
		vSQL, err := c.visitExpression(v)
		if err != nil {
			return "", err
		}
		pairs = append(pairs, "'"+k+"', "+vSQL)
	}
	return "json_object(" + strings.Join(pairs, ", ") + ")", nil
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
	projSQL := comp.Variable + ".value"
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
		sb.WriteString(" WHEN " + wSQL + " THEN " + tSQL)
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
		actualHeadVar = prefix + "_h" + strconv.Itoa(c.varIndex)
		fromJoins.WriteString("nodes " + actualHeadVar)
		c.addNodeFiltersToConditions(path.Head, actualHeadVar, &conditions)
	}

	prevVar := actualHeadVar
	for _, elem := range path.Chain {
		c.varIndex++
		relVar := prefix + "_r" + strconv.Itoa(c.varIndex)
		targetNode := elem.Target
		targetVar := targetNode.Variable
		targetIsOuter := targetVar != "" && c.declaredNodes[targetVar]

		actualTargetVar := targetVar
		if !targetIsOuter {
			if targetVar == "" {
				c.varIndex++
				actualTargetVar = prefix + "_t" + strconv.Itoa(c.varIndex)
			}
			if fromJoins.Len() == 0 {
				fromJoins.WriteString("edges " + relVar + " CROSS JOIN nodes " + actualTargetVar)
			} else {
				fromJoins.WriteString(" CROSS JOIN edges " + relVar + " CROSS JOIN nodes " + actualTargetVar)
			}
			c.addNodeFiltersToConditions(targetNode, actualTargetVar, &conditions)
		} else {
			if fromJoins.Len() == 0 {
				fromJoins.WriteString("edges " + relVar)
			} else {
				fromJoins.WriteString(" CROSS JOIN edges " + relVar)
			}
		}

		prevIDSrc := prevVar + ".id"
		targetIDSrc := actualTargetVar + ".id"

		switch elem.Relationship.Direction {
		case DirectionOutgoing:
			conditions = append(conditions, relVar+".from_id = "+prevIDSrc)
			conditions = append(conditions, targetIDSrc+" = "+relVar+".to_id")
		case DirectionIncoming:
			conditions = append(conditions, relVar+".to_id = "+prevIDSrc)
			conditions = append(conditions, targetIDSrc+" = "+relVar+".from_id")
		case DirectionUndirected:
			conditions = append(conditions, "("+relVar+".from_id = "+prevIDSrc+" OR "+relVar+".to_id = "+prevIDSrc+")")
			conditions = append(conditions,
				targetIDSrc+" = CASE WHEN "+relVar+".from_id = "+prevIDSrc+" THEN "+relVar+".to_id ELSE "+relVar+".from_id END")
		}

		if len(elem.Relationship.Types) == 1 {
			conditions = append(conditions, relVar+".kind = '"+elem.Relationship.Types[0]+"'")
		} else if len(elem.Relationship.Types) > 1 {
			conditions = append(conditions, relVar+".kind IN ('"+strings.Join(elem.Relationship.Types, "', '")+"')")
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
