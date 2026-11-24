package compiler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vmikhailov/cypher-sql-go/internal/ast"
)

// processVarLenStep compiles a variable-length relationship hop into a recursive CTE.
func (c *Compiler) processVarLenStep(
	prevNode ast.NodePattern,
	prevVar string,
	rel ast.RelationshipPattern,
	targetNode ast.NodePattern,
	targetVar string,
	relVar string,
	prevDeclared bool,
	targetAlreadyDeclared bool,
	isOptional bool,
	joinKeyword string,
	optionalWhereExtra []string,
	isShortestPath bool,
	pathVar string,
) error {
	minHops := 1
	if rel.MinHops != nil {
		minHops = *rel.MinHops
	}
	maxHops := 10
	if rel.MaxHops != nil {
		maxHops = *rel.MaxHops
	}

	escRel := c.escapeVar(relVar)
	escTarget := c.escapeVar(targetVar)
	escPrev := c.escapeVar(prevVar)

	c.declaredRels[relVar] = true
	c.varLenRels[relVar] = true
	c.varLenRels[escRel] = true

	// Collect edge filters (types + properties)
	var edgeFilterConds []string
	if len(rel.Types) == 1 {
		edgeFilterConds = append(edgeFilterConds, "e."+c.schema.EdgeKindCol+" = '"+SanitizeSQLLiteral(rel.Types[0])+"'")
	} else if len(rel.Types) > 1 {
		var escapedTypes []string
		for _, t := range rel.Types {
			escapedTypes = append(escapedTypes, SanitizeSQLLiteral(t))
		}
		edgeFilterConds = append(edgeFilterConds, "e."+c.schema.EdgeKindCol+" IN ('"+strings.Join(escapedTypes, "', '")+"')")
	}

	if rel.Properties != nil {
		for k, v := range rel.Properties {
			valSQL, err := c.visitExpression(v)
			if err == nil {
				switch strings.ToLower(k) {
				case strings.ToLower(c.schema.EdgeKindCol), "kind", "type":
					edgeFilterConds = append(edgeFilterConds, "e."+c.schema.EdgeKindCol+" = "+valSQL)
				case strings.ToLower(c.schema.EdgeFromCol), "from", "from_id":
					edgeFilterConds = append(edgeFilterConds, "e."+c.schema.EdgeFromCol+" = "+valSQL)
				case strings.ToLower(c.schema.EdgeToCol), "to", "to_id":
					edgeFilterConds = append(edgeFilterConds, "e."+c.schema.EdgeToCol+" = "+valSQL)
				default:
					extractFn := c.schema.Dialect.JSONExtract
					if c.isNumericExpr(v) {
						extractFn = c.schema.Dialect.JSONExtractNumber
					}
					edgeFilterConds = append(edgeFilterConds,
						extractFn("e."+c.schema.EdgePropsCol, k)+" = "+valSQL)
				}
			}
		}
	}

	edgeFilterSQL := ""
	if len(edgeFilterConds) > 0 {
		edgeFilterSQL = strings.Join(edgeFilterConds, " AND ")
	}

	// Collect filters on endpoints (including 1-hop anchored neighbors) for anchor selection
	prevNeighborConds := c.collectNeighborAnchorFilters(prevVar, "seed_n")
	targetNeighborConds := c.collectNeighborAnchorFilters(targetVar, "seed_n")

	prevFilters := append(c.collectNodePatternFilters(prevNode, "seed_n"), c.collectWhereConjunctsForVar(prevVar, "seed_n")...)
	prevFilters = append(prevFilters, prevNeighborConds...)

	targetFilters := append(c.collectNodePatternFilters(targetNode, "seed_n"), c.collectWhereConjunctsForVar(targetVar, "seed_n")...)
	targetFilters = append(targetFilters, targetNeighborConds...)

	prevScore := c.scoreAnchorSelectivity(prevNode, prevVar, len(prevNeighborConds) > 0)
	targetScore := c.scoreAnchorSelectivity(targetNode, targetVar, len(targetNeighborConds) > 0)

	var anchorEnd string
	if prevScore == 0 && targetScore == 0 {
		return fmt.Errorf("variable-length traversal requires an anchored start or target node with property filters (e.g. {id: ...} or WHERE x.name = ...) to prevent full-graph traversal; node label alone (e.g. :Person) is not selective enough")
	}
	if prevScore >= targetScore {
		anchorEnd = "prev"
	} else {
		anchorEnd = "target"
	}

	isUndirected := rel.Direction == ast.DirectionUndirected
	edgesSource := c.schema.EdgesTable
	if isUndirected {
		edgesSource = fmt.Sprintf("(SELECT %s, %s, %s, %s FROM %s UNION ALL SELECT %s AS %s, %s AS %s, %s, %s FROM %s)",
			c.schema.EdgeFromCol, c.schema.EdgeToCol, c.schema.EdgeKindCol, c.schema.EdgePropsCol, c.schema.EdgesTable,
			c.schema.EdgeToCol, c.schema.EdgeFromCol, c.schema.EdgeFromCol, c.schema.EdgeToCol, c.schema.EdgeKindCol, c.schema.EdgePropsCol, c.schema.EdgesTable)
	}

	charFn := "char"
	if c.schema.Dialect.Name() == "postgres" {
		charFn = "chr"
	}
	edgeKey := charFn + "(31) || e." + c.schema.EdgeFromCol + " || " + charFn + "(30) || e." + c.schema.EdgeToCol + " || " + charFn + "(30) || e." + c.schema.EdgeKindCol + " || " + charFn + "(31)"
	if isUndirected {
		edgeKey = fmt.Sprintf("%s(31) || (CASE WHEN e.%s < e.%s THEN e.%s || %s(30) || e.%s ELSE e.%s || %s(30) || e.%s END) || %s(30) || e.%s || %s(31)",
			charFn,
			c.schema.EdgeFromCol, c.schema.EdgeToCol,
			c.schema.EdgeFromCol, charFn, c.schema.EdgeToCol,
			c.schema.EdgeToCol, charFn, c.schema.EdgeFromCol,
			charFn, c.schema.EdgeKindCol, charFn)
	}
	noCycleCheck := "instr(v.trail, " + edgeKey + ") = 0"
	if c.schema.Dialect.Name() == "clickhouse" {
		noCycleCheck = "position(v.trail, " + edgeKey + ") = 0"
	} else if c.schema.Dialect.Name() == "postgres" {
		noCycleCheck = "strpos(v.trail, " + edgeKey + ") = 0"
	}

	cteName := fmt.Sprintf("_vl%d", len(c.ctes)+1)

	var seedWhere []string
	var seedSQL string
	var recursiveSQL string

	if rel.Direction == ast.DirectionOutgoing || isUndirected {
		switch anchorEnd {
		case "prev":
			seedWhere = append(seedWhere, "e."+c.schema.EdgeFromCol+" IN (SELECT "+c.schema.NodeIDCol+" FROM "+c.schema.NodesTable+" seed_n WHERE "+strings.Join(prevFilters, " AND ")+")")
			if edgeFilterSQL != "" {
				seedWhere = append(seedWhere, edgeFilterSQL)
			}
			seedSQL = fmt.Sprintf("SELECT e.%s, e.%s, 1, %s FROM %s e WHERE %s",
				c.schema.EdgeFromCol, c.schema.EdgeToCol, edgeKey, edgesSource, strings.Join(seedWhere, " AND "))

			recJoin := "e." + c.schema.EdgeFromCol + " = v.end_id"
			if edgeFilterSQL != "" {
				recJoin += " AND " + edgeFilterSQL
			}
			recursiveSQL = fmt.Sprintf("SELECT v.start_id, e.%s, v.depth + 1, v.trail || %s FROM %s v JOIN %s e ON %s WHERE v.depth < %d AND %s",
				c.schema.EdgeToCol, edgeKey, cteName, edgesSource, recJoin, maxHops, noCycleCheck)

		case "target":
			seedWhere = append(seedWhere, "e."+c.schema.EdgeToCol+" IN (SELECT "+c.schema.NodeIDCol+" FROM "+c.schema.NodesTable+" seed_n WHERE "+strings.Join(targetFilters, " AND ")+")")
			if edgeFilterSQL != "" {
				seedWhere = append(seedWhere, edgeFilterSQL)
			}
			seedSQL = fmt.Sprintf("SELECT e.%s, e.%s, 1, %s FROM %s e WHERE %s",
				c.schema.EdgeFromCol, c.schema.EdgeToCol, edgeKey, edgesSource, strings.Join(seedWhere, " AND "))

			recJoin := "e." + c.schema.EdgeToCol + " = v.start_id"
			if edgeFilterSQL != "" {
				recJoin += " AND " + edgeFilterSQL
			}
			recursiveSQL = fmt.Sprintf("SELECT e.%s, v.end_id, v.depth + 1, v.trail || %s FROM %s v JOIN %s e ON %s WHERE v.depth < %d AND %s",
				c.schema.EdgeFromCol, edgeKey, cteName, edgesSource, recJoin, maxHops, noCycleCheck)
		}
	} else { // ast.DirectionIncoming (<-)
		switch anchorEnd {
		case "prev":
			seedWhere = append(seedWhere, "e."+c.schema.EdgeToCol+" IN (SELECT "+c.schema.NodeIDCol+" FROM "+c.schema.NodesTable+" seed_n WHERE "+strings.Join(prevFilters, " AND ")+")")
			if edgeFilterSQL != "" {
				seedWhere = append(seedWhere, edgeFilterSQL)
			}
			seedSQL = fmt.Sprintf("SELECT e.%s, e.%s, 1, %s FROM %s e WHERE %s",
				c.schema.EdgeFromCol, c.schema.EdgeToCol, edgeKey, edgesSource, strings.Join(seedWhere, " AND "))

			recJoin := "e." + c.schema.EdgeToCol + " = v.start_id"
			if edgeFilterSQL != "" {
				recJoin += " AND " + edgeFilterSQL
			}
			recursiveSQL = fmt.Sprintf("SELECT e.%s, v.end_id, v.depth + 1, v.trail || %s FROM %s v JOIN %s e ON %s WHERE v.depth < %d AND %s",
				c.schema.EdgeFromCol, edgeKey, cteName, edgesSource, recJoin, maxHops, noCycleCheck)

		case "target":
			seedWhere = append(seedWhere, "e."+c.schema.EdgeFromCol+" IN (SELECT "+c.schema.NodeIDCol+" FROM "+c.schema.NodesTable+" seed_n WHERE "+strings.Join(targetFilters, " AND ")+")")
			if edgeFilterSQL != "" {
				seedWhere = append(seedWhere, edgeFilterSQL)
			}
			seedSQL = fmt.Sprintf("SELECT e.%s, e.%s, 1, %s FROM %s e WHERE %s",
				c.schema.EdgeFromCol, c.schema.EdgeToCol, edgeKey, edgesSource, strings.Join(seedWhere, " AND "))

			recJoin := "e." + c.schema.EdgeFromCol + " = v.end_id"
			if edgeFilterSQL != "" {
				recJoin += " AND " + edgeFilterSQL
			}
			recursiveSQL = fmt.Sprintf("SELECT v.start_id, e.%s, v.depth + 1, v.trail || %s FROM %s v JOIN %s e ON %s WHERE v.depth < %d AND %s",
				c.schema.EdgeToCol, edgeKey, cteName, edgesSource, recJoin, maxHops, noCycleCheck)
		}
	}

	cteDef := fmt.Sprintf("%s(start_id, end_id, depth, trail) AS (\n  %s\n  UNION ALL\n  %s\n)",
		cteName, seedSQL, recursiveSQL)
	c.ctes = append(c.ctes, cteDef)

	cteTable := cteName
	if c.schema.MaxPathTraversals > 0 && c.schema.Dialect.Name() != "clickhouse" {
		cteTable = fmt.Sprintf("(SELECT * FROM %s LIMIT %d)", cteName, c.schema.MaxPathTraversals)
	}

	prevIDSrc := c.getNodeIDSQL(prevVar)
	targetIDSrc := c.getNodeIDSQL(targetVar)

	var cteOnConds []string
	if !prevDeclared && targetAlreadyDeclared {
		// Backward join from already-declared targetVar to undeclared prevVar
		if rel.Direction == ast.DirectionOutgoing || isUndirected {
			cteOnConds = append(cteOnConds,
				escRel+".end_id = "+targetIDSrc,
				escRel+".depth >= "+strconv.Itoa(minHops))
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: cteTable,
				Alias: escRel,
				On:    strings.Join(cteOnConds, " AND "),
			})
			prevOnConds := []string{
				escPrev + "." + c.schema.NodeIDCol + " = " + escRel + ".start_id",
			}
			if isShortestPath {
				prevOnConds = append(prevOnConds, fmt.Sprintf("%s.depth = (SELECT min(depth) FROM %s WHERE start_id = %s.%s AND end_id = %s)",
					escRel, cteName, escPrev, c.schema.NodeIDCol, targetIDSrc))
			}
			c.addNodeFiltersToConditions(prevNode, prevVar, &prevOnConds)
			prevOnConds = append(prevOnConds, optionalWhereExtra...)
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: c.schema.NodesTable,
				Alias: escPrev,
				On:    strings.Join(prevOnConds, " AND "),
			})
		} else { // ast.DirectionIncoming (<-)
			cteOnConds = append(cteOnConds,
				escRel+".start_id = "+targetIDSrc,
				escRel+".depth >= "+strconv.Itoa(minHops))
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: cteTable,
				Alias: escRel,
				On:    strings.Join(cteOnConds, " AND "),
			})
			prevOnConds := []string{
				escPrev + "." + c.schema.NodeIDCol + " = " + escRel + ".end_id",
			}
			if isShortestPath {
				prevOnConds = append(prevOnConds, fmt.Sprintf("%s.depth = (SELECT min(depth) FROM %s WHERE start_id = %s AND end_id = %s.%s)",
					escRel, cteName, targetIDSrc, escPrev, c.schema.NodeIDCol))
			}
			c.addNodeFiltersToConditions(prevNode, prevVar, &prevOnConds)
			prevOnConds = append(prevOnConds, optionalWhereExtra...)
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: c.schema.NodesTable,
				Alias: escPrev,
				On:    strings.Join(prevOnConds, " AND "),
			})
		}
	} else if rel.Direction == ast.DirectionOutgoing || isUndirected {
		cteOnConds = append(cteOnConds,
			escRel+".start_id = "+prevIDSrc,
			escRel+".depth >= "+strconv.Itoa(minHops))
		if isShortestPath {
			cteOnConds = append(cteOnConds, fmt.Sprintf("%s.depth = (SELECT min(depth) FROM %s WHERE start_id = %s AND end_id = %s)",
				escRel, cteName, prevIDSrc, targetIDSrc))
		}
		if targetAlreadyDeclared {
			cteOnConds = append(cteOnConds, targetIDSrc+" = "+escRel+".end_id")
			c.addNodeFiltersToConditions(targetNode, targetVar, &cteOnConds)
			cteOnConds = append(cteOnConds, optionalWhereExtra...)
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: cteTable,
				Alias: escRel,
				On:    strings.Join(cteOnConds, " AND "),
			})
		} else {
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: cteTable,
				Alias: escRel,
				On:    strings.Join(cteOnConds, " AND "),
			})
			targetOnConds := []string{
				escTarget + "." + c.schema.NodeIDCol + " = " + escRel + ".end_id",
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
	} else { // ast.DirectionIncoming (<-)
		cteOnConds = append(cteOnConds,
			escRel+".end_id = "+prevIDSrc,
			escRel+".depth >= "+strconv.Itoa(minHops))
		if isShortestPath {
			cteOnConds = append(cteOnConds, fmt.Sprintf("%s.depth = (SELECT min(depth) FROM %s WHERE start_id = %s AND end_id = %s)",
				escRel, cteName, targetIDSrc, prevIDSrc))
		}
		if targetAlreadyDeclared {
			cteOnConds = append(cteOnConds, targetIDSrc+" = "+escRel+".start_id")
			c.addNodeFiltersToConditions(targetNode, targetVar, &cteOnConds)
			cteOnConds = append(cteOnConds, optionalWhereExtra...)
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: cteTable,
				Alias: escRel,
				On:    strings.Join(cteOnConds, " AND "),
			})
		} else {
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: cteTable,
				Alias: escRel,
				On:    strings.Join(cteOnConds, " AND "),
			})
			targetOnConds := []string{
				escTarget + "." + c.schema.NodeIDCol + " = " + escRel + ".start_id",
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
	}

	c.declaredNodes[prevVar] = true
	c.declaredNodes[targetVar] = true

	if pathVar != "" {
		if c.varAliases == nil {
			c.varAliases = make(map[string]string)
		}
		c.varAliases[pathVar] = escRel
	}

	return nil
}

func (c *Compiler) collectNodePatternFilters(node ast.NodePattern, seedAlias string) []string {
	var conds []string
	c.addNodeFiltersToConditions(node, seedAlias, &conds)
	return conds
}

func (c *Compiler) scoreAnchorSelectivity(node ast.NodePattern, varName string, hasAnchoredNeighbor bool) int {
	score := 0
	if node.Properties != nil {
		for k := range node.Properties {
			lowerK := strings.ToLower(k)
			if lowerK == strings.ToLower(c.schema.NodeIDCol) || lowerK == "id" {
				score += 100
			} else if lowerK != strings.ToLower(c.schema.NodeKindCol) && lowerK != "kind" && lowerK != "label" {
				score += 50
			}
		}
	}
	if c.query.Where != nil {
		for _, conj := range flattenAnd(c.query.Where) {
			if referencesOnlyVar(conj, varName) {
				score += c.scoreWhereExprSelectivity(conj)
			}
		}
	}
	if hasAnchoredNeighbor {
		score += 40
	}
	if score > 0 && len(node.Labels) > 0 {
		score += 10
	}
	return score
}

func (c *Compiler) scoreWhereExprSelectivity(expr ast.Expression) int {
	if b, ok := expr.(ast.BinaryExpr); ok {
		prop := extractPropertyName(b.Left)
		if prop == "" {
			prop = extractPropertyName(b.Right)
		}
		lowerProp := strings.ToLower(prop)
		if lowerProp == "kind" || lowerProp == "label" || lowerProp == strings.ToLower(c.schema.NodeKindCol) {
			return 0
		}
		isID := lowerProp == "id" || lowerProp == strings.ToLower(c.schema.NodeIDCol)
		switch b.Op {
		case ast.OpEq:
			if isID {
				return 100
			}
			return 50
		case ast.OpIn:
			if isID {
				return 80
			}
			return 40
		case ast.OpStarts:
			return 30
		case ast.OpContains, ast.OpEnds, ast.OpLt, ast.OpLte, ast.OpGt, ast.OpGte:
			return 20
		default:
			return 10
		}
	}
	if _, ok := expr.(ast.HasLabelExpr); ok {
		return 0
	}
	return 10
}

func extractPropertyName(expr ast.Expression) string {
	if p, ok := expr.(ast.PropertyAccessExpr); ok {
		return p.Property
	}
	if fn, ok := expr.(ast.FunctionCallExpr); ok && len(fn.Args) == 1 {
		lower := strings.ToLower(fn.Name)
		if lower == "id" || lower == "elementid" {
			return "id"
		}
		return extractPropertyName(fn.Args[0])
	}
	return ""
}

func (c *Compiler) collectNeighborAnchorFilters(endpointVar string, seedAlias string) []string {
	if endpointVar == "" || c.query == nil {
		return nil
	}
	var conds []string
	for _, m := range c.query.Matches {
		if m.IsOptional {
			continue
		}
		for _, p := range m.Paths {
			nodes := []ast.NodePattern{p.Head}
			for _, el := range p.Chain {
				nodes = append(nodes, el.Target)
			}
			for i, el := range p.Chain {
				if el.Relationship.MinHops != nil || el.Relationship.MaxHops != nil {
					continue
				}
				leftNode := nodes[i]
				rightNode := nodes[i+1]
				var neighborNode ast.NodePattern
				var neighborIsLeft bool
				if leftNode.Variable == endpointVar && rightNode.Variable != "" && rightNode.Variable != endpointVar {
					neighborNode = rightNode
					neighborIsLeft = false
				} else if rightNode.Variable == endpointVar && leftNode.Variable != "" && leftNode.Variable != endpointVar {
					neighborNode = leftNode
					neighborIsLeft = true
				} else {
					continue
				}
				if !c.nodeHasPropertyAnchor(neighborNode, neighborNode.Variable) {
					continue
				}
				nbFilters := append(c.collectNodePatternFilters(neighborNode, "nb_n"), c.collectWhereConjunctsForVar(neighborNode.Variable, "nb_n")...)
				if len(nbFilters) == 0 {
					continue
				}
				var edgeConds []string
				if len(el.Relationship.Types) == 1 {
					edgeConds = append(edgeConds, "nb_e."+c.schema.EdgeKindCol+" = '"+SanitizeSQLLiteral(el.Relationship.Types[0])+"'")
				} else if len(el.Relationship.Types) > 1 {
					var escapedTypes []string
					for _, t := range el.Relationship.Types {
						escapedTypes = append(escapedTypes, SanitizeSQLLiteral(t))
					}
					edgeConds = append(edgeConds, "nb_e."+c.schema.EdgeKindCol+" IN ('"+strings.Join(escapedTypes, "', '")+"')")
				}
				var seedCol, nbCol string
				if (!neighborIsLeft && el.Relationship.Direction == ast.DirectionOutgoing) ||
					(neighborIsLeft && el.Relationship.Direction == ast.DirectionIncoming) {
					seedCol = "nb_e." + c.schema.EdgeFromCol
					nbCol = "nb_e." + c.schema.EdgeToCol
				} else if (!neighborIsLeft && el.Relationship.Direction == ast.DirectionIncoming) ||
					(neighborIsLeft && el.Relationship.Direction == ast.DirectionOutgoing) {
					seedCol = "nb_e." + c.schema.EdgeToCol
					nbCol = "nb_e." + c.schema.EdgeFromCol
				} else {
					// Undirected 1-hop neighbor
					edgeWhere := ""
					if len(edgeConds) > 0 {
						edgeWhere = " AND " + strings.Join(edgeConds, " AND ")
					}
					subq := fmt.Sprintf(
						"%s.%s IN (SELECT CASE WHEN nb_e.%s = nb_n.%s THEN nb_e.%s ELSE nb_e.%s END FROM %s nb_e JOIN %s nb_n ON (nb_e.%s = nb_n.%s OR nb_e.%s = nb_n.%s)%s WHERE %s)",
						seedAlias, c.schema.NodeIDCol,
						c.schema.EdgeFromCol, c.schema.NodeIDCol, c.schema.EdgeToCol, c.schema.EdgeFromCol,
						c.schema.EdgesTable, c.schema.NodesTable,
						c.schema.EdgeFromCol, c.schema.NodeIDCol, c.schema.EdgeToCol, c.schema.NodeIDCol,
						edgeWhere, strings.Join(nbFilters, " AND "))
					conds = append(conds, subq)
					continue
				}
				edgeConds = append([]string{nbCol + " = nb_n." + c.schema.NodeIDCol}, edgeConds...)
				subq := fmt.Sprintf("%s.%s IN (SELECT %s FROM %s nb_e JOIN %s nb_n ON %s WHERE %s)",
					seedAlias, c.schema.NodeIDCol, seedCol, c.schema.EdgesTable, c.schema.NodesTable,
					strings.Join(edgeConds, " AND "), strings.Join(nbFilters, " AND "))
				conds = append(conds, subq)
			}
		}
	}
	return conds
}

func (c *Compiler) nodeHasPropertyAnchor(node ast.NodePattern, varName string) bool {
	if node.Properties != nil {
		for k := range node.Properties {
			lowerK := strings.ToLower(k)
			if lowerK != strings.ToLower(c.schema.NodeKindCol) && lowerK != "kind" && lowerK != "label" {
				return true
			}
		}
	}
	whereConds := c.collectWhereConjunctsForVar(varName, "seed_n")
	for _, cond := range whereConds {
		kindPrefix := "seed_n." + c.schema.NodeKindCol + " = "
		if !strings.HasPrefix(cond, kindPrefix) {
			return true
		}
	}
	return false
}

func (c *Compiler) collectWhereConjunctsForVar(varName string, seedAlias string) []string {
	if c.query.Where == nil {
		return nil
	}
	conjuncts := flattenAnd(c.query.Where)
	var res []string
	for _, conj := range conjuncts {
		if referencesOnlyVar(conj, varName) {
			prevAlias, hadAlias := c.varAliases[varName]
			if c.varAliases == nil {
				c.varAliases = make(map[string]string)
			}
			c.varAliases[varName] = seedAlias
			exprSQL, err := c.visitExpression(conj)
			if hadAlias {
				c.varAliases[varName] = prevAlias
			} else {
				delete(c.varAliases, varName)
			}
			if err == nil && exprSQL != "" {
				res = append(res, exprSQL)
			}
		}
	}
	return res
}

func flattenAnd(expr ast.Expression) []ast.Expression {
	if b, ok := expr.(ast.BinaryExpr); ok && b.Op == ast.OpAnd {
		return append(flattenAnd(b.Left), flattenAnd(b.Right)...)
	}
	return []ast.Expression{expr}
}

func referencesOnlyVar(expr ast.Expression, targetVar string) bool {
	vars := collectExprVars(expr)
	if len(vars) == 0 {
		return false
	}
	for _, v := range vars {
		if v != targetVar {
			return false
		}
	}
	return true
}

func collectExprVars(expr ast.Expression) []string {
	if expr == nil {
		return nil
	}
	var vars []string
	switch e := expr.(type) {
	case ast.IdentifierExpr:
		vars = append(vars, e.Name)
	case ast.PropertyAccessExpr:
		vars = append(vars, e.Variable)
	case ast.BinaryExpr:
		vars = append(vars, collectExprVars(e.Left)...)
		vars = append(vars, collectExprVars(e.Right)...)
	case ast.UnaryExpr:
		vars = append(vars, collectExprVars(e.Operand)...)
	case ast.FunctionCallExpr:
		for _, arg := range e.Args {
			vars = append(vars, collectExprVars(arg)...)
		}
	case ast.ListComprehensionExpr:
		vars = append(vars, collectExprVars(e.List)...)
		vars = append(vars, collectExprVars(e.Filter)...)
		vars = append(vars, collectExprVars(e.Projection)...)
	case ast.ListPredicateExpr:
		vars = append(vars, collectExprVars(e.List)...)
		vars = append(vars, collectExprVars(e.Predicate)...)
	}
	return vars
}
