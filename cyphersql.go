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

// Compile compiles a Cypher query string into SQLite SQL.
func Compile(cypher string) (*CompiledQuery, error) {
	return CompileWithParams(cypher, nil)
}

// CompileWithParams compiles a Cypher query string with initial parameters into SQLite SQL.
func CompileWithParams(cypher string, params map[string]any) (*CompiledQuery, error) {
	parser := NewParser(cypher)
	query, err := parser.Parse()
	if err != nil {
		return nil, err
	}

	compiler := NewCompiler(query, params)
	return compiler.Compile()
}
