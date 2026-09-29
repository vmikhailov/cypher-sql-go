package cyphersql

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

var funcMap = template.FuncMap{
	"join": strings.Join,
	"trim": strings.TrimSpace,
}

// QueryModel holds the structured data for rendering a complete SQL query.
type QueryModel struct {
	CTEs       []string
	IsDistinct bool
	Columns    []string
	FromTable  string
	FromAlias  string
	Joins      []JoinModel
	Where      string
	GroupBy    []string
	Having     string
	OrderBy    []string
	Limit      string
	Offset     string
}

// JoinModel holds data for a single SQL JOIN clause.
type JoinModel struct {
	Type  string // "JOIN" or "LEFT JOIN"
	Table string // "nodes" or "edges"
	Alias string
	On    string
}

// SubqueryModel holds parameters for rendering correlated subqueries.
type SubqueryModel struct {
	Distinct   bool
	Projection string
	Target     string
	FromJoins  string
	Where      string
}

// QuantifierModel holds parameters for rendering any(), all(), none(), single().
type QuantifierModel struct {
	Quantifier string // "any", "all", "none", "single"
	List       string
	Var        string
	Predicate  string
}

// ListCompModel holds parameters for rendering list comprehensions.
type ListCompModel struct {
	List       string
	Var        string
	Filter     string
	Projection string
}

var (
	queryTemplate = template.Must(template.New("query").Funcs(funcMap).Parse(
		`{{- if .CTEs }}WITH RECURSIVE
{{ join .CTEs ",\n" }}
{{ end -}}
SELECT {{ if .IsDistinct }}DISTINCT {{ end }}{{ join .Columns ", " }}
FROM {{ .FromTable }} {{ .FromAlias }}
{{- range .Joins }}
{{ .Type }} {{ .Table }} {{ .Alias }} ON {{ .On }}
{{- end }}
{{- if .Where }}
WHERE {{ .Where }}
{{- end }}
{{- if .GroupBy }}
GROUP BY {{ join .GroupBy ", " }}
{{- end }}
{{- if .Having }}
HAVING {{ .Having }}
{{- end }}
{{- if .OrderBy }}
ORDER BY {{ join .OrderBy ", " }}
{{- end }}
{{- if .Limit }}
LIMIT {{ .Limit }}
{{- end }}
{{- if .Offset }}
OFFSET {{ .Offset }}
{{- end }}`,
	))

	collectSubqueryTemplate = template.Must(template.New("collectSubquery").Funcs(funcMap).Parse(
		`(SELECT json_group_array({{ if .Distinct }}DISTINCT {{ end }}{{ .Projection }}) FILTER (WHERE {{ .Projection }} IS NOT NULL) FROM {{ .FromJoins }}{{ if .Where }} WHERE {{ .Where }}{{ end }})`,
	))

	countSubqueryTemplate = template.Must(template.New("countSubquery").Funcs(funcMap).Parse(
		`(SELECT COUNT({{ if .Distinct }}DISTINCT {{ end }}{{ .Target }}) FROM {{ .FromJoins }}{{ if .Where }} WHERE {{ .Where }}{{ end }})`,
	))

	existsSubqueryTemplate = template.Must(template.New("existsSubquery").Funcs(funcMap).Parse(
		`(EXISTS (SELECT 1 FROM {{ .FromJoins }}{{ if .Where }} WHERE {{ .Where }}{{ end }}))`,
	))

	quantifierTemplate = template.Must(template.New("quantifier").Funcs(funcMap).Parse(
		`{{ if eq .Quantifier "any" }}(EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid({{ .List }}) THEN {{ .List }} ELSE '[]' END) AS {{ .Var }} WHERE {{ .Predicate }}))
{{- else if eq .Quantifier "none" }}(NOT EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid({{ .List }}) THEN {{ .List }} ELSE '[]' END) AS {{ .Var }} WHERE {{ .Predicate }}))
{{- else if eq .Quantifier "all" }}(NOT EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid({{ .List }}) THEN {{ .List }} ELSE '[]' END) AS {{ .Var }} WHERE NOT ({{ .Predicate }})))
{{- else if eq .Quantifier "single" }}((SELECT COUNT(1) FROM json_each(CASE WHEN json_valid({{ .List }}) THEN {{ .List }} ELSE '[]' END) AS {{ .Var }} WHERE {{ .Predicate }}) = 1)
{{- else }}(EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid({{ .List }}) THEN {{ .List }} ELSE '[]' END) AS {{ .Var }} WHERE {{ .Predicate }})){{ end }}`,
	))

	listCompTemplate = template.Must(template.New("listComp").Funcs(funcMap).Parse(
		`(SELECT json_group_array({{ .Projection }}) FROM json_each(CASE WHEN json_valid({{ .List }}) THEN {{ .List }} ELSE '[]' END) AS {{ .Var }}{{ if .Filter }} WHERE {{ .Filter }}{{ end }})`,
	))
)

func renderTemplate(tmpl *template.Template, data any) string {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Sprintf("/* template render error: %v */", err)
	}
	return strings.TrimSpace(buf.String())
}

// RenderQuery renders a full SQL query from QueryModel using queryTemplate.
func RenderQuery(m *QueryModel) string {
	return renderTemplate(queryTemplate, m)
}

// RenderCollectSubquery renders a correlated collect() subquery.
func RenderCollectSubquery(m *SubqueryModel) string {
	return renderTemplate(collectSubqueryTemplate, m)
}

// RenderCountSubquery renders a correlated count() subquery.
func RenderCountSubquery(m *SubqueryModel) string {
	return renderTemplate(countSubqueryTemplate, m)
}

// RenderExistsSubquery renders a correlated EXISTS(...) subquery.
func RenderExistsSubquery(m *SubqueryModel) string {
	return renderTemplate(existsSubqueryTemplate, m)
}

// RenderQuantifier renders any/all/none/single quantifier expression.
func RenderQuantifier(m *QuantifierModel) string {
	return renderTemplate(quantifierTemplate, m)
}

// RenderListComprehension renders list comprehension expression.
func RenderListComprehension(m *ListCompModel) string {
	return renderTemplate(listCompTemplate, m)
}
