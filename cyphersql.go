// Package cyphersql provides an embedded read-only OpenCypher to SQL compiler.
// It transpiles graph pattern matching, path traversals, quantifiers, and aggregations
// into fast, deterministic SQLite and ClickHouse queries using relational indexes and JSON functions.
package cyphersql

import (
	"github.com/vmikhailov/cypher-sql-go/internal/compiler"
	"github.com/vmikhailov/cypher-sql-go/internal/parser"
)

// Version of the cyphersql library.
const Version = "1.0.0"

// Re-export core types for public API backwards compatibility and clean usage.
type (
	// Dialect abstracts database-specific SQL dialect rendering.
	Dialect = compiler.Dialect

	// SchemaConfig defines physical table and column mappings for Cypher to SQL compilation.
	SchemaConfig = compiler.SchemaConfig

	// CompiledQuery contains the generated SQL statement and associated parameters.
	CompiledQuery = compiler.CompiledQuery

	// SubqueryModel represents an isolated traversal subquery for decomposition.
	SubqueryModel = compiler.SubqueryModel

	// QuantifierModel represents an array quantifier (any/all/none/single).
	QuantifierModel = compiler.QuantifierModel

	// ListCompModel represents a list comprehension expression.
	ListCompModel = compiler.ListCompModel
)

// SQLiteDialect returns the standard SQLite dialect implementation.
func SQLiteDialect() Dialect {
	return compiler.SQLiteDialect()
}

// ClickHouseDialect returns the ClickHouse dialect implementation.
func ClickHouseDialect() Dialect {
	return compiler.ClickHouseDialect()
}

// DefaultSchemaConfig returns the standard Universal Property Graph schema configuration for SQLite.
func DefaultSchemaConfig() SchemaConfig {
	return compiler.DefaultSchemaConfig()
}

// ClickHouseSchemaConfig returns a standard SchemaConfig tuned for ClickHouse.
func ClickHouseSchemaConfig() SchemaConfig {
	return compiler.ClickHouseSchemaConfig()
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
	p := parser.NewParser(cypher)
	query, err := p.Parse()
	if err != nil {
		return nil, err
	}

	c := compiler.NewCompilerWithOptions(query, params, cfg)
	return c.Compile()
}
