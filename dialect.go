package cyphersql

import (
	"strings"
)

// Dialect abstracts SQL differences between database engines (SQLite, ClickHouse, etc.).
type Dialect interface {
	// Name returns the dialect identifier ("sqlite", "clickhouse").
	Name() string

	// JSONExtract returns an expression extracting a property from a JSON column.
	JSONExtract(column, property string) string

	// ArrayAgg returns an aggregation expression collecting elements into an array.
	ArrayAgg(expr string, filter string, distinct bool) string

	// ArrayLiteral returns a literal array expression.
	ArrayLiteral(items ...string) string

	// JSONValid returns an expression checking if a string contains valid JSON.
	JSONValid(expr string) string

	// ArrayLength returns an expression measuring array length or string length.
	ArrayLength(expr string) string

	// ParamPlaceholder returns a parameter placeholder for query parameters.
	ParamPlaceholder(name string, index int) string

	// RenderCollectSubquery renders a correlated subquery collecting array items.
	RenderCollectSubquery(m *SubqueryModel) string

	// RenderQuantifier renders an any/all/none/single quantifier expression.
	RenderQuantifier(m *QuantifierModel) string

	// RenderListComprehension renders a list comprehension expression.
	RenderListComprehension(m *ListCompModel) string
}

// --- SQLite Dialect ---

type sqliteDialect struct{}

// SQLiteDialect returns the standard SQLite dialect.
func SQLiteDialect() Dialect {
	return &sqliteDialect{}
}

func (d *sqliteDialect) Name() string {
	return "sqlite"
}

func (d *sqliteDialect) JSONExtract(column, property string) string {
	return "json_extract(" + column + ", '$." + property + "')"
}

func (d *sqliteDialect) ArrayAgg(expr string, filter string, distinct bool) string {
	distinctStr := ""
	if distinct {
		distinctStr = "DISTINCT "
	}
	if filter != "" {
		return "json_group_array(" + distinctStr + expr + ") FILTER (WHERE " + filter + ")"
	}
	return "json_group_array(" + distinctStr + expr + ")"
}

func (d *sqliteDialect) ArrayLiteral(items ...string) string {
	return "json_array(" + strings.Join(items, ", ") + ")"
}

func (d *sqliteDialect) JSONValid(expr string) string {
	return "json_valid(" + expr + ")"
}

func (d *sqliteDialect) ArrayLength(expr string) string {
	return "CASE WHEN json_valid(" + expr + ") THEN json_array_length(" + expr + ") ELSE length(" + expr + ") END"
}

func (d *sqliteDialect) ParamPlaceholder(name string, index int) string {
	return "@" + name
}

func (d *sqliteDialect) RenderCollectSubquery(m *SubqueryModel) string {
	return renderTemplate(collectSubqueryTemplate, m)
}

func (d *sqliteDialect) RenderQuantifier(m *QuantifierModel) string {
	return renderTemplate(quantifierTemplate, m)
}

func (d *sqliteDialect) RenderListComprehension(m *ListCompModel) string {
	return renderTemplate(listCompTemplate, m)
}

// --- ClickHouse Dialect ---

type clickhouseDialect struct{}

// ClickHouseDialect returns a high-performance ClickHouse dialect.
func ClickHouseDialect() Dialect {
	return &clickhouseDialect{}
}

func (d *clickhouseDialect) Name() string {
	return "clickhouse"
}

func (d *clickhouseDialect) JSONExtract(column, property string) string {
	return "JSONExtractString(" + column + ", '" + property + "')"
}

func (d *clickhouseDialect) ArrayAgg(expr string, filter string, distinct bool) string {
	if distinct {
		if filter != "" {
			return "groupArrayDistinctIf(" + expr + ", " + filter + ")"
		}
		return "groupArrayDistinct(" + expr + ")"
	}
	if filter != "" {
		return "groupArrayIf(" + expr + ", " + filter + ")"
	}
	return "groupArray(" + expr + ")"
}

func (d *clickhouseDialect) ArrayLiteral(items ...string) string {
	return "[" + strings.Join(items, ", ") + "]"
}

func (d *clickhouseDialect) JSONValid(expr string) string {
	return "isValidJSON(" + expr + ")"
}

func (d *clickhouseDialect) ArrayLength(expr string) string {
	return "length(" + expr + ")"
}

func (d *clickhouseDialect) ParamPlaceholder(name string, index int) string {
	return "{" + name + ":String}"
}

func (d *clickhouseDialect) RenderCollectSubquery(m *SubqueryModel) string {
	distinctStr := ""
	if m.Distinct {
		distinctStr = "Distinct"
	}
	agg := "groupArray" + distinctStr + "If(" + m.Projection + ", " + m.Projection + " != '')"
	whereClause := ""
	if m.Where != "" {
		whereClause = " WHERE " + m.Where
	}
	return "(SELECT " + agg + " FROM " + m.FromJoins + whereClause + ")"
}

func (d *clickhouseDialect) RenderQuantifier(m *QuantifierModel) string {
	switch m.Quantifier {
	case "any":
		return "(EXISTS (SELECT 1 FROM (SELECT arrayJoin(JSONExtract(" + m.List +
			", 'Array(String)')) AS " + m.Var + ") WHERE " + m.Predicate + "))"
	case "none":
		return "(NOT EXISTS (SELECT 1 FROM (SELECT arrayJoin(JSONExtract(" + m.List +
			", 'Array(String)')) AS " + m.Var + ") WHERE " + m.Predicate + "))"
	case "all":
		return "(NOT EXISTS (SELECT 1 FROM (SELECT arrayJoin(JSONExtract(" + m.List +
			", 'Array(String)')) AS " + m.Var + ") WHERE NOT (" + m.Predicate + ")))"
	case "single":
		return "((SELECT count(1) FROM (SELECT arrayJoin(JSONExtract(" + m.List +
			", 'Array(String)')) AS " + m.Var + ") WHERE " + m.Predicate + ") = 1)"
	default:
		return "(EXISTS (SELECT 1 FROM (SELECT arrayJoin(JSONExtract(" + m.List +
			", 'Array(String)')) AS " + m.Var + ") WHERE " + m.Predicate + "))"
	}
}

func (d *clickhouseDialect) RenderListComprehension(m *ListCompModel) string {
	filterClause := ""
	if m.Filter != "" {
		filterClause = " WHERE " + m.Filter
	}
	return "(SELECT groupArray(" + m.Projection + ") FROM " +
		"(SELECT arrayJoin(JSONExtract(" + m.List + ", 'Array(String)')) AS " +
		m.Var + ")" + filterClause + ")"
}
