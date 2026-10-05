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
	case ast.HasLabelExpr:
		return c.visitHasLabel(e)
	case ast.MapProjectionExpr:
		return c.visitMapProjection(e)
	case ast.ReduceExpr:
		return c.visitReduce(e)
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
	var pairs []string
	for k, v := range m.Entries {
		vSQL, err := c.visitExpression(v)
		if err != nil {
			return "", err
		}
		pairs = append(pairs, "'"+SanitizeSQLLiteral(k)+"', "+vSQL)
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
	if id, ok := mp.Base.(ast.IdentifierExpr); ok {
		baseVar = c.escapeVar(id.Name)
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
				propSQL = baseVar + "." + c.schema.NodeIDCol
			case strings.ToLower(c.schema.NodeKindCol), "kind":
				propSQL = baseVar + "." + c.schema.NodeKindCol
			default:
				propSQL = c.schema.Dialect.JSONExtract(baseVar+"."+c.schema.NodePropsCol, elem.PropertyName)
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
	return "(" + initSQL + " + COALESCE((SELECT sum(value) FROM json_each(" + listSQL + ")), 0))", nil
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
		fromJoins.WriteString(c.schema.NodesTable)
		fromJoins.WriteString(" ")
		fromJoins.WriteString(actualHeadVar)
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
