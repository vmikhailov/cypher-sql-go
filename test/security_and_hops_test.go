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

func TestVariableLengthRelationships_UnsupportedCases(t *testing.T) {
	unsupportedQueries := []string{
		"MATCH (a:Person)-[*1..3]-(b:Person) RETURN a, b",   // undirected
		"MATCH (a:Person)-[r*1..3]->(b:Person) RETURN a, b", // bound variable
		"MATCH (a:Person)-[*0..3]->(b:Person) RETURN a, b",  // 0 hops
		"MATCH (a:Person)-[*3..1]->(b:Person) RETURN a, b",  // max < min
	}

	for _, q := range unsupportedQueries {
		_, err := cyphersql.Compile(q)
		if err == nil {
			t.Fatalf("expected query %q to return error, but got nil", q)
		}
	}

	// ClickHouse dialect unsupported
	_, err := cyphersql.CompileWithSchema("MATCH (a:Person)-[*1..3]->(b:Person) RETURN a, b", cyphersql.ClickHouseSchemaConfig())
	if err == nil {
		t.Fatalf("expected ClickHouse variable-length query to return error, but got nil")
	}
	if !strings.Contains(err.Error(), "ClickHouse") {
		t.Fatalf("expected error mentioning ClickHouse, got: %v", err)
	}
}
