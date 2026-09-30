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
		return "@" + e.Name, nil
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
		return escaped + ".value"
	}
	if c.declaredNodes[id.Name] {
		return "json_object('id', " + escaped + "." + c.schema.NodeIDCol + ", 'kind', " +
			escaped + "." + c.schema.NodeKindCol + ", 'properties', json(" +
			escaped + "." + c.schema.NodePropsCol + "))"
	}
	if c.declaredRels[id.Name] {
		return "json_object('type', " + escaped + "." + c.schema.EdgeKindCol + ", 'from', " +
			escaped + "." + c.schema.EdgeFromCol + ", 'to', " + escaped + "." + c.schema.EdgeToCol +
			", 'properties', json(" + escaped + "." + c.schema.EdgePropsCol + "))"
	}
	return escaped
}

func (c *Compiler) visitPropertyAccess(prop ast.PropertyAccessExpr) string {
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
			return v + "." + c.schema.EdgeKindCol
		case "from", "from_id":
			return v + "." + c.schema.EdgeFromCol
		case "to", "to_id":
			return v + "." + c.schema.EdgeToCol
		case "properties":
			return v + "." + c.schema.EdgePropsCol
		default:
			return c.schema.Dialect.JSONExtract(v+"."+c.schema.EdgePropsCol, propName)
		}
	}

	switch strings.ToLower(propName) {
	case "id":
		return v + "." + c.schema.NodeIDCol
	case "kind":
		return v + "." + c.schema.NodeKindCol
	case "properties":
		return v + "." + c.schema.NodePropsCol
	default:
		return c.schema.Dialect.JSONExtract(v+"."+c.schema.NodePropsCol, propName)
	}
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
	leftSQL, err := c.visitExpression(b.Left)
	if err != nil {
		return "", err
	}
	rightSQL, err := c.visitExpression(b.Right)
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
		return "(" + leftSQL + " IN (SELECT value FROM json_each(" + rightSQL + ")))", nil
	case ast.OpStarts:
		return "(" + leftSQL + " LIKE (" + rightSQL + " || '%'))", nil
	case ast.OpEnds:
		return "(" + leftSQL + " LIKE ('%' || " + rightSQL + "))", nil
	case ast.OpContains:
		return "(" + leftSQL + " LIKE ('%' || " + rightSQL + " || '%'))", nil
	case ast.OpIs:
		return "(" + leftSQL + " IS " + rightSQL + ")", nil
	case ast.OpIsNot:
		return "(" + leftSQL + " IS NOT " + rightSQL + ")", nil
	default:
		return "(" + leftSQL + " " + string(b.Op) + " " + rightSQL + ")", nil
	}
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

func (c *Compiler) visitFunctionCall(fn ast.FunctionCallExpr) (string, error) {
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
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
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
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
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
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
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
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
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
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
			v := c.escapeVar(id.Name)
			return "json_array(" + v + "." + c.schema.NodeKindCol + ")", nil
		}
	}

	if lower == "exists" && len(fn.Args) == 1 {
		if pat, ok := fn.Args[0].(ast.PatternExpr); ok {
			return c.visitPatternExpr(pat)
		}
		argSQL, _ := c.visitExpression(fn.Args[0])
		return "(" + argSQL + " IS NOT NULL)", nil
	}

	// 3. Aggregations
	if lower == "count" && len(fn.Args) == 1 {
		if _, ok := fn.Args[0].(ast.WildcardExpr); ok {
			return "COUNT(" + distinctStr + "*)", nil
		}
		if id, ok := fn.Args[0].(ast.IdentifierExpr); ok {
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
		return c.schema.Dialect.ArrayAgg(innerSQL, innerSQL+" IS NOT NULL", fn.IsDistinct), nil
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
		return c.schema.Dialect.ArrayLength(argSQL), nil
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

func (c *Compiler) buildSubqueryPath(path ast.PathPattern, prefix string) (string, []string) {
	var conditions []string
	var fromJoins strings.Builder

	headVar := path.Head.Variable
	headIsOuter := headVar != "" && c.declaredNodes[headVar]

	actualHeadVar := headVar
	if !headIsOuter {
		c.varIndex++
		actualHeadVar = prefix + "_h" + strconv.Itoa(c.varIndex)
		fromJoins.WriteString(c.schema.NodesTable + " " + actualHeadVar)
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
				fromJoins.WriteString(c.schema.EdgesTable + " " + relVar + " CROSS JOIN " +
					c.schema.NodesTable + " " + actualTargetVar)
			} else {
				fromJoins.WriteString(" CROSS JOIN " + c.schema.EdgesTable + " " + relVar +
					" CROSS JOIN " + c.schema.NodesTable + " " + actualTargetVar)
			}
			c.addNodeFiltersToConditions(targetNode, actualTargetVar, &conditions)
		} else {
			if fromJoins.Len() == 0 {
				fromJoins.WriteString(c.schema.EdgesTable + " " + relVar)
			} else {
				fromJoins.WriteString(" CROSS JOIN " + c.schema.EdgesTable + " " + relVar)
			}
		}

		prevIDSrc := prevVar + "." + c.schema.NodeIDCol
		targetIDSrc := actualTargetVar + "." + c.schema.NodeIDCol

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
				relVar+"."+c.schema.EdgeKindCol+" = '"+elem.Relationship.Types[0]+"'")
		} else if len(elem.Relationship.Types) > 1 {
			conditions = append(conditions,
				relVar+"."+c.schema.EdgeKindCol+" IN ('"+strings.Join(elem.Relationship.Types, "', '")+"')")
		}

		prevVar = actualTargetVar
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
	}
}
