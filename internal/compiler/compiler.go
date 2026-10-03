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

	return &Compiler{
		query:              query,
		schema:             cfg,
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

func (c *Compiler) processMatchClause(match ast.MatchClause, mainWhereConditions *[]string) error {
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

	if !c.declaredNodes[headVar] {
		c.bindHeadNode(path.Head, headVar, isOptional, joinKeyword, mainWhereConditions, optionalWhereExtra)
	}

	if len(path.Chain) == 0 {
		return nil
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

func (c *Compiler) processPathChain(
	path ast.PathPattern,
	headVar string,
	isOptional bool,
	joinKeyword string,
	mainWhereConditions *[]string,
	optionalWhereExtra []string,
) error {
	prevVar := headVar

	for _, elem := range path.Chain {
		rel := elem.Relationship
		targetNode := elem.Target
		targetVar := targetNode.Variable
		if targetVar == "" {
			c.varIndex++
			targetVar = "_n" + strconv.Itoa(c.varIndex)
		}

		relVar := rel.Variable
		if relVar == "" {
			c.varIndex++
			relVar = "_r" + strconv.Itoa(c.varIndex)
		}

		c.declaredRels[relVar] = true
		targetAlreadyDeclared := c.declaredNodes[targetVar]
		c.declaredNodes[targetVar] = true

		escRel := c.escapeVar(relVar)
		escTarget := c.escapeVar(targetVar)
		escPrev := c.escapeVar(prevVar)

		var relOnConds []string
		switch rel.Direction {
		case ast.DirectionOutgoing:
			relOnConds = append(relOnConds,
				escRel+"."+c.schema.EdgeFromCol+" = "+escPrev+"."+c.schema.NodeIDCol)
		case ast.DirectionIncoming:
			relOnConds = append(relOnConds,
				escRel+"."+c.schema.EdgeToCol+" = "+escPrev+"."+c.schema.NodeIDCol)
		case ast.DirectionUndirected:
			relOnConds = append(relOnConds,
				"("+escRel+"."+c.schema.EdgeFromCol+" = "+escPrev+"."+c.schema.NodeIDCol+" OR "+
					escRel+"."+c.schema.EdgeToCol+" = "+escPrev+"."+c.schema.NodeIDCol+")")
		}

		if len(rel.Types) == 1 {
			relOnConds = append(relOnConds, escRel+"."+c.schema.EdgeKindCol+" = '"+SanitizeSQLLiteral(rel.Types[0])+"'")
		} else if len(rel.Types) > 1 {
			var escapedTypes []string
			for _, t := range rel.Types {
				escapedTypes = append(escapedTypes, SanitizeSQLLiteral(t))
			}
			relOnConds = append(relOnConds,
				escRel+"."+c.schema.EdgeKindCol+" IN ('"+strings.Join(escapedTypes, "', '")+"')")
		}

		if rel.Properties != nil {
			for k, v := range rel.Properties {
				valSQL, err := c.visitExpression(v)
				if err == nil {
					switch strings.ToLower(k) {
					case strings.ToLower(c.schema.EdgeKindCol), "kind", "type":
						relOnConds = append(relOnConds, escRel+"."+c.schema.EdgeKindCol+" = "+valSQL)
					case strings.ToLower(c.schema.EdgeFromCol), "from", "from_id":
						relOnConds = append(relOnConds, escRel+"."+c.schema.EdgeFromCol+" = "+valSQL)
					case strings.ToLower(c.schema.EdgeToCol), "to", "to_id":
						relOnConds = append(relOnConds, escRel+"."+c.schema.EdgeToCol+" = "+valSQL)
					default:
						relOnConds = append(relOnConds,
							c.schema.Dialect.JSONExtract(escRel+"."+c.schema.EdgePropsCol, k)+" = "+valSQL)
					}
				}
			}
		}

		if targetAlreadyDeclared {
			switch rel.Direction {
			case ast.DirectionOutgoing:
				relOnConds = append(relOnConds,
					escTarget+"."+c.schema.NodeIDCol+" = "+escRel+"."+c.schema.EdgeToCol)
			case ast.DirectionIncoming:
				relOnConds = append(relOnConds,
					escTarget+"."+c.schema.NodeIDCol+" = "+escRel+"."+c.schema.EdgeFromCol)
			case ast.DirectionUndirected:
				relOnConds = append(relOnConds,
					escTarget+"."+c.schema.NodeIDCol+" = CASE WHEN "+
						escRel+"."+c.schema.EdgeFromCol+" = "+escPrev+"."+c.schema.NodeIDCol+" THEN "+
						escRel+"."+c.schema.EdgeToCol+" ELSE "+escRel+"."+c.schema.EdgeFromCol+" END")
			}
			c.addNodeFiltersToConditions(targetNode, targetVar, &relOnConds)
			relOnConds = append(relOnConds, optionalWhereExtra...)

			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: c.schema.EdgesTable,
				Alias: escRel,
				On:    strings.Join(relOnConds, " AND "),
			})
		} else {
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: c.schema.EdgesTable,
				Alias: escRel,
				On:    strings.Join(relOnConds, " AND "),
			})

			var targetOnConds []string
			switch rel.Direction {
			case ast.DirectionOutgoing:
				targetOnConds = append(targetOnConds,
					escTarget+"."+c.schema.NodeIDCol+" = "+escRel+"."+c.schema.EdgeToCol)
			case ast.DirectionIncoming:
				targetOnConds = append(targetOnConds,
					escTarget+"."+c.schema.NodeIDCol+" = "+escRel+"."+c.schema.EdgeFromCol)
			case ast.DirectionUndirected:
				targetOnConds = append(targetOnConds,
					escTarget+"."+c.schema.NodeIDCol+" = CASE WHEN "+
						escRel+"."+c.schema.EdgeFromCol+" = "+escPrev+"."+c.schema.NodeIDCol+" THEN "+
						escRel+"."+c.schema.EdgeToCol+" ELSE "+escRel+"."+c.schema.EdgeFromCol+" END")
			}

			c.addNodeFiltersToConditions(targetNode, targetVar, &targetOnConds)
			targetOnConds = append(targetOnConds, optionalWhereExtra...)

			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: c.schema.NodesTable,
				Alias: escTarget,
				On:    strings.Join(targetOnConds, " AND "),
			})
		}

		prevVar = targetVar
	}
	return nil
}

func (c *Compiler) addNodeFiltersToConditions(node ast.NodePattern, nodeVar string, conditions *[]string) {
	nVar := c.escapeVar(nodeVar)
	for _, label := range node.Labels {
		*conditions = append(*conditions, c.compileNodeLabelPredicate(nVar, label))
	}
	if node.Properties != nil {
		for k, v := range node.Properties {
			valSQL, err := c.visitExpression(v)
			if err == nil {
				switch strings.ToLower(k) {
				case strings.ToLower(c.schema.NodeIDCol), "id":
					*conditions = append(*conditions, nVar+"."+c.schema.NodeIDCol+" = "+valSQL)
				case strings.ToLower(c.schema.NodeKindCol), "kind":
					*conditions = append(*conditions, nVar+"."+c.schema.NodeKindCol+" = "+valSQL)
				default:
					*conditions = append(*conditions,
						c.schema.Dialect.JSONExtract(nVar+"."+c.schema.NodePropsCol, k)+" = "+valSQL)
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
				if id, ok := item.Expression.(ast.IdentifierExpr); ok {
					if c.declaredNodes[id.Name] {
						*groupByColumns = append(*groupByColumns, c.escapeVar(id.Name)+"."+c.schema.NodeIDCol)
					} else if c.declaredRels[id.Name] {
						*groupByColumns = append(*groupByColumns, c.escapeVar(id.Name)+".rowid")
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
				*groupByColumns = append(*groupByColumns, c.escapeVar(id.Name)+"."+c.schema.NodeIDCol)
			} else if c.declaredRels[id.Name] {
				*groupByColumns = append(*groupByColumns, c.escapeVar(id.Name)+".rowid")
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

	var orderItems []string
	for _, item := range c.query.OrderBy {
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
