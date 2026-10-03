package test

import (
	"strings"
	"testing"

	"github.com/vmikhailov/cypher-sql-go"
)

func TestSQLInjection_IdentifiersRejected(t *testing.T) {
	injectionQueries := []string{
		"MATCH (p:`Person'; DELETE FROM nodes; --`) RETURN count(p)",
		"MATCH (p:`Person' UNION SELECT name FROM sqlite_master --`) RETURN p.name",
		"MATCH (a)-[:`CALLS'; DROP TABLE nodes; --`]->(b) RETURN a",
		"MATCH (p) WHERE p.`name' OR 1=1 --` = 'Alice' RETURN p",
	}

	for _, q := range injectionQueries {
		compiled, err := cyphersql.Compile(q)
		if err == nil {
			t.Fatalf("expected query to fail compilation due to unsafe identifier, but got SQL: %s", compiled.SQL)
		}
		if !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "unexpected") {
			t.Logf("Query %q rejected with error: %v", q, err)
		}
	}
}

func TestVariableLengthRelationships_ExplicitError(t *testing.T) {
	varQueries := []string{
		"MATCH (a:Person)-[*1..3]->(b:Person) RETURN a, b",
		"MATCH (a:Person)-[:CALLS*2..5]->(b:Person) RETURN a, b",
		"MATCH (a:Person)-[*]->(b:Person) RETURN a, b",
	}

	for _, q := range varQueries {
		_, err := cyphersql.Compile(q)
		if err == nil {
			t.Fatalf("expected variable-length path query %q to return error, but got nil", q)
		}
		if !strings.Contains(err.Error(), "variable-length") {
			t.Fatalf("expected error mentioning 'variable-length', got: %v", err)
		}
	}
}
