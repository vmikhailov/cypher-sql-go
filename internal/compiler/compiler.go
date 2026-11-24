package compiler

import (
	"github.com/vmikhailov/cypher-sql-go/internal/ast"
	"strconv"
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
	query              *ast.Query
	schema             SchemaConfig
	initialParams      map[string]any
	params             map[string]any
	declaredNodes      map[string]bool
	declaredRels       map[string]bool
	varLenRels         map[string]bool
	withAliases        map[string]string
	unwindVariables    map[string]bool
	aggregatedAliases  map[string]bool
	decomposedBranches map[string]*decomposedBranch
	nodeIDSource       map[string]string
	nodeKindSource     map[string]string
	nodePropsSource    map[string]string
	ctes               []string
	varAliases         map[string]string
	fromTable          string
	fromAlias          string
	joins              []JoinModel
	varIndex           int
	paramIndex         int
	paramSlots         map[string]int
	paramOrder         []string
}

// NewCompiler creates a new Compiler with default schema.
func NewCompiler(query *ast.Query, params map[string]any) *Compiler {
	return NewCompilerWithOptions(query, params, DefaultSchemaConfig())
}

// NewCompilerWithOptions creates a new Compiler with a custom SchemaConfig.
func NewCompilerWithOptions(query *ast.Query, params map[string]any, cfg SchemaConfig) *Compiler {
	if cfg.NodesTable == "" {
		cfg.NodesTable = "nodes"
	}
	if cfg.EdgesTable == "" {
		cfg.EdgesTable = "edges"
	}
	if cfg.NodeIDCol == "" {
		cfg.NodeIDCol = "id"
	}
	if cfg.NodeKindCol == "" {
		cfg.NodeKindCol = "kind"
	}
	if cfg.NodePropsCol == "" {
		cfg.NodePropsCol = "properties"
	}
	if cfg.EdgeFromCol == "" {
		cfg.EdgeFromCol = "from_id"
	}
	if cfg.EdgeToCol == "" {
		cfg.EdgeToCol = "to_id"
	}
	if cfg.EdgeKindCol == "" {
		cfg.EdgeKindCol = "kind"
	}
	if cfg.EdgePropsCol == "" {
		cfg.EdgePropsCol = "properties"
	}
	if cfg.Dialect == nil {
		cfg.Dialect = SQLiteDialect()
	}
	if cfg.MaxPathTraversals == 0 {
		cfg.MaxPathTraversals = DefaultMaxPathTraversals
	}

	return &Compiler{
		query:              query,
		schema:             cfg,
		initialParams:      params,
		params:             make(map[string]any),
		paramSlots:         make(map[string]int),
		declaredNodes:      make(map[string]bool),
		declaredRels:       make(map[string]bool),
		varLenRels:         make(map[string]bool),
		withAliases:        make(map[string]string),
		unwindVariables:    make(map[string]bool),
		aggregatedAliases:  make(map[string]bool),
		decomposedBranches: make(map[string]*decomposedBranch),
		nodeIDSource:       make(map[string]string),
		nodeKindSource:     make(map[string]string),
		nodePropsSource:    make(map[string]string),
		varAliases:         make(map[string]string),
	}
}

func (c *Compiler) escapeVar(name string) string {
	if c.varAliases != nil {
		if alias, ok := c.varAliases[name]; ok {
			name = alias
		}
	}
	if reservedKeywords[strings.ToLower(name)] {
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
	return name
}

// Compile generates a CompiledQuery from the AST.
func (c *Compiler) Compile() (*CompiledQuery, error) {
	if err := c.validateQuery(c.query); err != nil {
		return nil, err
	}

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

	// 2. UNWIND clauses: unnest collections / arrays into relational table rows (json_each / arrayJoin)
	if err := c.processUnwinds(c.query.Unwinds); err != nil {
		return nil, err
	}

	// 3. CALL { ... } clauses: compile correlated subqueries and bind returned aliases
	if err := c.processCalls(c.query.Calls); err != nil {
		return nil, err
	}

	// 4. WITH clauses: handle aggregations, aliasing, and intermediate filters
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
		SQL:        sql,
		Params:     c.params,
		ParamOrder: c.paramOrder,
	}, nil
}

func (c *Compiler) processMatchClause(match ast.MatchClause, mainWhereConditions *[]string) error {
	isOptional := match.IsOptional
	joinKeyword := "JOIN"
	if isOptional {
		joinKeyword = "LEFT JOIN"
	}

	var optionalWhereExtra []string
	if isOptional && match.Where != nil {
		var tempNodes []string
		var tempRels []string
		for _, p := range match.Paths {
			if p.Head.Variable != "" && !c.declaredNodes[p.Head.Variable] {
				c.declaredNodes[p.Head.Variable] = true
				tempNodes = append(tempNodes, p.Head.Variable)
			}
			for _, step := range p.Chain {
				if step.Relationship.Variable != "" && !c.declaredRels[step.Relationship.Variable] {
					c.declaredRels[step.Relationship.Variable] = true
					tempRels = append(tempRels, step.Relationship.Variable)
				}
				if step.Target.Variable != "" && !c.declaredNodes[step.Target.Variable] {
					c.declaredNodes[step.Target.Variable] = true
					tempNodes = append(tempNodes, step.Target.Variable)
				}
			}
		}
		for _, conj := range flattenAnd(match.Where) {
			wSQL, err := c.visitExpression(conj)
			if err != nil {
				for _, n := range tempNodes {
					delete(c.declaredNodes, n)
				}
				for _, r := range tempRels {
					delete(c.declaredRels, r)
				}
				return err
			}
			if wSQL != "" {
				optionalWhereExtra = append(optionalWhereExtra, wSQL)
			}
		}
		for _, n := range tempNodes {
			delete(c.declaredNodes, n)
		}
		for _, r := range tempRels {
			delete(c.declaredRels, r)
		}
	}

	for _, path := range match.Paths {
		err := c.processPathPattern(path, isOptional, joinKeyword, mainWhereConditions, optionalWhereExtra)
		if err != nil {
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

func (c *Compiler) isPathAnchored(path ast.PathPattern) bool {
	if path.Head.Variable != "" && c.declaredNodes[path.Head.Variable] {
		return true
	}
	for _, step := range path.Chain {
		if step.Target.Variable != "" && c.declaredNodes[step.Target.Variable] {
			return true
		}
	}
	return false
}

func (c *Compiler) processPathPattern(
	path ast.PathPattern,
	isOptional bool,
	joinKeyword string,
	mainWhereConditions *[]string,
	optionalWhereExtra []string,
) error {
	headVar := path.Head.Variable
	if headVar == "" {
		c.varIndex++
		headVar = "_n" + strconv.Itoa(c.varIndex)
	}

	if len(path.Chain) == 0 {
		if !c.declaredNodes[headVar] {
			c.bindHeadNode(path.Head, headVar, isOptional, joinKeyword, mainWhereConditions, optionalWhereExtra)
		} else if len(path.Head.Labels) > 0 || path.Head.Properties != nil {
			c.addNodeFiltersToConditions(path.Head, headVar, mainWhereConditions)
		}
		return nil
	}

	if !c.isPathAnchored(path) {
		c.bindHeadNode(path.Head, headVar, isOptional, joinKeyword, mainWhereConditions, optionalWhereExtra)
	} else if c.declaredNodes[headVar] && (len(path.Head.Labels) > 0 || path.Head.Properties != nil) {
		c.addNodeFiltersToConditions(path.Head, headVar, mainWhereConditions)
	}

	return c.processPathChain(path, headVar, isOptional, joinKeyword, mainWhereConditions, optionalWhereExtra)
}

func (c *Compiler) bindHeadNode(
	headNode ast.NodePattern,
	headVar string,
	isOptional bool,
	joinKeyword string,
	mainWhereConditions *[]string,
	optionalWhereExtra []string,
) {
	escHead := c.escapeVar(headVar)

	var conds []string
	c.addNodeFiltersToConditions(headNode, headVar, &conds)

	if c.fromTable == "" && !isOptional {
		c.fromTable = c.schema.NodesTable
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
			Table: c.schema.NodesTable,
			Alias: escHead,
			On:    onClause,
		})
	}

	c.declaredNodes[headVar] = true
}

func (c *Compiler) getNodeIDSQL(varName string) string {
	if src, ok := c.nodeIDSource[varName]; ok {
		return src
	}
	return c.escapeVar(varName) + "." + c.schema.NodeIDCol
}

func (c *Compiler) getNodeKindSQL(varName string) string {
	if src, ok := c.nodeKindSource[varName]; ok {
		return src
	}
	return c.escapeVar(varName) + "." + c.schema.NodeKindCol
}

func (c *Compiler) getNodePropsSQL(varName string) string {
	if src, ok := c.nodePropsSource[varName]; ok {
		return src
	}
	return c.escapeVar(varName) + "." + c.schema.NodePropsCol
}

func (c *Compiler) processPathChain(
	path ast.PathPattern,
	headVar string,
	isOptional bool,
	joinKeyword string,
	mainWhereConditions *[]string,
	optionalWhereExtra []string,
) error {
	nodes := make([]ast.NodePattern, len(path.Chain)+1)
	vars := make([]string, len(path.Chain)+1)
	nodes[0] = path.Head
	vars[0] = headVar
	for i := 0; i < len(path.Chain); i++ {
		nodes[i+1] = path.Chain[i].Target
		tVar := path.Chain[i].Target.Variable
		if tVar == "" {
			c.varIndex++
			tVar = "_n" + strconv.Itoa(c.varIndex)
		}
		vars[i+1] = tVar
	}

	anchorIndex := 0
	for i, v := range vars {
		if c.declaredNodes[v] {
			anchorIndex = i
			break
		}
	}

	for i := anchorIndex - 1; i >= 0; i-- {
		if err := c.processPathElementAt(path, i, nodes[i], vars[i], nodes[i+1], vars[i+1], isOptional, joinKeyword, mainWhereConditions, optionalWhereExtra); err != nil {
			return err
		}
	}

	for i := anchorIndex; i < len(path.Chain); i++ {
		if err := c.processPathElementAt(path, i, nodes[i], vars[i], nodes[i+1], vars[i+1], isOptional, joinKeyword, mainWhereConditions, optionalWhereExtra); err != nil {
			return err
		}
	}

	return nil
}

func (c *Compiler) processPathElementAt(
	path ast.PathPattern,
	elementIndex int,
	prevNode ast.NodePattern,
	prevVar string,
	targetNode ast.NodePattern,
	targetVar string,
	isOptional bool,
	joinKeyword string,
	mainWhereConditions *[]string,
	optionalWhereExtra []string,
) error {
	elem := path.Chain[elementIndex]
	rel := elem.Relationship
	relVar := rel.Variable
	if relVar == "" {
		c.varIndex++
		relVar = "_r" + strconv.Itoa(c.varIndex)
	}

	prevDeclared := c.declaredNodes[prevVar]
	targetAlreadyDeclared := c.declaredNodes[targetVar]

	if rel.MinHops != nil || rel.MaxHops != nil || path.IsShortestPath || path.IsAllShortestPaths {
		return c.processVarLenStep(prevNode, prevVar, rel, targetNode, targetVar, relVar, prevDeclared, targetAlreadyDeclared, isOptional, joinKeyword, optionalWhereExtra, path.IsShortestPath, path.PathVariable)
	}

	c.declaredRels[relVar] = true
	if path.PathVariable != "" {
		c.varAliases[path.PathVariable] = relVar
	}

	escRel := c.escapeVar(relVar)
	escTarget := c.escapeVar(targetVar)
	escPrev := c.escapeVar(prevVar)
	prevIDSQL := c.getNodeIDSQL(prevVar)
	targetIDSQL := c.getNodeIDSQL(targetVar)

	var relOnConds []string
	c.addRelFiltersToConditions(rel, escRel, &relOnConds)

	if prevDeclared && !targetAlreadyDeclared {
		// Forward hop: prevVar is bound, join edges then targetVar
		switch rel.Direction {
		case ast.DirectionOutgoing:
			relOnConds = append([]string{escRel + "." + c.schema.EdgeFromCol + " = " + prevIDSQL}, relOnConds...)
		case ast.DirectionIncoming:
			relOnConds = append([]string{escRel + "." + c.schema.EdgeToCol + " = " + prevIDSQL}, relOnConds...)
		case ast.DirectionUndirected:
			relOnConds = append([]string{"(" + escRel + "." + c.schema.EdgeFromCol + " = " + prevIDSQL + " OR " +
				escRel + "." + c.schema.EdgeToCol + " = " + prevIDSQL + ")"}, relOnConds...)
		}

		var targetExtra []string
		for _, cond := range optionalWhereExtra {
			if !strings.Contains(cond, escTarget+".") {
				relOnConds = append(relOnConds, cond)
			} else {
				targetExtra = append(targetExtra, cond)
			}
		}

		c.joins = append(c.joins, JoinModel{
			Type:  joinKeyword,
			Table: c.schema.EdgesTable,
			Alias: escRel,
			On:    strings.Join(relOnConds, " AND "),
		})

		var targetOnConds []string
		switch rel.Direction {
		case ast.DirectionOutgoing:
			targetOnConds = append(targetOnConds, targetIDSQL+" = "+escRel+"."+c.schema.EdgeToCol)
		case ast.DirectionIncoming:
			targetOnConds = append(targetOnConds, targetIDSQL+" = "+escRel+"."+c.schema.EdgeFromCol)
		case ast.DirectionUndirected:
			targetOnConds = append(targetOnConds,
				targetIDSQL+" = CASE WHEN "+escRel+"."+c.schema.EdgeFromCol+" = "+prevIDSQL+" THEN "+
					escRel+"."+c.schema.EdgeToCol+" ELSE "+escRel+"."+c.schema.EdgeFromCol+" END")
		}
		c.addNodeFiltersToConditions(targetNode, targetVar, &targetOnConds)
		targetOnConds = append(targetOnConds, targetExtra...)

		c.joins = append(c.joins, JoinModel{
			Type:  joinKeyword,
			Table: c.schema.NodesTable,
			Alias: escTarget,
			On:    strings.Join(targetOnConds, " AND "),
		})
		c.declaredNodes[targetVar] = true
	} else if !prevDeclared && targetAlreadyDeclared {
		// Backward hop: targetVar is bound, join edges then prevVar
		switch rel.Direction {
		case ast.DirectionOutgoing:
			relOnConds = append([]string{escRel + "." + c.schema.EdgeToCol + " = " + targetIDSQL}, relOnConds...)
		case ast.DirectionIncoming:
			relOnConds = append([]string{escRel + "." + c.schema.EdgeFromCol + " = " + targetIDSQL}, relOnConds...)
		case ast.DirectionUndirected:
			relOnConds = append([]string{"(" + escRel + "." + c.schema.EdgeFromCol + " = " + targetIDSQL + " OR " +
				escRel + "." + c.schema.EdgeToCol + " = " + targetIDSQL + ")"}, relOnConds...)
		}

		var prevExtra []string
		for _, cond := range optionalWhereExtra {
			if !strings.Contains(cond, escPrev+".") {
				relOnConds = append(relOnConds, cond)
			} else {
				prevExtra = append(prevExtra, cond)
			}
		}

		c.joins = append(c.joins, JoinModel{
			Type:  joinKeyword,
			Table: c.schema.EdgesTable,
			Alias: escRel,
			On:    strings.Join(relOnConds, " AND "),
		})

		var prevOnConds []string
		switch rel.Direction {
		case ast.DirectionOutgoing:
			prevOnConds = append(prevOnConds, prevIDSQL+" = "+escRel+"."+c.schema.EdgeFromCol)
		case ast.DirectionIncoming:
			prevOnConds = append(prevOnConds, prevIDSQL+" = "+escRel+"."+c.schema.EdgeToCol)
		case ast.DirectionUndirected:
			prevOnConds = append(prevOnConds,
				prevIDSQL+" = CASE WHEN "+escRel+"."+c.schema.EdgeFromCol+" = "+targetIDSQL+" THEN "+
					escRel+"."+c.schema.EdgeToCol+" ELSE "+escRel+"."+c.schema.EdgeFromCol+" END")
		}
		c.addNodeFiltersToConditions(prevNode, prevVar, &prevOnConds)
		prevOnConds = append(prevOnConds, prevExtra...)

		c.joins = append(c.joins, JoinModel{
			Type:  joinKeyword,
			Table: c.schema.NodesTable,
			Alias: escPrev,
			On:    strings.Join(prevOnConds, " AND "),
		})
		c.declaredNodes[prevVar] = true
	} else {
		// Both prevVar and targetVar are already declared
		switch rel.Direction {
		case ast.DirectionOutgoing:
			relOnConds = append([]string{
				escRel + "." + c.schema.EdgeFromCol + " = " + prevIDSQL,
				targetIDSQL + " = " + escRel + "." + c.schema.EdgeToCol,
			}, relOnConds...)
		case ast.DirectionIncoming:
			relOnConds = append([]string{
				escRel + "." + c.schema.EdgeToCol + " = " + prevIDSQL,
				targetIDSQL + " = " + escRel + "." + c.schema.EdgeFromCol,
			}, relOnConds...)
		case ast.DirectionUndirected:
			relOnConds = append([]string{
				"(" + escRel + "." + c.schema.EdgeFromCol + " = " + prevIDSQL + " OR " + escRel + "." + c.schema.EdgeToCol + " = " + prevIDSQL + ")",
				targetIDSQL + " = CASE WHEN " + escRel + "." + c.schema.EdgeFromCol + " = " + prevIDSQL + " THEN " + escRel + "." + c.schema.EdgeToCol + " ELSE " + escRel + "." + c.schema.EdgeFromCol + " END",
			}, relOnConds...)
		}
		c.addNodeFiltersToConditions(targetNode, targetVar, &relOnConds)
		relOnConds = append(relOnConds, optionalWhereExtra...)

		c.joins = append(c.joins, JoinModel{
			Type:  joinKeyword,
			Table: c.schema.EdgesTable,
			Alias: escRel,
			On:    strings.Join(relOnConds, " AND "),
		})
	}

	return nil
}

func (c *Compiler) addRelFiltersToConditions(rel ast.RelationshipPattern, escRel string, relOnConds *[]string) {
	if len(rel.Types) == 1 {
		*relOnConds = append(*relOnConds, escRel+"."+c.schema.EdgeKindCol+" = '"+SanitizeSQLLiteral(rel.Types[0])+"'")
	} else if len(rel.Types) > 1 {
		var escapedTypes []string
		for _, t := range rel.Types {
			escapedTypes = append(escapedTypes, SanitizeSQLLiteral(t))
		}
		*relOnConds = append(*relOnConds,
			escRel+"."+c.schema.EdgeKindCol+" IN ('"+strings.Join(escapedTypes, "', '")+"')")
	}

	if rel.Properties != nil {
		for k, v := range rel.Properties {
			valSQL, err := c.visitExpression(v)
			if err == nil {
				switch strings.ToLower(k) {
				case strings.ToLower(c.schema.EdgeKindCol), "kind", "type":
					*relOnConds = append(*relOnConds, escRel+"."+c.schema.EdgeKindCol+" = "+valSQL)
				case strings.ToLower(c.schema.EdgeFromCol), "from", "from_id":
					*relOnConds = append(*relOnConds, escRel+"."+c.schema.EdgeFromCol+" = "+valSQL)
				case strings.ToLower(c.schema.EdgeToCol), "to", "to_id":
					*relOnConds = append(*relOnConds, escRel+"."+c.schema.EdgeToCol+" = "+valSQL)
				default:
					extractFn := c.schema.Dialect.JSONExtract
					if c.isNumericExpr(v) {
						extractFn = c.schema.Dialect.JSONExtractNumber
					}
					*relOnConds = append(*relOnConds,
						extractFn(escRel+"."+c.schema.EdgePropsCol, k)+" = "+valSQL)
				}
			}
		}
	}
}

func (c *Compiler) addNodeFiltersToConditions(node ast.NodePattern, nodeVar string, conditions *[]string) {
	idSQL := c.getNodeIDSQL(nodeVar)
	kindSQL := c.getNodeKindSQL(nodeVar)
	propsSQL := c.getNodePropsSQL(nodeVar)
	nVar := c.escapeVar(nodeVar)
	for _, label := range node.Labels {
		if _, remapped := c.nodeKindSource[nodeVar]; remapped {
			*conditions = append(*conditions, kindSQL+" = '"+SanitizeSQLLiteral(label)+"'")
		} else {
			*conditions = append(*conditions, c.compileNodeLabelPredicate(nVar, label))
		}
	}
	if node.Properties != nil {
		for k, v := range node.Properties {
			valSQL, err := c.visitExpression(v)
			if err == nil {
				switch strings.ToLower(k) {
				case strings.ToLower(c.schema.NodeIDCol), "id":
					*conditions = append(*conditions, idSQL+" = "+valSQL)
				case strings.ToLower(c.schema.NodeKindCol), "kind":
					*conditions = append(*conditions, kindSQL+" = "+valSQL)
				default:
					extractFn := c.schema.Dialect.JSONExtract
					if c.isNumericExpr(v) {
						extractFn = c.schema.Dialect.JSONExtractNumber
					}
					*conditions = append(*conditions,
						extractFn(propsSQL, k)+" = "+valSQL)
				}
			}
		}
	}
}

func (c *Compiler) compileNodeLabelPredicate(nVar, label string) string {
	if c.schema.LabelResolver != nil {
		if pred, ok := c.schema.LabelResolver(nVar, label); ok {
			return pred
		}
	}
	return nVar + "." + c.schema.NodeKindCol + " = '" + SanitizeSQLLiteral(label) + "'"
}

func (c *Compiler) processWithClauses(
	whereConditions *[]string,
	groupByColumns *[]string,
	havingConditions *[]string,
) error {
	for withIndex, with := range c.query.WithClauses {
		var prevWith *ast.WithClause
		if withIndex > 0 {
			prevWith = &c.query.WithClauses[withIndex-1]
		}

		isStageSplit := false
		for _, item := range with.Items {
			if c.hasAggregation(item.Expression) {
				refs := c.getReferencedIdentifiers(item.Expression)
				for id := range refs {
					if c.aggregatedAliases[id] {
						isStageSplit = true
						break
					}
				}
				if prevWith != nil && (prevWith.IsDistinct || len(prevWith.OrderBy) > 0 || prevWith.Skip != nil || prevWith.Limit != nil) {
					isStageSplit = true
				}
			}
		}

		if isStageSplit {
			c.processStageSplitCTE(withIndex, whereConditions, groupByColumns, havingConditions)
		}

		hasAgg := false
		for _, item := range with.Items {
			if c.hasAggregation(item.Expression) {
				hasAgg = true
				break
			}
		}

		for _, item := range with.Items {
			if _, ok := item.Expression.(ast.WildcardExpr); ok {
				continue
			}
			alias := item.Alias
			if alias == "" {
				if id, ok := item.Expression.(ast.IdentifierExpr); ok {
					alias = id.Name
				}
			}
			if alias != "" {
				isDirectEntity := false
				if id, ok := item.Expression.(ast.IdentifierExpr); ok && strings.EqualFold(id.Name, alias) {
					if c.declaredNodes[id.Name] || c.declaredRels[id.Name] {
						isDirectEntity = true
					}
				}
				if !isDirectEntity {
					exprSQL, err := c.visitExpression(item.Expression)
					if err != nil {
						return err
					}
					c.withAliases[alias] = exprSQL
				}
				if c.hasAggregation(item.Expression) {
					c.aggregatedAliases[alias] = true
				}
			}

			if hasAgg {
				if c.hasAggregation(item.Expression) {
					continue
				}
				if id, ok := item.Expression.(ast.IdentifierExpr); ok {
					if c.declaredNodes[id.Name] {
						*groupByColumns = append(*groupByColumns, c.getNodeIDSQL(id.Name))
					} else if c.declaredRels[id.Name] {
						if c.schema.Dialect.Name() == "clickhouse" || c.schema.Dialect.Name() == "postgres" {
							*groupByColumns = append(*groupByColumns, c.escapeVar(id.Name)+"."+c.schema.EdgeFromCol)
						} else {
							*groupByColumns = append(*groupByColumns, c.escapeVar(id.Name)+".rowid")
						}
					} else {
						exprSQL, _ := c.visitExpression(item.Expression)
						*groupByColumns = append(*groupByColumns, exprSQL)
					}
				} else {
					exprSQL, _ := c.visitExpression(item.Expression)
					*groupByColumns = append(*groupByColumns, exprSQL)
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

	if len(c.aggregatedAliases) > 0 {
		returnHasAgg := false
		for _, item := range c.query.Return.Items {
			if c.hasAggregation(item.Expression) {
				returnHasAgg = true
				break
			}
		}
		unwindRefsAgg := false
		for _, u := range c.query.Unwinds {
			for id := range c.getReferencedIdentifiers(u.Expression) {
				if c.aggregatedAliases[id] {
					unwindRefsAgg = true
					break
				}
			}
		}
		if returnHasAgg || unwindRefsAgg {
			c.processStageSplitCTE(len(c.query.WithClauses), whereConditions, groupByColumns, havingConditions)
		}
	}

	return nil
}

func (c *Compiler) processStageSplitCTE(
	withIndex int,
	whereConditions *[]string,
	groupByColumns *[]string,
	havingConditions *[]string,
) {
	stageName := "_stage_" + strconv.Itoa(len(c.ctes)+1)
	remainingReferenced := c.collectRemainingReferencedIdentifiers(withIndex)
	var stageSelectCols []string

	for node := range c.declaredNodes {
		if remainingReferenced[node] {
			idExpr := c.getNodeIDSQL(node)
			kindExpr := c.getNodeKindSQL(node)
			propsExpr := c.getNodePropsSQL(node)
			stageSelectCols = append(stageSelectCols,
				idExpr+" AS "+node+"_id",
				kindExpr+" AS "+node+"_kind",
				propsExpr+" AS "+node+"_properties",
			)
			c.nodeIDSource[node] = stageName + "." + node + "_id"
			c.nodeKindSource[node] = stageName + "." + node + "_kind"
			c.nodePropsSource[node] = stageName + "." + node + "_properties"
		}
	}

	for alias, exprSQL := range c.withAliases {
		if remainingReferenced[alias] {
			quoted := `"` + strings.ReplaceAll(alias, `"`, `""`) + `"`
			stageSelectCols = append(stageSelectCols, exprSQL+" AS "+quoted)
			c.withAliases[alias] = stageName + "." + quoted
		}
	}

	if len(stageSelectCols) == 0 {
		stageSelectCols = []string{"1"}
	}

	var prevWith *ast.WithClause
	if withIndex > 0 {
		prevWith = &c.query.WithClauses[withIndex-1]
	}
	var curWith *ast.WithClause
	if withIndex < len(c.query.WithClauses) {
		curWith = &c.query.WithClauses[withIndex]
	}

	var stageGroupKeys []string
	if !(prevWith != nil && prevWith.IsDistinct && len(*groupByColumns) == 0) {
		if prevWith != nil {
			for _, prevItem := range prevWith.Items {
				if id, ok := prevItem.Expression.(ast.IdentifierExpr); ok && c.declaredNodes[id.Name] {
					stageGroupKeys = append(stageGroupKeys, c.escapeVar(id.Name)+"."+c.schema.NodeIDCol)
				}
			}
		}
		if len(stageGroupKeys) == 0 {
			stageGroupKeys = append(stageGroupKeys, (*groupByColumns)...)
		}
	}

	seenGroup := make(map[string]bool)
	var uniqueGroup []string
	for _, g := range stageGroupKeys {
		if !seenGroup[g] {
			seenGroup[g] = true
			uniqueGroup = append(uniqueGroup, g)
		}
	}

	var orderItems []string
	if prevWith != nil && len(prevWith.OrderBy) > 0 {
		for _, ob := range prevWith.OrderBy {
			if s, err := c.visitExpression(ob.Expression); err == nil {
				dir := "ASC"
				if ob.IsDesc {
					dir = "DESC"
				}
				orderItems = append(orderItems, s+" "+dir)
			}
		}
	}

	limitSQL := ""
	offsetSQL := ""
	if prevWith != nil && (prevWith.Limit != nil || prevWith.Skip != nil) {
		if prevWith.Limit != nil {
			if l, err := c.visitExpression(prevWith.Limit); err == nil {
				limitSQL = l
			}
		} else {
			limitSQL = "-1"
		}
		if prevWith.Skip != nil {
			if s, err := c.visitExpression(prevWith.Skip); err == nil {
				offsetSQL = s
			}
		}
	}

	stageModel := &QueryModel{
		IsDistinct: (prevWith != nil && prevWith.IsDistinct) || (curWith != nil && curWith.IsDistinct),
		Columns:    stageSelectCols,
		FromTable:  c.fromTable,
		FromAlias:  c.fromAlias,
		Joins:      c.joins,
		Where:      strings.Join(*whereConditions, " AND "),
		GroupBy:    uniqueGroup,
		Having:     strings.Join(*havingConditions, " AND "),
		OrderBy:    orderItems,
		Limit:      limitSQL,
		Offset:     offsetSQL,
	}
	stageBody := RenderQuery(stageModel)
	c.ctes = append(c.ctes, stageName+" AS (\n"+stageBody+"\n)")

	c.fromTable = stageName
	c.fromAlias = ""
	c.joins = nil
	*whereConditions = nil
	*groupByColumns = nil
	*havingConditions = nil
	c.aggregatedAliases = make(map[string]bool)
}

func (c *Compiler) collectRemainingReferencedIdentifiers(withIndex int) map[string]bool {
	res := make(map[string]bool)
	for i := withIndex; i < len(c.query.WithClauses); i++ {
		w := c.query.WithClauses[i]
		for _, it := range w.Items {
			c.collectIdentifiers(it.Expression, res)
		}
		if w.Where != nil {
			c.collectIdentifiers(w.Where, res)
		}
		for _, ob := range w.OrderBy {
			c.collectIdentifiers(ob.Expression, res)
		}
	}
	for _, u := range c.query.Unwinds {
		c.collectIdentifiers(u.Expression, res)
	}
	if c.query.Where != nil {
		c.collectIdentifiers(c.query.Where, res)
	}
	for _, it := range c.query.Return.Items {
		c.collectIdentifiers(it.Expression, res)
	}
	for _, ob := range c.query.OrderBy {
		c.collectIdentifiers(ob.Expression, res)
	}
	return res
}

func (c *Compiler) isReservedColumnAlias(alias string) bool {
	switch strings.ToLower(alias) {
	case "id", "kind", "properties", "from_id", "to_id", "rowid",
		strings.ToLower(c.schema.NodeIDCol),
		strings.ToLower(c.schema.NodeKindCol),
		strings.ToLower(c.schema.NodePropsCol),
		strings.ToLower(c.schema.EdgeFromCol),
		strings.ToLower(c.schema.EdgeToCol),
		strings.ToLower(c.schema.EdgeKindCol),
		strings.ToLower(c.schema.EdgePropsCol):
		return true
	}
	return false
}

func (c *Compiler) populateReturnGroupBy(ret ast.ReturnClause, groupByColumns *[]string) {
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
		if _, ok := item.Expression.(ast.WildcardExpr); ok {
			continue
		}
		if id, ok := item.Expression.(ast.IdentifierExpr); ok {
			if c.declaredNodes[id.Name] {
				*groupByColumns = append(*groupByColumns, c.getNodeIDSQL(id.Name))
			} else if c.declaredRels[id.Name] {
				if c.schema.Dialect.Name() == "clickhouse" || c.schema.Dialect.Name() == "postgres" {
					*groupByColumns = append(*groupByColumns, c.escapeVar(id.Name)+"."+c.schema.EdgeFromCol)
				} else {
					*groupByColumns = append(*groupByColumns, c.escapeVar(id.Name)+".rowid")
				}
			} else if item.Alias != "" && !c.isReservedColumnAlias(item.Alias) {
				*groupByColumns = append(*groupByColumns, `"`+item.Alias+`"`)
			} else {
				exprSQL, _ := c.visitExpression(item.Expression)
				*groupByColumns = append(*groupByColumns, exprSQL)
			}
		} else if item.Alias != "" && !c.isReservedColumnAlias(item.Alias) {
			*groupByColumns = append(*groupByColumns, `"`+item.Alias+`"`)
		} else {
			exprSQL, _ := c.visitExpression(item.Expression)
			*groupByColumns = append(*groupByColumns, exprSQL)
		}
	}

	// PostgreSQL strictly requires anchor node IDs referenced by decomposed correlated subqueries
	// to appear in GROUP BY (SQLSTATE 42803).
	if c.schema.Dialect.Name() == "postgres" && len(c.decomposedBranches) > 0 {
		seenRoots := make(map[string]bool)
		for _, col := range *groupByColumns {
			seenRoots[col] = true
		}
		for _, branch := range c.decomposedBranches {
			if branch.RootVar != "" && c.declaredNodes[branch.RootVar] {
				idCol := c.getNodeIDSQL(branch.RootVar)
				if !seenRoots[idCol] {
					seenRoots[idCol] = true
					*groupByColumns = append(*groupByColumns, idCol)
				}
			}
		}
	}
}

func (c *Compiler) buildSelectColumns(ret ast.ReturnClause) ([]string, error) {
	var cols []string
	for _, item := range ret.Items {
		if _, ok := item.Expression.(ast.WildcardExpr); ok {
			cols = append(cols, "*")
			continue
		}
		exprSQL, err := c.visitExpression(item.Expression)
		if err != nil {
			return nil, err
		}
		alias := item.Alias
		if alias == "" {
			switch e := item.Expression.(type) {
			case ast.IdentifierExpr:
				alias = e.Name
			case ast.PropertyAccessExpr:
				if e.Variable != "" {
					alias = e.Variable + "." + e.Property
				} else {
					alias = e.Property
				}
			}
		}
		if alias != "" {
			cols = append(cols, exprSQL+` AS "`+strings.ReplaceAll(alias, `"`, `""`)+`"`)
		} else {
			cols = append(cols, exprSQL)
		}
	}
	return cols, nil
}

func (c *Compiler) assembleQuerySQL(
	selectColumns []string,
	whereConditions []string,
	groupByColumns []string,
	havingConditions []string,
) string {
	// Deduplicate groupBy columns
	seen := make(map[string]bool)
	var uniqueGroup []string
	for _, col := range groupByColumns {
		if !seen[col] {
			seen[col] = true
			uniqueGroup = append(uniqueGroup, col)
		}
	}

	effectiveOrderBy := c.query.OrderBy
	effectiveLimit := c.query.Limit
	effectiveSkip := c.query.Skip
	if len(c.query.WithClauses) == 1 {
		lastWith := c.query.WithClauses[0]
		if len(effectiveOrderBy) == 0 && len(lastWith.OrderBy) > 0 {
			effectiveOrderBy = lastWith.OrderBy
		}
		if effectiveLimit == nil && lastWith.Limit != nil {
			effectiveLimit = lastWith.Limit
		}
		if effectiveSkip == nil && lastWith.Skip != nil {
			effectiveSkip = lastWith.Skip
		}
	}

	var orderItems []string
	for _, item := range effectiveOrderBy {
		exprSQL, err := c.visitExpression(item.Expression)
		if err == nil {
			dir := "ASC"
			if item.IsDesc {
				dir = "DESC"
			}
			orderItems = append(orderItems, exprSQL+" "+dir)
		}
	}

	limitSQL := ""
	if effectiveLimit != nil {
		lim, err := c.visitExpression(effectiveLimit)
		if err == nil {
			limitSQL = lim
		}
	} else if effectiveSkip != nil && c.schema.Dialect.Name() == "sqlite" {
		limitSQL = "-1"
	}

	offsetSQL := ""
	if effectiveSkip != nil {
		skip, err := c.visitExpression(effectiveSkip)
		if err == nil {
			offsetSQL = skip
		}
	}

	fromTable := c.fromTable
	fromAlias := c.fromAlias
	joins := c.joins
	if fromTable == "" && len(joins) > 0 {
		fromTable = joins[0].Table
		fromAlias = joins[0].Alias
		if joins[0].On != "" {
			whereConditions = append([]string{joins[0].On}, whereConditions...)
		}
		joins = joins[1:]
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
		FromTable:  fromTable,
		FromAlias:  fromAlias,
		Joins:      joins,
		Where:      whereSQL,
		GroupBy:    uniqueGroup,
		Having:     havingSQL,
		OrderBy:    orderItems,
		Limit:      limitSQL,
		Offset:     offsetSQL,
	}

	baseSQL := RenderQuery(model)
	return c.appendUnions(baseSQL, c.query.Unions)
}

// processUnwinds compiles UNWIND clauses into relational unnest operations (json_each in SQLite, arrayJoin in ClickHouse).
func (c *Compiler) processUnwinds(unwinds []ast.UnwindClause) error {
	for _, u := range unwinds {
		c.unwindVariables[u.Alias] = true
		exprSQL, err := c.visitExpression(u.Expression)
		if err != nil {
			return err
		}
		escAlias := c.escapeVar(u.Alias)
		isFirst := c.fromTable == ""
		tbl, joinType := c.schema.Dialect.RenderUnwind(exprSQL, escAlias, isFirst)
		if isFirst {
			c.fromTable = tbl
			c.fromAlias = escAlias
		} else {
			c.joins = append(c.joins, JoinModel{
				Type:  joinType,
				Table: tbl,
				Alias: escAlias,
				On:    "",
			})
		}
	}
	return nil
}

// processCalls compiles CALL { ... } subqueries in isolation and binds their returned aliases into subquery expressions.
func (c *Compiler) processCalls(calls []ast.CallClause) error {
	for _, call := range calls {
		subCompiler := NewCompilerWithOptions(call.Subquery, c.initialParams, c.schema)
		for node := range c.declaredNodes {
			subCompiler.declaredNodes[node] = true
		}
		for rel := range c.declaredRels {
			subCompiler.declaredRels[rel] = true
		}
		for k, v := range c.withAliases {
			subCompiler.withAliases[k] = v
		}
		subCompiled, err := subCompiler.Compile()
		if err != nil {
			return err
		}
		for k, v := range subCompiled.Params {
			c.params[k] = v
		}
		for _, item := range call.Subquery.Return.Items {
			alias := item.Alias
			if alias == "" {
				if id, ok := item.Expression.(ast.IdentifierExpr); ok {
					alias = id.Name
				}
			}
			if alias != "" {
				c.withAliases[alias] = "(" + subCompiled.SQL + ")"
			}
		}
	}
	return nil
}

// appendUnions compiles subsequent UNION and UNION ALL queries and appends them to baseSQL.
func (c *Compiler) appendUnions(baseSQL string, unions []ast.UnionClause) string {
	if len(unions) == 0 {
		return baseSQL
	}
	var sb strings.Builder
	sb.WriteString(baseSQL)
	for _, u := range unions {
		subCompiler := NewCompilerWithOptions(u.Query, c.initialParams, c.schema)
		subCompiled, err := subCompiler.Compile()
		if err != nil {
			continue
		}
		for k, v := range subCompiled.Params {
			c.params[k] = v
		}
		sb.WriteString("\n")
		if u.IsAll {
			sb.WriteString("UNION ALL\n")
		} else {
			sb.WriteString("UNION\n")
		}
		sb.WriteString(subCompiled.SQL)
	}
	return sb.String()
}
