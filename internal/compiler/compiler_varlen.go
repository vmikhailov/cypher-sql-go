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
	targetAlreadyDeclared bool,
	joinKeyword string,
	optionalWhereExtra []string,
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
					edgeFilterConds = append(edgeFilterConds,
						c.schema.Dialect.JSONExtract("e."+c.schema.EdgePropsCol, k)+" = "+valSQL)
				}
			}
		}
	}

	edgeFilterSQL := ""
	if len(edgeFilterConds) > 0 {
		edgeFilterSQL = strings.Join(edgeFilterConds, " AND ")
	}

	// Collect filters on endpoints for anchor selection
	prevFilters := append(c.collectNodePatternFilters(prevNode, "seed_n"), c.collectWhereConjunctsForVar(prevVar, "seed_n")...)
	targetFilters := append(c.collectNodePatternFilters(targetNode, "seed_n"), c.collectWhereConjunctsForVar(targetVar, "seed_n")...)

	var anchorEnd string
	if len(prevFilters) > 0 {
		anchorEnd = "prev"
	} else if len(targetFilters) > 0 {
		anchorEnd = "target"
	} else {
		// Neither endpoint has filters: require explicit LIMIT and small hop count
		if c.query.Limit == nil {
			return fmt.Errorf("variable-length path without anchored endpoints requires a filter on at least one endpoint or an explicit LIMIT to prevent combinatorial explosion")
		}
		if maxHops > 3 {
			return fmt.Errorf("variable-length path without anchored endpoints requires explicit small hop limit (<= 3)")
		}
		anchorEnd = "none"
	}

	edgeKey := "char(31) || e." + c.schema.EdgeFromCol + " || char(30) || e." + c.schema.EdgeToCol + " || char(30) || e." + c.schema.EdgeKindCol + " || char(31)"

	cteName := fmt.Sprintf("_vl%d", len(c.ctes)+1)

	var seedWhere []string
	var seedSQL string
	var recursiveSQL string

	if rel.Direction == ast.DirectionOutgoing {
		switch anchorEnd {
		case "prev":
			seedWhere = append(seedWhere, "e."+c.schema.EdgeFromCol+" IN (SELECT "+c.schema.NodeIDCol+" FROM "+c.schema.NodesTable+" seed_n WHERE "+strings.Join(prevFilters, " AND ")+")")
			if edgeFilterSQL != "" {
				seedWhere = append(seedWhere, edgeFilterSQL)
			}
			seedSQL = fmt.Sprintf("SELECT e.%s, e.%s, 1, %s FROM %s e WHERE %s",
				c.schema.EdgeFromCol, c.schema.EdgeToCol, edgeKey, c.schema.EdgesTable, strings.Join(seedWhere, " AND "))

			recJoin := "e." + c.schema.EdgeFromCol + " = v.end_id"
			if edgeFilterSQL != "" {
				recJoin += " AND " + edgeFilterSQL
			}
			recursiveSQL = fmt.Sprintf("SELECT v.start_id, e.%s, v.depth + 1, v.trail || %s FROM %s v JOIN %s e ON %s WHERE v.depth < %d AND instr(v.trail, %s) = 0",
				c.schema.EdgeToCol, edgeKey, cteName, c.schema.EdgesTable, recJoin, maxHops, edgeKey)

		case "target":
			seedWhere = append(seedWhere, "e."+c.schema.EdgeToCol+" IN (SELECT "+c.schema.NodeIDCol+" FROM "+c.schema.NodesTable+" seed_n WHERE "+strings.Join(targetFilters, " AND ")+")")
			if edgeFilterSQL != "" {
				seedWhere = append(seedWhere, edgeFilterSQL)
			}
			seedSQL = fmt.Sprintf("SELECT e.%s, e.%s, 1, %s FROM %s e WHERE %s",
				c.schema.EdgeFromCol, c.schema.EdgeToCol, edgeKey, c.schema.EdgesTable, strings.Join(seedWhere, " AND "))

			recJoin := "e." + c.schema.EdgeToCol + " = v.start_id"
			if edgeFilterSQL != "" {
				recJoin += " AND " + edgeFilterSQL
			}
			recursiveSQL = fmt.Sprintf("SELECT e.%s, v.end_id, v.depth + 1, v.trail || %s FROM %s v JOIN %s e ON %s WHERE v.depth < %d AND instr(v.trail, %s) = 0",
				c.schema.EdgeFromCol, edgeKey, cteName, c.schema.EdgesTable, recJoin, maxHops, edgeKey)

		default: // "none"
			if edgeFilterSQL != "" {
				seedWhere = append(seedWhere, edgeFilterSQL)
			}
			whereClause := ""
			if len(seedWhere) > 0 {
				whereClause = " WHERE " + strings.Join(seedWhere, " AND ")
			}
			seedSQL = fmt.Sprintf("SELECT e.%s, e.%s, 1, %s FROM %s e%s",
				c.schema.EdgeFromCol, c.schema.EdgeToCol, edgeKey, c.schema.EdgesTable, whereClause)

			recJoin := "e." + c.schema.EdgeFromCol + " = v.end_id"
			if edgeFilterSQL != "" {
				recJoin += " AND " + edgeFilterSQL
			}
			recursiveSQL = fmt.Sprintf("SELECT v.start_id, e.%s, v.depth + 1, v.trail || %s FROM %s v JOIN %s e ON %s WHERE v.depth < %d AND instr(v.trail, %s) = 0",
				c.schema.EdgeToCol, edgeKey, cteName, c.schema.EdgesTable, recJoin, maxHops, edgeKey)
		}
	} else { // ast.DirectionIncoming (<-)
		switch anchorEnd {
		case "prev":
			seedWhere = append(seedWhere, "e."+c.schema.EdgeToCol+" IN (SELECT "+c.schema.NodeIDCol+" FROM "+c.schema.NodesTable+" seed_n WHERE "+strings.Join(prevFilters, " AND ")+")")
			if edgeFilterSQL != "" {
				seedWhere = append(seedWhere, edgeFilterSQL)
			}
			seedSQL = fmt.Sprintf("SELECT e.%s, e.%s, 1, %s FROM %s e WHERE %s",
				c.schema.EdgeFromCol, c.schema.EdgeToCol, edgeKey, c.schema.EdgesTable, strings.Join(seedWhere, " AND "))

			recJoin := "e." + c.schema.EdgeToCol + " = v.start_id"
			if edgeFilterSQL != "" {
				recJoin += " AND " + edgeFilterSQL
			}
			recursiveSQL = fmt.Sprintf("SELECT e.%s, v.end_id, v.depth + 1, v.trail || %s FROM %s v JOIN %s e ON %s WHERE v.depth < %d AND instr(v.trail, %s) = 0",
				c.schema.EdgeFromCol, edgeKey, cteName, c.schema.EdgesTable, recJoin, maxHops, edgeKey)

		case "target":
			seedWhere = append(seedWhere, "e."+c.schema.EdgeFromCol+" IN (SELECT "+c.schema.NodeIDCol+" FROM "+c.schema.NodesTable+" seed_n WHERE "+strings.Join(targetFilters, " AND ")+")")
			if edgeFilterSQL != "" {
				seedWhere = append(seedWhere, edgeFilterSQL)
			}
			seedSQL = fmt.Sprintf("SELECT e.%s, e.%s, 1, %s FROM %s e WHERE %s",
				c.schema.EdgeFromCol, c.schema.EdgeToCol, edgeKey, c.schema.EdgesTable, strings.Join(seedWhere, " AND "))

			recJoin := "e." + c.schema.EdgeFromCol + " = v.end_id"
			if edgeFilterSQL != "" {
				recJoin += " AND " + edgeFilterSQL
			}
			recursiveSQL = fmt.Sprintf("SELECT v.start_id, e.%s, v.depth + 1, v.trail || %s FROM %s v JOIN %s e ON %s WHERE v.depth < %d AND instr(v.trail, %s) = 0",
				c.schema.EdgeToCol, edgeKey, cteName, c.schema.EdgesTable, recJoin, maxHops, edgeKey)

		default: // "none"
			if edgeFilterSQL != "" {
				seedWhere = append(seedWhere, edgeFilterSQL)
			}
			whereClause := ""
			if len(seedWhere) > 0 {
				whereClause = " WHERE " + strings.Join(seedWhere, " AND ")
			}
			seedSQL = fmt.Sprintf("SELECT e.%s, e.%s, 1, %s FROM %s e%s",
				c.schema.EdgeFromCol, c.schema.EdgeToCol, edgeKey, c.schema.EdgesTable, whereClause)

			recJoin := "e." + c.schema.EdgeToCol + " = v.start_id"
			if edgeFilterSQL != "" {
				recJoin += " AND " + edgeFilterSQL
			}
			recursiveSQL = fmt.Sprintf("SELECT e.%s, v.end_id, v.depth + 1, v.trail || %s FROM %s v JOIN %s e ON %s WHERE v.depth < %d AND instr(v.trail, %s) = 0",
				c.schema.EdgeFromCol, edgeKey, cteName, c.schema.EdgesTable, recJoin, maxHops, edgeKey)
		}
	}

	cteDef := fmt.Sprintf("%s(start_id, end_id, depth, trail) AS (\n  %s\n  UNION ALL\n  %s\n)",
		cteName, seedSQL, recursiveSQL)
	c.ctes = append(c.ctes, cteDef)

	var cteOnConds []string
	if rel.Direction == ast.DirectionOutgoing {
		cteOnConds = append(cteOnConds,
			escRel+".start_id = "+escPrev+"."+c.schema.NodeIDCol,
			escRel+".depth >= "+strconv.Itoa(minHops))
		if targetAlreadyDeclared {
			cteOnConds = append(cteOnConds, escTarget+"."+c.schema.NodeIDCol+" = "+escRel+".end_id")
			c.addNodeFiltersToConditions(targetNode, targetVar, &cteOnConds)
			cteOnConds = append(cteOnConds, optionalWhereExtra...)
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: cteName,
				Alias: escRel,
				On:    strings.Join(cteOnConds, " AND "),
			})
		} else {
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: cteName,
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
			escRel+".end_id = "+escPrev+"."+c.schema.NodeIDCol,
			escRel+".depth >= "+strconv.Itoa(minHops))
		if targetAlreadyDeclared {
			cteOnConds = append(cteOnConds, escTarget+"."+c.schema.NodeIDCol+" = "+escRel+".start_id")
			c.addNodeFiltersToConditions(targetNode, targetVar, &cteOnConds)
			cteOnConds = append(cteOnConds, optionalWhereExtra...)
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: cteName,
				Alias: escRel,
				On:    strings.Join(cteOnConds, " AND "),
			})
		} else {
			c.joins = append(c.joins, JoinModel{
				Type:  joinKeyword,
				Table: cteName,
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

	return nil
}

func (c *Compiler) collectNodePatternFilters(node ast.NodePattern, seedAlias string) []string {
	var conds []string
	c.addNodeFiltersToConditions(node, seedAlias, &conds)
	return conds
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
