package cyphersql

import (
	"strings"
)

type decomposedBranch struct {
	Match          MatchClause
	Path           PathPattern
	RootVar        string
	IntroducedVars map[string]bool
	IsCompiled     bool
}

func (c *Compiler) detectDecomposedOptionalMatches(query *Query) {
	optionalCount := 0
	for _, m := range query.Matches {
		if m.IsOptional {
			optionalCount++
		}
	}
	if optionalCount < 2 {
		return
	}

	declaredSoFar := make(map[string]bool)
	for _, match := range query.Matches {
		if !match.IsOptional {
			for _, path := range match.Paths {
				if path.Head.Variable != "" {
					declaredSoFar[path.Head.Variable] = true
				}
				for _, elem := range path.Chain {
					if elem.Relationship.Variable != "" {
						declaredSoFar[elem.Relationship.Variable] = true
					}
					if elem.Target.Variable != "" {
						declaredSoFar[elem.Target.Variable] = true
					}
				}
			}
		}
	}

	var candidateBranches []*decomposedBranch

	for matchIdx, match := range query.Matches {
		if !match.IsOptional || len(match.Paths) != 1 {
			continue
		}
		path := match.Paths[0]
		headVar := path.Head.Variable
		if headVar == "" || !declaredSoFar[headVar] || len(path.Chain) == 0 {
			continue
		}

		introduced := make(map[string]bool)
		referencesOtherOptional := false

		for _, elem := range path.Chain {
			if elem.Relationship.Variable != "" {
				if declaredSoFar[elem.Relationship.Variable] {
					referencesOtherOptional = true
				} else {
					introduced[elem.Relationship.Variable] = true
				}
			}
			if elem.Target.Variable != "" {
				if declaredSoFar[elem.Target.Variable] {
					referencesOtherOptional = true
				} else {
					introduced[elem.Target.Variable] = true
				}
			}
		}

		if referencesOtherOptional || len(introduced) == 0 {
			continue
		}

		// Verify that all introduced variables are only used inside aggregations (collect, count)
		allAggregated := true
		for v := range introduced {
			if hasVariableOutsideAggregation(query, matchIdx, v) {
				allAggregated = false
				break
			}
		}

		if allAggregated {
			candidateBranches = append(candidateBranches, &decomposedBranch{
				Match:          match,
				Path:           path,
				RootVar:        headVar,
				IntroducedVars: introduced,
			})
		}
	}

	// Decompose only when 2 or more independent optional branches exist
	if len(candidateBranches) >= 2 {
		for _, branch := range candidateBranches {
			for v := range branch.IntroducedVars {
				c.decomposedBranches[v] = branch
			}
		}
	}
}

func (c *Compiler) isMatchDecomposed(match MatchClause) bool {
	if !match.IsOptional || len(c.decomposedBranches) == 0 {
		return false
	}
	for _, path := range match.Paths {
		for _, elem := range path.Chain {
			if elem.Target.Variable != "" && c.decomposedBranches[elem.Target.Variable] != nil {
				return true
			}
			if elem.Relationship.Variable != "" && c.decomposedBranches[elem.Relationship.Variable] != nil {
				return true
			}
		}
	}
	return false
}

func hasVariableOutsideAggregation(query *Query, currentMatchIdx int, varName string) bool {
	// 1. Check other MATCH clauses
	for idx, match := range query.Matches {
		if idx == currentMatchIdx {
			continue
		}
		if match.Where != nil && hasVariable(match.Where, varName) {
			return true
		}
		for _, p := range match.Paths {
			if p.Head.Variable == varName {
				return true
			}
			for _, elem := range p.Chain {
				if elem.Relationship.Variable == varName || elem.Target.Variable == varName {
					return true
				}
			}
		}
	}

	// 2. Check top-level WHERE
	if query.Where != nil && hasVariableOutsideAggregationExpr(query.Where, varName, false) {
		return true
	}

	// 3. Check WITH clauses
	carriedAliases := make(map[string]bool)
	for _, with := range query.WithClauses {
		if with.Where != nil {
			if hasVariableOutsideAggregationExpr(with.Where, varName, false) {
				return true
			}
			for alias := range carriedAliases {
				if hasVariable(with.Where, alias) {
					return true
				}
			}
		}

		for _, item := range with.Items {
			if hasVariableOutsideAggregationExpr(item.Expression, varName, false) {
				return true
			}
			for alias := range carriedAliases {
				if hasVariable(item.Expression, alias) {
					return true
				}
			}
			if item.Alias != "" && hasVariable(item.Expression, varName) {
				carriedAliases[item.Alias] = true
			}
		}
	}

	if len(carriedAliases) > 0 {
		for alias := range carriedAliases {
			for _, item := range query.Return.Items {
				if hasVariableOutsideAggregationExpr(item.Expression, alias, false) {
					return true
				}
			}
		}
	}

	// 4. Check RETURN clause
	for _, item := range query.Return.Items {
		if hasVariableOutsideAggregationExpr(item.Expression, varName, false) {
			return true
		}
	}

	// 5. Check ORDER BY
	for _, item := range query.OrderBy {
		if hasVariableOutsideAggregationExpr(item.Expression, varName, false) {
			return true
		}
	}

	return false
}

func hasVariable(expr Expression, varName string) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case IdentifierExpr:
		return strings.EqualFold(e.Name, varName)
	case PropertyAccessExpr:
		return strings.EqualFold(e.Variable, varName)
	case BinaryExpr:
		return hasVariable(e.Left, varName) || hasVariable(e.Right, varName)
	case UnaryExpr:
		return hasVariable(e.Operand, varName)
	case FunctionCallExpr:
		for _, arg := range e.Args {
			if hasVariable(arg, varName) {
				return true
			}
		}
		return false
	case ListExpr:
		for _, item := range e.Items {
			if hasVariable(item, varName) {
				return true
			}
		}
		return false
	case MapExpr:
		for _, v := range e.Entries {
			if hasVariable(v, varName) {
				return true
			}
		}
		return false
	case CaseExpr:
		if e.Test != nil && hasVariable(e.Test, varName) {
			return true
		}
		for _, w := range e.WhenBranches {
			if hasVariable(w.When, varName) || hasVariable(w.Then, varName) {
				return true
			}
		}
		return e.Else != nil && hasVariable(e.Else, varName)
	case ListComprehensionExpr:
		return hasVariable(e.List, varName) ||
			(e.Filter != nil && hasVariable(e.Filter, varName)) ||
			(e.Projection != nil && hasVariable(e.Projection, varName))
	case ListPredicateExpr:
		return hasVariable(e.List, varName) || hasVariable(e.Predicate, varName)
	default:
		return false
	}
}

func hasVariableOutsideAggregationExpr(expr Expression, varName string, insideAgg bool) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case FunctionCallExpr:
		lower := strings.ToLower(e.Name)
		if lower == "collect" || lower == "count" {
			for _, arg := range e.Args {
				if hasVariableOutsideAggregationExpr(arg, varName, true) {
					return true
				}
			}
			return false
		}
		for _, arg := range e.Args {
			if hasVariableOutsideAggregationExpr(arg, varName, insideAgg) {
				return true
			}
		}
		return false
	case IdentifierExpr:
		if strings.EqualFold(e.Name, varName) {
			return !insideAgg
		}
		return false
	case PropertyAccessExpr:
		if strings.EqualFold(e.Variable, varName) {
			return !insideAgg
		}
		return false
	case BinaryExpr:
		return hasVariableOutsideAggregationExpr(e.Left, varName, insideAgg) ||
			hasVariableOutsideAggregationExpr(e.Right, varName, insideAgg)
	case UnaryExpr:
		return hasVariableOutsideAggregationExpr(e.Operand, varName, insideAgg)
	case ListExpr:
		for _, item := range e.Items {
			if hasVariableOutsideAggregationExpr(item, varName, insideAgg) {
				return true
			}
		}
		return false
	case MapExpr:
		for _, v := range e.Entries {
			if hasVariableOutsideAggregationExpr(v, varName, insideAgg) {
				return true
			}
		}
		return false
	case CaseExpr:
		if e.Test != nil && hasVariableOutsideAggregationExpr(e.Test, varName, insideAgg) {
			return true
		}
		for _, w := range e.WhenBranches {
			if hasVariableOutsideAggregationExpr(w.When, varName, insideAgg) ||
				hasVariableOutsideAggregationExpr(w.Then, varName, insideAgg) {
				return true
			}
		}
		return e.Else != nil && hasVariableOutsideAggregationExpr(e.Else, varName, insideAgg)
	case ListComprehensionExpr:
		return hasVariableOutsideAggregationExpr(e.List, varName, insideAgg) ||
			(e.Filter != nil && hasVariableOutsideAggregationExpr(e.Filter, varName, insideAgg)) ||
			(e.Projection != nil && hasVariableOutsideAggregationExpr(e.Projection, varName, insideAgg))
	case ListPredicateExpr:
		return hasVariableOutsideAggregationExpr(e.List, varName, insideAgg) ||
			hasVariableOutsideAggregationExpr(e.Predicate, varName, insideAgg)
	default:
		return false
	}
}

func (c *Compiler) tryCompileDecomposedAggregation(funcExpr FunctionCallExpr, distinctStr string) string {
	if len(funcExpr.Args) != 1 {
		return ""
	}

	referenced := c.getReferencedIdentifiers(funcExpr.Args[0])
	var branch *decomposedBranch
	for id := range referenced {
		if b, ok := c.decomposedBranches[id]; ok {
			branch = b
			break
		}
	}

	if branch == nil {
		return ""
	}

	fromJoins, conditions := c.buildSubqueryPath(branch.Path, "_dec")

	var addedNodes []string
	var addedRels []string

	if branch.Path.Head.Variable != "" && !c.declaredNodes[branch.Path.Head.Variable] {
		c.declaredNodes[branch.Path.Head.Variable] = true
		addedNodes = append(addedNodes, branch.Path.Head.Variable)
	}

	for _, elem := range branch.Path.Chain {
		if elem.Relationship.Variable != "" && !c.declaredRels[elem.Relationship.Variable] {
			c.declaredRels[elem.Relationship.Variable] = true
			addedRels = append(addedRels, elem.Relationship.Variable)
		}
		if elem.Target.Variable != "" && !c.declaredNodes[elem.Target.Variable] {
			c.declaredNodes[elem.Target.Variable] = true
			addedNodes = append(addedNodes, elem.Target.Variable)
		}
	}

	var projSQL string
	if branch.Match.Where != nil {
		whereSQL, err := c.visitExpression(branch.Match.Where)
		if err == nil && whereSQL != "" {
			conditions = append(conditions, whereSQL)
		}
	}

	pSQL, err := c.visitExpression(funcExpr.Args[0])
	if err == nil {
		projSQL = pSQL
	}

	for _, n := range addedNodes {
		delete(c.declaredNodes, n)
	}
	for _, r := range addedRels {
		delete(c.declaredRels, r)
	}

	whereSQL := ""
	if len(conditions) > 0 {
		whereSQL = strings.Join(conditions, " AND ")
	}
	branch.IsCompiled = true

	lower := strings.ToLower(funcExpr.Name)
	if lower == "collect" {
		return c.schema.Dialect.RenderCollectSubquery(&SubqueryModel{
			Distinct:   funcExpr.IsDistinct,
			Projection: projSQL,
			FromJoins:  fromJoins,
			Where:      whereSQL,
		})
	}

	if lower == "count" {
		countTarget := "1"
		if _, ok := funcExpr.Args[0].(WildcardExpr); ok {
			countTarget = "*"
		}
		return RenderCountSubquery(&SubqueryModel{
			Distinct:  funcExpr.IsDistinct,
			Target:    countTarget,
			FromJoins: fromJoins,
			Where:     whereSQL,
		})
	}

	return ""
}
