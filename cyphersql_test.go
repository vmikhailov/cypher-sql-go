package cyphersql_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vmikhailov/cypher-sql-go"
	_ "modernc.org/sqlite"
)

func TestCompile_SimpleMatch(t *testing.T) {
	cypher := `MATCH (n:Type) WHERE n.name = $name RETURN n.name AS name, n.symbol AS fullName`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "FROM nodes n") {
		t.Errorf("expected FROM nodes n, got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "json_extract(n.properties, '$.name')") {
		t.Errorf("expected json_extract for property access, got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "@name") {
		t.Errorf("expected @name parameter, got:\n%s", compiled.SQL)
	}
}

func TestCompile_RelationshipTraversal(t *testing.T) {
	cypher := `
		MATCH (a:Function)-[r:CALLS]->(b:Function)
		RETURN a.name AS caller, type(r) AS relType, b.name AS callee
	`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "JOIN edges r ON r.from_id = a.id AND r.kind = 'CALLS'") {
		t.Errorf("expected edge join condition, got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "r.kind") {
		t.Errorf("expected r.kind for type(r), got:\n%s", compiled.SQL)
	}
}

func TestCompile_RelationshipFunctions_And_DirectProps(t *testing.T) {
	cypher := `
		MATCH (a:Function)-[r:ASYNC_CALL]->(b:Function)
		RETURN r.via AS via, r.call_chain AS chain, startNode(r) AS src, endNode(r) AS dst, properties(r) AS props
	`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "json_extract(r.properties, '$.via')") {
		t.Errorf("expected r.via extraction, got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "r.from_id") {
		t.Errorf("expected r.from_id for startNode(r), got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "r.to_id") {
		t.Errorf("expected r.to_id for endNode(r), got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "json(r.properties)") {
		t.Errorf("expected json(r.properties) for properties(r), got:\n%s", compiled.SQL)
	}
}

func TestCompile_CartesianProductDecomposition(t *testing.T) {
	cypher := `
		MATCH (p:Project) WHERE p.name = 'MyApi'
		OPTIONAL MATCH (p)-[:CONTAINS]->(ep:Endpoint)
		OPTIONAL MATCH (p)-[:USES_DB]->(db:Database)
		RETURN p.name, collect(ep.name) AS eps, collect(db.name) AS dbs
	`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// In decomposed mode, outer query must NOT LEFT JOIN ep or db directly
	if strings.Contains(compiled.SQL, "LEFT JOIN nodes ep") {
		t.Errorf("expected ep branch to be decomposed, but found LEFT JOIN nodes ep:\n%s", compiled.SQL)
	}
	if strings.Contains(compiled.SQL, "LEFT JOIN nodes db") {
		t.Errorf("expected db branch to be decomposed, but found LEFT JOIN nodes db:\n%s", compiled.SQL)
	}
	// Instead, it must contain correlated subqueries with json_group_array
	if !strings.Contains(compiled.SQL, "(SELECT json_group_array(") {
		t.Errorf("expected correlated json_group_array subquery, got:\n%s", compiled.SQL)
	}
}

func TestCompile_PathPredicates_And_Quantifiers(t *testing.T) {
	cypher := `
		MATCH (a:Function)
		WHERE EXISTS((a)-[:CALLS]->(:Function)) AND any(t IN a.tags WHERE t = 'security')
		RETURN a.name AS name
	`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "EXISTS (SELECT 1 FROM") {
		t.Errorf("expected EXISTS subquery for path predicate, got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "json_each(CASE WHEN json_valid(") {
		t.Errorf("expected json_valid guarded json_each for quantifier, got:\n%s", compiled.SQL)
	}
}

func TestCompile_CaseExpression_And_Strings(t *testing.T) {
	cypher := `
		MATCH (s:Service)
		RETURN s.name AS name,
		       CASE WHEN s.tier = 1 THEN 'critical' WHEN s.tier = 2 THEN 'standard' ELSE 'low' END AS priority,
		       toLower(s.name) AS lowerName,
		       coalesce(s.description, 'none') AS desc
		ORDER BY s.name ASC
		LIMIT 10 OFFSET 5
	`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "CASE WHEN") {
		t.Errorf("expected CASE WHEN in SQL, got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "lower(") {
		t.Errorf("expected lower() in SQL, got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "COALESCE(") {
		t.Errorf("expected COALESCE() in SQL, got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "LIMIT 10") {
		t.Errorf("expected LIMIT 10, got:\n%s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "OFFSET 5") {
		t.Errorf("expected OFFSET 5, got:\n%s", compiled.SQL)
	}
}

func TestExecution_InMemorySQLite(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer db.Close()

	// 1. Create Schema
	schema := `
		CREATE TABLE nodes (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			properties TEXT NOT NULL
		);
		CREATE TABLE edges (
			from_id TEXT NOT NULL,
			to_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			properties TEXT NOT NULL
		);
		CREATE INDEX idx_edges_from ON edges(from_id);
		CREATE INDEX idx_edges_to ON edges(to_id);
		CREATE INDEX idx_edges_kind ON edges(kind);
		CREATE INDEX idx_nodes_kind ON nodes(kind);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	// 2. Insert Data
	insertNode := func(id, kind string, props map[string]any) {
		b, _ := json.Marshal(props)
		_, err := db.Exec("INSERT INTO nodes (id, kind, properties) VALUES (?, ?, ?)", id, kind, string(b))
		if err != nil {
			t.Fatalf("insert node %s: %v", id, err)
		}
	}
	insertEdge := func(from, to, kind string, props map[string]any) {
		b, _ := json.Marshal(props)
		_, err := db.Exec(
			"INSERT INTO edges (from_id, to_id, kind, properties) VALUES (?, ?, ?, ?)",
			from, to, kind, string(b))
		if err != nil {
			t.Fatalf("insert edge: %v", err)
		}
	}

	insertNode("proj:1", "Project", map[string]any{
		"name": "OrdersService", "project_type": "go", "tags": []string{"production", "core"},
	})
	insertNode("ep:1", "Endpoint", map[string]any{"name": "POST /orders"})
	insertNode("ep:2", "Endpoint", map[string]any{"name": "GET /orders/:id"})
	insertNode("db:1", "Database", map[string]any{"name": "orders_pg"})
	insertNode("db:2", "Database", map[string]any{"name": "redis_cache"})
	insertNode("fn:1", "Function", map[string]any{"name": "HandleOrder"})
	insertNode("fn:2", "Function", map[string]any{"name": "SaveToDB"})

	insertEdge("proj:1", "ep:1", "CONTAINS", map[string]any{"via": "http"})
	insertEdge("proj:1", "ep:2", "CONTAINS", map[string]any{"via": "http"})
	insertEdge("proj:1", "db:1", "USES_DB", map[string]any{})
	insertEdge("proj:1", "db:2", "USES_DB", map[string]any{})
	insertEdge("fn:1", "fn:2", "CALLS", map[string]any{"call_chain": "HandleOrder->SaveToDB", "via": "direct"})

	// Test 1: Multi-Branch OPTIONAL MATCH Decomposition
	t.Run("MultiBranchDecomposition", func(t *testing.T) {
		cypherQuery := `
			MATCH (p:Project) WHERE p.name = 'OrdersService'
			OPTIONAL MATCH (p)-[:CONTAINS]->(ep:Endpoint)
			OPTIONAL MATCH (p)-[:USES_DB]->(db:Database)
			RETURN p.name AS project, collect(ep.name) AS eps, collect(db.name) AS dbs, count(ep) AS epCount
		`
		compiled, err := cyphersql.Compile(cypherQuery)
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		rows, err := db.Query(compiled.SQL)
		if err != nil {
			t.Fatalf("sql execute error: %v\nSQL:\n%s", err, compiled.SQL)
		}
		defer rows.Close()

		if !rows.Next() {
			t.Fatalf("expected 1 row returned")
		}

		var project, epsJSON, dbsJSON string
		var epCount int64
		if err := rows.Scan(&project, &epsJSON, &dbsJSON, &epCount); err != nil {
			t.Fatalf("scan error: %v", err)
		}

		if project != "OrdersService" {
			t.Errorf("expected project OrdersService, got %s", project)
		}
		if epCount != 2 {
			t.Errorf("expected epCount 2, got %d", epCount)
		}

		var eps []string
		_ = json.Unmarshal([]byte(epsJSON), &eps)
		if len(eps) != 2 || eps[0] != "POST /orders" || eps[1] != "GET /orders/:id" {
			t.Errorf("expected 2 unique endpoints without cartesian duplicates, got %v", eps)
		}

		var dbs []string
		_ = json.Unmarshal([]byte(dbsJSON), &dbs)
		if len(dbs) != 2 || dbs[0] != "orders_pg" || dbs[1] != "redis_cache" {
			t.Errorf("expected 2 unique databases without cartesian duplicates, got %v", dbs)
		}
	})

	// Test 2: Relationship functions, attributes, and path predicates
	t.Run("RelationshipFunctionsAndPredicates", func(t *testing.T) {
		cypherQuery := `
			MATCH (a:Function)-[r:CALLS]->(b:Function)
			WHERE EXISTS((a)-[:CALLS]->(b))
			RETURN a.name AS caller, b.name AS callee, type(r) AS relType, r.via AS via, r.call_chain AS chain
		`
		compiled, err := cyphersql.Compile(cypherQuery)
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		rows, err := db.Query(compiled.SQL)
		if err != nil {
			t.Fatalf("sql execute error: %v\nSQL:\n%s", err, compiled.SQL)
		}
		defer rows.Close()

		if !rows.Next() {
			t.Fatalf("expected 1 row returned")
		}

		var caller, callee, relType, via, chain string
		if err := rows.Scan(&caller, &callee, &relType, &via, &chain); err != nil {
			t.Fatalf("scan error: %v", err)
		}

		if caller != "HandleOrder" || callee != "SaveToDB" || relType != "CALLS" ||
			via != "direct" || chain != "HandleOrder->SaveToDB" {
			t.Errorf("unexpected row values: caller=%s, callee=%s, type=%s, via=%s, chain=%s",
				caller, callee, relType, via, chain)
		}
	})

	// Test 3: Quantifier Predicates (any, all, none, single)
	t.Run("QuantifierPredicates", func(t *testing.T) {
		cypherQuery := `
			MATCH (p:Project)
			WHERE any(t IN p.tags WHERE t = 'production') AND none(t IN p.tags WHERE t = 'deprecated')
			RETURN p.name AS name
		`
		compiled, err := cyphersql.Compile(cypherQuery)
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		rows, err := db.Query(compiled.SQL)
		if err != nil {
			t.Fatalf("sql execute error: %v\nSQL:\n%s", err, compiled.SQL)
		}
		defer rows.Close()

		if !rows.Next() {
			t.Fatalf("expected 1 row returned")
		}
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		if name != "OrdersService" {
			t.Errorf("expected OrdersService, got %s", name)
		}
	})
}

func BenchmarkCompile(b *testing.B) {
	cypher := `
		MATCH (p:Project) WHERE p.name = 'OrdersService'
		OPTIONAL MATCH (p)-[:CONTAINS]->(ep:Endpoint)
		OPTIONAL MATCH (p)-[:USES_DB]->(db:Database)
		RETURN p.name AS project, collect(ep.name) AS eps, collect(db.name) AS dbs, count(ep) AS epCount
	`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cyphersql.Compile(cypher)
		if err != nil {
			b.Fatal(err)
		}
	}
}
