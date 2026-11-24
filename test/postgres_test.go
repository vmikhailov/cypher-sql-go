package test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vmikhailov/cypher-sql-go"
)

func TestPostgresDialect_PropertiesAndNumericExtraction(t *testing.T) {
	cypher := `
		MATCH (p:Policy)-[:HAS_SECTION]->(s:Section)<-[:DEFINED_IN]-(r:Rule)
		WHERE p.status = 'active' AND r.numeric_value > 100
		RETURN p.title AS policyTitle, r.numeric_value AS amount
	`
	compiled, err := cyphersql.CompileWithSchema(cypher, cyphersql.PostgresSchemaConfig())
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "((p.properties)::jsonb->>'status') = 'active'") {
		t.Errorf("expected Postgres JSONB ->> extraction in WHERE, got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "((p.properties)::jsonb->>'title') AS \"policyTitle\"") {
		t.Errorf("expected Postgres JSONB ->> projection, got:\n%s", compiled.SQL)
	}
	if strings.Contains(compiled.SQL, "json_extract") || strings.Contains(compiled.SQL, "rowid") {
		t.Errorf("unexpected SQLite syntax leaked into Postgres SQL:\n%s", compiled.SQL)
	}
}

func TestPostgresDialect_PositionalParamsAndDeduplication(t *testing.T) {
	cypher := `
		MATCH (p:Policy)-[:APPLIES_IN]->(j:Jurisdiction)
		WHERE p.status = $status AND (j.code = $country OR p.owner = $status)
		RETURN p.id, p.title
	`
	params := map[string]any{"status": "active", "country": "PL"}
	compiled, err := cyphersql.CompileWithOptions(cypher, params, cyphersql.PostgresSchemaConfig())
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "$1") || !strings.Contains(compiled.SQL, "$2") {
		t.Errorf("expected positional $1/$2 placeholders, got:\n%s", compiled.SQL)
	}
	if strings.Contains(compiled.SQL, "$3") {
		t.Errorf("expected repeated $status to reuse $1, got:\n%s", compiled.SQL)
	}
	expectedOrder := []string{"status", "country"}
	if !reflect.DeepEqual(compiled.ParamOrder, expectedOrder) {
		t.Errorf("expected ParamOrder %v, got %v", expectedOrder, compiled.ParamOrder)
	}
	expectedArgs := []any{"active", "PL"}
	if !reflect.DeepEqual(compiled.PositionalArgs(), expectedArgs) {
		t.Errorf("expected PositionalArgs %v, got %v", expectedArgs, compiled.PositionalArgs())
	}
}

func TestPostgresDialect_CollectAndOptionalMatchDecomposition(t *testing.T) {
	cypher := `
		MATCH (p:Policy) WHERE p.id = 'policy:travel-pl'
		OPTIONAL MATCH (p)-[:HAS_SECTION]->(s:Section)
		OPTIONAL MATCH (p)-[:APPLIES_IN]->(j:Jurisdiction)
		RETURN p.title, collect(DISTINCT s.title) AS sections, collect(j.name) AS jurisdictions
	`
	compiled, err := cyphersql.CompileWithSchema(cypher, cyphersql.PostgresSchemaConfig())
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "jsonb_agg(DISTINCT") {
		t.Errorf("expected jsonb_agg(DISTINCT ...) in Postgres SQL, got:\n%s", compiled.SQL)
	}
	if strings.Contains(compiled.SQL, "json_group_array") {
		t.Errorf("unexpected SQLite json_group_array in Postgres SQL:\n%s", compiled.SQL)
	}
}

func TestPostgresDialect_VarLenPathTraversal(t *testing.T) {
	cypher := `
		MATCH (a:Jurisdiction {id: 'jur:krakow'})-[:PART_OF*1..3]->(b:Jurisdiction)
		RETURN b.id, b.name
	`
	compiled, err := cyphersql.CompileWithSchema(cypher, cyphersql.PostgresSchemaConfig())
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "chr(31)") || !strings.Contains(compiled.SQL, "strpos(v.trail,") {
		t.Errorf("expected chr() and strpos() in Postgres varlen CTE, got:\n%s", compiled.SQL)
	}
	if strings.Contains(compiled.SQL, "instr(") || strings.Contains(compiled.SQL, "char(31)") {
		t.Errorf("unexpected SQLite instr/char in Postgres varlen CTE:\n%s", compiled.SQL)
	}
}

func TestPostgresDialect_ListsUnwindKeysAndQuantifiers(t *testing.T) {
	queries := []string{
		`MATCH (n:Role) RETURN keys(n) AS k`,
		`MATCH (n:Policy) UNWIND ['a', 'b'] AS tag RETURN n.id, tag`,
		`MATCH (n:Role) WHERE any(x IN n.aliases WHERE x = 'HR') RETURN n.id`,
		`MATCH (n:Role) RETURN head(n.aliases) AS first_alias, last(n.aliases) AS last_alias`,
		`MATCH (n:Role) RETURN n.aliases[0..2] AS slice`,
		`MATCH (p:Policy)-[r:OWNED_BY]->(role:Role) RETURN p.id, count(r) AS cnt`,
	}
	for _, q := range queries {
		compiled, err := cyphersql.CompileWithSchema(q, cyphersql.PostgresSchemaConfig())
		if err != nil {
			t.Fatalf("failed to compile %q: %v", q, err)
		}
		for _, bad := range []string{"json_extract", "json_each", "json_group_array", "rowid", "json_array("} {
			if strings.Contains(compiled.SQL, bad) {
				t.Errorf("query %q leaked SQLite construct %q in Postgres SQL:\n%s", q, bad, compiled.SQL)
			}
		}
	}
}
