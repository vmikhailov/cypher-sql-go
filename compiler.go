package cyphersql

import (
	"fmt"
	"strings"
)

var reservedKeywords = map[string]bool{
	"in": true, "order": true, "group": true, "by": true, "where": true,
	"from": true, "select": true, "join": true, "table": true, "index": true,
	"as": true, "on": true, "case": true, "when": true, "then": true,
	"else": true, "end": true, "with": true, "limit": true, "offset": true,
	"union": true, "all": true, "distinct": true, "values": true, "into": true,
	"set": true, "update": true, "delete": true, "insert": true, "drop": true,
	"create": true, "alter": true, "not": true, "and": true, "or": true,
	"is": true, "null": true, "like": true, "glob": true, "between": true,
	"exists": true, "key": true, "check": true, "column": true, "primary": true,
}

// Compiler compiles a Cypher AST into SQLite SQL using structured models and templates.
type Compiler struct {
	query              *Query
	initialParams      map[string]any
	params             map[string]any
	declaredNodes      map[string]bool
	declaredRels       map[string]bool
	withAliases        map[string]string
	unwindVariables    map[string]bool
	aggregatedAliases  map[string]bool
	decomposedBranches map[string]*decomposedBranch
	ctes               []string
	fromTable          string
	fromAlias          string
	joins              []JoinModel
	varIndex           int
	paramIndex         int
}

// NewCompiler creates a new Compiler.
func NewCompiler(query *Query, params map[string]any) *Compiler {
	return &Compiler{
		query:              query,
		initialParams:      params,
		params:             make(map[string]any),
		declaredNodes:      make(map[string]bool),
		declaredRels:       make(map[string]bool),
		withAliases:        make(map[string]string),
		unwindVariables:    make(map[string]bool),
		aggregatedAliases:  make(map[string]bool),
		decomposedBranches: make(map[string]*decomposedBranch),
	}
}

func (c *Compiler) escapeVar(name string) string {
	if reservedKeywords[strings.ToLower(name)] {
		return `"` + name + `"`
	}
	return name
}

// Compile generates a CompiledQuery from the AST.
func (c *Compiler) Compile() (*CompiledQuery, error) {
	for k, v := range c.initialParams {
		c.params[k] = v
	}

	c.detectDecomposedOptionalMatches(c.query)

	var whereConditions []string
	var groupByColumns []string
	var havingConditions []string

	for _, match := range c.query.Matches {
		if c.isMatchDecomposed(match) {
			continue
		}
		if err := c.processMatchClause(match, &whereConditions); err != nil {
			return nil, err
		}
	}

	if err := c.processWithClauses(&whereConditions, &groupByColumns, &havingConditions); err != nil {
		return nil, err
	}

	if c.query.Where != nil {
		wSQL, err := c.visitExpression(c.query.Where)
		if err != nil {
			return nil, err
		}
		if wSQL != "" {
			whereConditions = append(whereConditions, wSQL)
		}
	}

	c.populateReturnGroupBy(c.query.Return, &groupByColumns)

	selectColumns, err := c.buildSelectColumns(c.query.Return)
	if err != nil {
		return nil, err
	}

	sql := c.assembleQuerySQL(selectColumns, whereConditions, groupByColumns, havingConditions)

	return &CompiledQuery{
		SQL:    sql,
		Params: c.params,
	}, nil
}

func (c *Compiler) processMatchClause(match MatchClause, mainWhereConditions *[]string) error {
	isOptional := match.IsOptional
	joinKeyword := "JOIN"
	if isOptional {
		joinKeyword = "LEFT JOIN"
	}

	var optionalWhereExtra []string
	if isOptional && match.Where != nil {
		wSQL, err := c.visitExpression(match.Where)
		if err != nil {
			return err
		}
		if wSQL != "" {
			optionalWhereExtra = append(optionalWhereExtra, wSQL)
		}
	}

	for _, path := range match.Paths {
		if err := c.processPathPattern(path, isOptional, joinKeyword, mainWhereConditions, optionalWhereExtra); err != nil {
			return err
		}
	}

	if !isOptional && match.Where != nil {
		wSQL, err := c.visitExpression(match.Where)
		if err != nil {
			return err
		}
		if wSQL != "" {
			*mainWhereConditions = append(*mainWhereConditions, wSQL)
		}
	}
	return nil
}

func (c *Compiler) processPathPattern(path PathPattern, isOptional bool, joinKeyword string, mainWhereConditions *[]string, optionalWhereExtra []string) error {
	headVar := path.Head.Variable
	if headVar == "" {
		c.varIndex++
		headVar = fmt.Sprintf("_n%d", c.varIndex)
	}

	if len(path.Chain) == 0 {
		if !c.declaredNodes[headVar] {
			c.bindHeadNode(path.Head, headVar, isOptional, joinKeyword, mainWhereConditions, optionalWhereExtra)
		}
		return nil
	}

	anyDeclared := c.declaredNodes[headVar]
	if !anyDeclared {
		for _, elem := range path.Chain {
			if elem.Target.Variable != "" && c.declaredNodes[elem.Target.Variable] {
				anyDeclared = true
				break
			}
		}
	}

	if !anyDeclared {
		c.bindHeadNode(path.Head, headVar, isOptional, joinKeyword, mainWhereConditions, optionalWhereExtra)
	}

	return c.processPathChain(path, headVar, isOptional, joinKeyword, mainWhereConditions, optionalWhereExtra)
}

func (c *Compiler) bindHeadNode(headNode NodePattern, headVar string, isOptional bool, joinKeyword string, mainWhereConditions *[]string, optionalWhereExtra []string) {
	escHead := c.escapeVar(headVar)

	var conds []string
	c.addNodeFiltersToConditions(headNode, headVar, &conds)

	if c.fromTable == "" && !isOptional {
		c.fromTable = "nodes"
		c.fromAlias = escHead
		*mainWhereConditions = append(*mainWhereConditions, conds...)
	} else {
		onClause := "1=1"
		var allOn []string
		allOn = append(allOn, conds...)
		allOn = append(allOn, optionalWhereExtra...)
		if len(allOn) > 0 {
			onClause = strings.Join(allOn, " AND ")
		}
		c.joins = append(c.joins, JoinModel{
			Type:  joinKeyword,
			Table: "nodes",
			Alias: escHead,
			On:    onClause,
		})
	}

	c.declaredNodes[headVar] = true
}

func (c *Compiler) processPathChain(path PathPattern, headVar string, isOptional bool, joinKeyword string, mainWhereConditions *[]string, optionalWhereExtra []string) error {
	prevVar := headVar

	for _, elem := range path.Chain {
		rel := elem.Relationship
		targetNode := elem.Target
		targetVar := targetNode.Variable
		if targetVar == "" {
			c.varIndex++
			targetVar = fmt.Sprintf("_n%d", c.varIndex)
		}

		relVar := rel.Variable
		if relVar == "" {
			c.varIndex++
			relVar = fmt.Sprintf("_r%d", c.varIndex)
		}

		c.declaredRels[relVar] = true
		c.declaredNodes[targetVar] = true

		escRel := c.escapeVar(relVar)
		escTarget := c.escapeVar(targetVar)
		escPrev := c.escapeVar(prevVar)

		var relOnConds []string
		switch rel.Direction {
		case DirectionOutgoing:
			relOnConds = append(relOnConds, fmt.Sprintf("%s.from_id = %s.id", escRel, escPrev))
		case DirectionIncoming:
			relOnConds = append(relOnConds, fmt.Sprintf("%s.to_id = %s.id", escRel, escPrev))
		case DirectionUndirected:
			relOnConds = append(relOnConds, fmt.Sprintf("(%s.from_id = %s.id OR %s.to_id = %s.id)", escRel, escPrev, escRel, escPrev))
		}

		if len(rel.Types) == 1 {
			relOnConds = append(relOnConds, fmt.Sprintf("%s.kind = '%s'", escRel, rel.Types[0]))
		} else if len(rel.Types) > 1 {
			var quoted []string
			for _, t := range rel.Types {
				quoted = append(quoted, fmt.Sprintf("'%s'", t))
			}
			relOnConds = append(relOnConds, fmt.Sprintf("%s.kind IN (%s)", escRel, strings.Join(quoted, ", ")))
		}

		c.joins = append(c.joins, JoinModel{
			Type:  joinKeyword,
			Table: "edges",
			Alias: escRel,
			On:    strings.Join(relOnConds, " AND "),
		})

		var targetOnConds []string
		switch rel.Direction {
		case DirectionOutgoing:
			targetOnConds = append(targetOnConds, fmt.Sprintf("%s.id = %s.to_id", escTarget, escRel))
		case DirectionIncoming:
			targetOnConds = append(targetOnConds, fmt.Sprintf("%s.id = %s.from_id", escTarget, escRel))
		case DirectionUndirected:
			targetOnConds = append(targetOnConds, fmt.Sprintf("%s.id = CASE WHEN %s.from_id = %s.id THEN %s.to_id ELSE %s.from_id END",
				escTarget, escRel, escPrev, escRel, escRel))
		}

		c.addNodeFiltersToConditions(targetNode, targetVar, &targetOnConds)
		targetOnConds = append(targetOnConds, optionalWhereExtra...)

		c.joins = append(c.joins, JoinModel{
			Type:  joinKeyword,
			Table: "nodes",
			Alias: escTarget,
			On:    strings.Join(targetOnConds, " AND "),
		})

		prevVar = targetVar
	}
	return nil
}

func (c *Compiler) addNodeFiltersToConditions(node NodePattern, nodeVar string, conditions *[]string) {
	nVar := c.escapeVar(nodeVar)
	for _, label := range node.Labels {
		*conditions = append(*conditions, c.compileNodeLabelPredicate(nVar, label))
	}
	if node.Properties != nil {
		for k, v := range node.Properties {
			valSQL, err := c.visitExpression(v)
			if err == nil {
				*conditions = append(*conditions, fmt.Sprintf("json_extract(%s.properties, '$.%s') = %s", nVar, k, valSQL))
			}
		}
	}
}

func (c *Compiler) compileNodeLabelPredicate(nVar, label string) string {
	lower := strings.ToLower(label)
	switch lower {
	case "service":
		return fmt.Sprintf("(%s.kind = 'Service' OR (%s.kind = 'Project' AND json_extract(%s.properties, '$.role') = 'Service' AND NOT EXISTS (SELECT 1 FROM nodes _s WHERE _s.kind = 'Service' AND (json_extract(_s.properties, '$.project_id') = %s.id OR json_extract(_s.properties, '$.name') = json_extract(%s.properties, '$.name')))))",
			nVar, nVar, nVar, nVar, nVar)
	case "app":
		return fmt.Sprintf("(%s.kind IN ('App', 'FrontendApp') OR (%s.kind = 'Project' AND json_extract(%s.properties, '$.role') IN ('App', 'FrontendApp') AND NOT EXISTS (SELECT 1 FROM nodes _a WHERE _a.kind IN ('App', 'FrontendApp') AND (json_extract(_a.properties, '$.project_id') = %s.id OR json_extract(_a.properties, '$.name') = json_extract(%s.properties, '$.name')))))",
			nVar, nVar, nVar, nVar, nVar)
	case "library":
		return fmt.Sprintf("(%s.kind IN ('Library', 'SharedLibrary') OR (%s.kind = 'Project' AND (json_extract(%s.properties, '$.role') IN ('Library', 'SharedLibrary') OR json_extract(%s.properties, '$.is_library') = 1) AND NOT EXISTS (SELECT 1 FROM nodes _l WHERE _l.kind IN ('Library', 'SharedLibrary') AND (json_extract(_l.properties, '$.project_id') = %s.id OR json_extract(_l.properties, '$.name') = json_extract(%s.properties, '$.name')))))",
			nVar, nVar, nVar, nVar, nVar, nVar)
	case "project":
		return fmt.Sprintf("(%s.kind = 'Project' OR (%s.kind IN ('Service', 'App', 'FrontendApp', 'Library', 'SharedLibrary', 'Worker', 'CliTool') AND NOT EXISTS (SELECT 1 FROM nodes _p WHERE _p.kind = 'Project' AND (json_extract(%s.properties, '$.project_id') = _p.id OR json_extract(_p.properties, '$.name') = json_extract(%s.properties, '$.name')))))",
			nVar, nVar, nVar, nVar)
	default:
		return fmt.Sprintf("%s.kind = '%s'", nVar, label)
	}
}

func (c *Compiler) processWithClauses(whereConditions *[]string, groupByColumns *[]string, havingConditions *[]string) error {
	for _, with := range c.query.WithClauses {
		hasAgg := false
		for _, item := range with.Items {
			if c.hasAggregation(item.Expression) {
				hasAgg = true
				break
			}
		}

		for _, item := range with.Items {
			if item.Alias != "" {
				exprSQL, err := c.visitExpression(item.Expression)
				if err != nil {
					return err
				}
				c.withAliases[item.Alias] = exprSQL
				if c.hasAggregation(item.Expression) {
					c.aggregatedAliases[item.Alias] = true
				}
			}

			if hasAgg {
				if c.hasAggregation(item.Expression) {
					continue
				}
				if id, ok := item.Expression.(IdentifierExpr); ok {
					if c.declaredNodes[id.Name] {
						*groupByColumns = append(*groupByColumns, fmt.Sprintf("%s.id", c.escapeVar(id.Name)))
					} else if c.declaredRels[id.Name] {
						*groupByColumns = append(*groupByColumns, fmt.Sprintf("%s.rowid", c.escapeVar(id.Name)))
					} else {
						exprSQL, _ := c.visitExpression(item.Expression)
						*groupByColumns = append(*groupByColumns, exprSQL)
					}
				}
			}
		}

		if with.Where != nil {
			wSQL, err := c.visitExpression(with.Where)
			if err != nil {
				return err
			}
			if hasAgg {
				*havingConditions = append(*havingConditions, wSQL)
			} else {
				*whereConditions = append(*whereConditions, wSQL)
			}
		}
	}
	return nil
}

func (c *Compiler) populateReturnGroupBy(ret ReturnClause, groupByColumns *[]string) {
	if len(*groupByColumns) > 0 {
		return
	}
	hasAgg := false
	for _, item := range ret.Items {
		if c.hasAggregation(item.Expression) {
			hasAgg = true
			break
		}
	}
	if !hasAgg {
		return
	}

	for _, item := range ret.Items {
		if c.hasAggregation(item.Expression) {
			continue
		}
		if _, ok := item.Expression.(WildcardExpr); ok {
			continue
		}
		if id, ok := item.Expression.(IdentifierExpr); ok {
			if c.declaredNodes[id.Name] {
				*groupByColumns = append(*groupByColumns, fmt.Sprintf("%s.id", c.escapeVar(id.Name)))
			} else if c.declaredRels[id.Name] {
				*groupByColumns = append(*groupByColumns, fmt.Sprintf("%s.rowid", c.escapeVar(id.Name)))
			} else if item.Alias != "" {
				*groupByColumns = append(*groupByColumns, `"`+item.Alias+`"`)
			} else {
				exprSQL, _ := c.visitExpression(item.Expression)
				*groupByColumns = append(*groupByColumns, exprSQL)
			}
		} else if item.Alias != "" {
			*groupByColumns = append(*groupByColumns, `"`+item.Alias+`"`)
		} else {
			exprSQL, _ := c.visitExpression(item.Expression)
			*groupByColumns = append(*groupByColumns, exprSQL)
		}
	}
}

func (c *Compiler) buildSelectColumns(ret ReturnClause) ([]string, error) {
	var cols []string
	for _, item := range ret.Items {
		if _, ok := item.Expression.(WildcardExpr); ok {
			cols = append(cols, "*")
			continue
		}
		exprSQL, err := c.visitExpression(item.Expression)
		if err != nil {
			return nil, err
		}
		alias := item.Alias
		if alias == "" {
			if id, ok := item.Expression.(IdentifierExpr); ok {
				alias = id.Name
			}
		}
		if alias != "" {
			cols = append(cols, fmt.Sprintf(`%s AS "%s"`, exprSQL, alias))
		} else {
			cols = append(cols, exprSQL)
		}
	}
	return cols, nil
}

func (c *Compiler) assembleQuerySQL(selectColumns []string, whereConditions []string, groupByColumns []string, havingConditions []string) string {
	// Deduplicate groupBy columns
	seen := make(map[string]bool)
	var uniqueGroup []string
	for _, col := range groupByColumns {
		if !seen[col] {
			seen[col] = true
			uniqueGroup = append(uniqueGroup, col)
		}
	}

	var orderItems []string
	for _, item := range c.query.OrderBy {
		exprSQL, err := c.visitExpression(item.Expression)
		if err == nil {
			dir := "ASC"
			if item.IsDesc {
				dir = "DESC"
			}
			orderItems = append(orderItems, fmt.Sprintf("%s %s", exprSQL, dir))
		}
	}

	limitSQL := ""
	if c.query.Limit != nil {
		lim, err := c.visitExpression(c.query.Limit)
		if err == nil {
			limitSQL = lim
		}
	}

	offsetSQL := ""
	if c.query.Skip != nil {
		skip, err := c.visitExpression(c.query.Skip)
		if err == nil {
			offsetSQL = skip
		}
	}

	whereSQL := ""
	if len(whereConditions) > 0 {
		whereSQL = strings.Join(whereConditions, " AND ")
	}

	havingSQL := ""
	if len(havingConditions) > 0 {
		havingSQL = strings.Join(havingConditions, " AND ")
	}

	model := &QueryModel{
		CTEs:       c.ctes,
		IsDistinct: c.query.Return.IsDistinct,
		Columns:    selectColumns,
		FromTable:  c.fromTable,
		FromAlias:  c.fromAlias,
		Joins:      c.joins,
		Where:      whereSQL,
		GroupBy:    uniqueGroup,
		Having:     havingSQL,
		OrderBy:    orderItems,
		Limit:      limitSQL,
		Offset:     offsetSQL,
	}

	return RenderQuery(model)
}
