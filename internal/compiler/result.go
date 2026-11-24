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

	// ParamOrder lists parameter names in positional order ($1, $2, ...) for dialects like PostgreSQL.
	ParamOrder []string
}

// NamedArgs converts query parameters to a slice of sql.NamedArg
// suitable for passing directly to db.QueryContext or db.ExecContext (SQLite).
func (q *CompiledQuery) NamedArgs() []any {
	var args []any
	for k, v := range q.Params {
		name := strings.TrimPrefix(k, "@")
		args = append(args, sql.Named(name, v))
	}
	return args
}

// PositionalArgs returns parameter values in $1..$N order for PostgreSQL (pgx / database/sql).
func (q *CompiledQuery) PositionalArgs() []any {
	args := make([]any, 0, len(q.ParamOrder))
	for _, name := range q.ParamOrder {
		if val, ok := q.Params[name]; ok {
			args = append(args, val)
		} else if val, ok := q.Params["@"+name]; ok {
			args = append(args, val)
		} else {
			args = append(args, nil)
		}
	}
	return args
}
