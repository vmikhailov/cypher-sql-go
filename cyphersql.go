// Package cyphersql provides an embedded OpenCypher to SQLite SQL compiler.
// It transpiles graph pattern matching, path traversals, quantifiers, and aggregations
// into fast, deterministic SQLite queries using relational indexes and JSON functions.
package cyphersql

import (
	"database/sql"
	"strings"
)

// Version of the cyphersql library.
const Version = "1.0.0"

// SchemaConfig defines physical table and column mappings for Cypher to SQL compilation.
type SchemaConfig struct {
	// NodesTable is the table name for vertices (default: "nodes").
	NodesTable string
	// EdgesTable is the table name for relationships (default: "edges").
	EdgesTable string
	// NodeIDCol is the primary key column for nodes (default: "id").
	NodeIDCol string
	// NodeKindCol is the label/type column for nodes (default: "kind").
	NodeKindCol string
	// NodePropsCol is the JSON properties column for nodes (default: "properties").
	NodePropsCol string
	// EdgeFromCol is the source vertex ID column for edges (default: "from_id").
	EdgeFromCol string
	// EdgeToCol is the target vertex ID column for edges (default: "to_id").
	EdgeToCol string
	// EdgeKindCol is the relationship type column for edges (default: "kind").
	EdgeKindCol string
	// EdgePropsCol is the JSON properties column for edges (default: "properties").
	EdgePropsCol string
	// LabelResolver provides an optional hook to override SQL predicates for node labels.
	// Return (predicateSql, true) to use custom SQL, or (_, false) for standard kind equality.
	LabelResolver func(nodeVar, label string) (string, bool)
}

// DefaultSchemaConfig returns the standard Universal Property Graph schema configuration.
func DefaultSchemaConfig() SchemaConfig {
	return SchemaConfig{
		NodesTable:   "nodes",
		EdgesTable:   "edges",
		NodeIDCol:    "id",
		NodeKindCol:  "kind",
		NodePropsCol: "properties",
		EdgeFromCol:  "from_id",
		EdgeToCol:    "to_id",
		EdgeKindCol:  "kind",
		EdgePropsCol: "properties",
	}
}

// CompiledQuery contains the generated SQL statement and associated parameters.
type CompiledQuery struct {
	// SQL is the generated SQLite-compatible SQL query.
	SQL string

	// Params contains any named parameters defined in the query or passed by the caller.
	Params map[string]any
}

// NamedArgs converts query parameters to a slice of sql.NamedArg
// suitable for passing directly to db.QueryContext or db.ExecContext.
func (q *CompiledQuery) NamedArgs() []any {
	var args []any
	for k, v := range q.Params {
		name := strings.TrimPrefix(k, "@")
		args = append(args, sql.Named(name, v))
	}
	return args
}

// Compile compiles a Cypher query string into SQLite SQL using the default schema.
func Compile(cypher string) (*CompiledQuery, error) {
	return CompileWithOptions(cypher, nil, DefaultSchemaConfig())
}

// CompileWithParams compiles a Cypher query string with initial parameters into SQLite SQL.
func CompileWithParams(cypher string, params map[string]any) (*CompiledQuery, error) {
	return CompileWithOptions(cypher, params, DefaultSchemaConfig())
}

// CompileWithSchema compiles a Cypher query string using a custom SchemaConfig.
func CompileWithSchema(cypher string, cfg SchemaConfig) (*CompiledQuery, error) {
	return CompileWithOptions(cypher, nil, cfg)
}

// CompileWithOptions compiles a Cypher query string with custom parameters and SchemaConfig.
func CompileWithOptions(cypher string, params map[string]any, cfg SchemaConfig) (*CompiledQuery, error) {
	parser := NewParser(cypher)
	query, err := parser.Parse()
	if err != nil {
		return nil, err
	}

	compiler := NewCompilerWithOptions(query, params, cfg)
	return compiler.Compile()
}
