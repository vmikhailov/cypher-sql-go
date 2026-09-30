package compiler

import (
	"database/sql"
	"strings"
)

// CompiledQuery contains the generated SQL statement and associated parameters.
type CompiledQuery struct {
	// SQL is the generated SQL query.
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
