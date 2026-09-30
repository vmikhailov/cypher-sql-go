package test

import (
	"strings"
	"testing"

	"github.com/vmikhailov/cypher-sql-go"
)

func TestClickHouseDialect_PropertiesAndExtraction(t *testing.T) {
	cypher := `
		MATCH (s:Service)
		WHERE s.team = 'payments' AND s.tier = 1
		RETURN s.name AS serviceName, s.tier AS tier
	`
	compiled, err := cyphersql.CompileWithSchema(cypher, cyphersql.ClickHouseSchemaConfig())
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "JSONExtractString(s.properties, 'name') AS \"serviceName\"") {
		t.Errorf("expected ClickHouse JSONExtractString in projection, got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "JSONExtractString(s.properties, 'team') = 'payments'") {
		t.Errorf("expected ClickHouse JSONExtractString in WHERE, got:\n%s", compiled.SQL)
	}
}

func TestClickHouseDialect_CollectAndAggregations(t *testing.T) {
	cypher := `
		MATCH (s:Service)
		RETURN s.team AS team, collect(s.name) AS services, count(s) AS total
		GROUP BY team
	`
	compiled, err := cyphersql.CompileWithSchema(cypher, cyphersql.ClickHouseSchemaConfig())
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "groupArrayIf(JSONExtractString(s.properties, 'name')") {
		t.Errorf("expected groupArrayIf for collect in ClickHouse, got:\n%s", compiled.SQL)
	}
}

func TestClickHouseDialect_DecomposedOptionalMatch(t *testing.T) {
	cypher := `
		MATCH (s:Service) WHERE s.name = 'orders-service'
		OPTIONAL MATCH (s)-[:CALLS]->(t:Service)
		OPTIONAL MATCH (s)-[:USES_DB]->(d:Database)
		RETURN s.name, collect(t.name) AS calls, collect(d.name) AS dbs
	`
	compiled, err := cyphersql.CompileWithSchema(cypher, cyphersql.ClickHouseSchemaConfig())
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "SELECT groupArrayIf(JSONExtractString(t.properties, 'name')") {
		t.Errorf("expected groupArrayIf subquery for ClickHouse, got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "SELECT groupArrayIf(JSONExtractString(d.properties, 'name')") {
		t.Errorf("expected groupArrayIf subquery for ClickHouse, got:\n%s", compiled.SQL)
	}
}

func TestClickHouseDialect_HigherOrderQuantifiers(t *testing.T) {
	cypher := `
		MATCH (s:Service)
		WHERE any(t IN s.tags WHERE t = 'production')
		RETURN s.name
	`
	compiled, err := cyphersql.CompileWithSchema(cypher, cyphersql.ClickHouseSchemaConfig())
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "arrayJoin(JSONExtract(JSONExtractString(s.properties, 'tags')") {
		t.Errorf("expected ClickHouse arrayJoin quantifier, got:\n%s", compiled.SQL)
	}
}
