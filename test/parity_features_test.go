package test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vmikhailov/cypher-sql-go"
	_ "modernc.org/sqlite"
)

// 1. UNWIND: standalone and joined with node properties
func TestFeature_Unwind_Execution(t *testing.T) {
	db := setupMemoryDB(t)
	defer db.Close()

	// Insert test data
	_, err := db.Exec(`
		INSERT INTO nodes (id, kind, properties) VALUES 
		('p1', 'Person', '{"name": "Alice", "hobbies": ["reading", "hiking"], "scores": [10, 80, 95]}'),
		('p2', 'Person', '{"name": "Bob", "hobbies": ["gaming"], "scores": [30]}');
	`)
	if err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	t.Run("Standalone UNWIND literal list", func(t *testing.T) {
		cypher := `UNWIND [10, 20, 30] AS x RETURN x ORDER BY x DESC`
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			t.Fatalf("compilation error: %v", err)
		}

		rows, err := db.Query(compiled.SQL, compiled.NamedArgs()...)
		if err != nil {
			t.Fatalf("execution error on SQL:\n%s\nErr: %v", compiled.SQL, err)
		}
		defer rows.Close()

		var results []int
		for rows.Next() {
			var val int
			if err := rows.Scan(&val); err != nil {
				t.Fatalf("scan error: %v", err)
			}
			results = append(results, val)
		}

		if len(results) != 3 || results[0] != 30 || results[1] != 20 || results[2] != 10 {
			t.Errorf("unexpected results: %v", results)
		}
	})

	t.Run("MATCH with UNWIND node property array", func(t *testing.T) {
		cypher := `
			MATCH (p:Person)
			UNWIND p.hobbies AS hobby
			RETURN p.name AS name, hobby
			ORDER BY name, hobby
		`
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			t.Fatalf("compilation error: %v", err)
		}

		rows, err := db.Query(compiled.SQL, compiled.NamedArgs()...)
		if err != nil {
			t.Fatalf("execution error on SQL:\n%s\nErr: %v", compiled.SQL, err)
		}
		defer rows.Close()

		type rowData struct {
			Name  string
			Hobby string
		}
		var rowsCollected []rowData
		for rows.Next() {
			var r rowData
			if err := rows.Scan(&r.Name, &r.Hobby); err != nil {
				t.Fatalf("scan error: %v", err)
			}
			rowsCollected = append(rowsCollected, r)
		}

		// Alice has reading and hiking; Bob has gaming -> 3 rows
		if len(rowsCollected) != 3 {
			t.Fatalf("expected 3 rows, got %d: %v", len(rowsCollected), rowsCollected)
		}
		if rowsCollected[0].Name != "Alice" || rowsCollected[0].Hobby != "hiking" {
			t.Errorf("unexpected row 0: %+v", rowsCollected[0])
		}
		if rowsCollected[1].Name != "Alice" || rowsCollected[1].Hobby != "reading" {
			t.Errorf("unexpected row 1: %+v", rowsCollected[1])
		}
		if rowsCollected[2].Name != "Bob" || rowsCollected[2].Hobby != "gaming" {
			t.Errorf("unexpected row 2: %+v", rowsCollected[2])
		}
	})
}

// 2. UNION and UNION ALL
func TestFeature_Union_And_UnionAll(t *testing.T) {
	db := setupMemoryDB(t)
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO nodes (id, kind, properties) VALUES 
		('s1', 'Service', '{"name": "AuthService"}'),
		('s2', 'Service', '{"name": "CommonName"}'),
		('d1', 'Database', '{"name": "CommonName"}'),
		('d2', 'Database', '{"name": "MainDB"}');
	`)
	if err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	t.Run("UNION deduplicates matching rows", func(t *testing.T) {
		cypher := `
			MATCH (s:Service) RETURN s.name AS name
			UNION
			MATCH (d:Database) RETURN d.name AS name
		`
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			t.Fatalf("compilation error: %v", err)
		}

		if !strings.Contains(compiled.SQL, "UNION\n") && !strings.Contains(compiled.SQL, "UNION ") {
			t.Fatalf("expected UNION keyword in SQL:\n%s", compiled.SQL)
		}

		rows, err := db.Query(compiled.SQL, compiled.NamedArgs()...)
		if err != nil {
			t.Fatalf("execution error on SQL:\n%s\nErr: %v", compiled.SQL, err)
		}
		defer rows.Close()

		var names []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatalf("scan error: %v", err)
			}
			names = append(names, n)
		}

		// 'CommonName' should appear only once (AuthService, CommonName, MainDB = 3)
		if len(names) != 3 {
			t.Fatalf("expected 3 distinct names with UNION, got %d: %v", len(names), names)
		}
	})

	t.Run("UNION ALL preserves duplicates", func(t *testing.T) {
		cypher := `
			MATCH (s:Service) RETURN s.name AS name
			UNION ALL
			MATCH (d:Database) RETURN d.name AS name
		`
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			t.Fatalf("compilation error: %v", err)
		}

		if !strings.Contains(compiled.SQL, "UNION ALL") {
			t.Fatalf("expected UNION ALL keyword in SQL:\n%s", compiled.SQL)
		}

		rows, err := db.Query(compiled.SQL, compiled.NamedArgs()...)
		if err != nil {
			t.Fatalf("execution error: %v", compiled.SQL)
		}
		defer rows.Close()

		var names []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatalf("scan error: %v", err)
			}
			names = append(names, n)
		}

		// 'CommonName' should appear twice (total 4 rows)
		if len(names) != 4 {
			t.Fatalf("expected 4 rows with UNION ALL, got %d: %v", len(names), names)
		}
	})
}

// 3. CALL { ... } Subquery
func TestFeature_CallSubquery(t *testing.T) {
	db := setupMemoryDB(t)
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO nodes (id, kind, properties) VALUES 
		('s1', 'Service', '{"name": "PaymentService"}'),
		('d1', 'Database', '{"name": "PostgresDB"}');
	`)
	if err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	cypher := `
		MATCH (s:Service)
		CALL {
			MATCH (d:Database) RETURN d.name AS dbName
		}
		RETURN s.name AS serviceName, dbName
	`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("compilation error: %v", err)
	}

	rows, err := db.Query(compiled.SQL, compiled.NamedArgs()...)
	if err != nil {
		t.Fatalf("execution error on SQL:\n%s\nErr: %v", compiled.SQL, err)
	}
	defer rows.Close()

	type resRow struct {
		Service  string
		Database string
	}
	var res []resRow
	for rows.Next() {
		var r resRow
		if err := rows.Scan(&r.Service, &r.Database); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		res = append(res, r)
	}

	if len(res) != 1 || res[0].Service != "PaymentService" || res[0].Database != "PostgresDB" {
		t.Fatalf("unexpected call subquery result: %+v", res)
	}
}

// 4. shortestPath(...) and allShortestPaths(...)
func TestFeature_ShortestPath(t *testing.T) {
	db := setupMemoryDB(t)
	defer db.Close()

	// Graph: A -> B -> C (2 hops)
	//        A -> D -> E -> C (3 hops)
	_, err := db.Exec(`
		INSERT INTO nodes (id, kind, properties) VALUES 
		('A', 'Node', '{"name": "A"}'),
		('B', 'Node', '{"name": "B"}'),
		('C', 'Node', '{"name": "C"}'),
		('D', 'Node', '{"name": "D"}'),
		('E', 'Node', '{"name": "E"}');

		INSERT INTO edges (from_id, to_id, kind, properties) VALUES 
		('A', 'B', 'NEXT', '{}'),
		('B', 'C', 'NEXT', '{}'),
		('A', 'D', 'NEXT', '{}'),
		('D', 'E', 'NEXT', '{}'),
		('E', 'C', 'NEXT', '{}');
	`)
	if err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	cypher := `
		MATCH p = shortestPath((a:Node {id: 'A'})-[:NEXT*1..5]->(c:Node {id: 'C'}))
		RETURN a.name AS startNode, c.name AS endNode, length(p) AS depth
	`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("compilation error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "min(depth)") {
		t.Fatalf("expected min(depth) filter in shortestPath SQL:\n%s", compiled.SQL)
	}

	rows, err := db.Query(compiled.SQL, compiled.NamedArgs()...)
	if err != nil {
		t.Fatalf("execution error on SQL:\n%s\nErr: %v", compiled.SQL, err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var startN, endN string
		var depth int
		if err := rows.Scan(&startN, &endN, &depth); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		count++
		if depth != 2 {
			t.Errorf("expected shortest path depth 2, got %d", depth)
		}
	}

	if count != 1 {
		t.Fatalf("expected exactly 1 shortest path, got %d", count)
	}
}

// 5. Map Projection: n { .prop1, .prop2, custom: expr }
func TestFeature_MapProjection(t *testing.T) {
	db := setupMemoryDB(t)
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO nodes (id, kind, properties) VALUES 
		('s1', 'Service', '{"name": "Billing", "env": "prod", "cluster": "us-east"}');
	`)
	if err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	cypher := `
		MATCH (s:Service)
		RETURN s { .name, .env, role: 'backend', port: 8080 } AS svcMap
	`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("compilation error: %v", err)
	}

	if !strings.Contains(compiled.SQL, "json_object(") {
		t.Fatalf("expected json_object in map projection SQL:\n%s", compiled.SQL)
	}

	rows, err := db.Query(compiled.SQL, compiled.NamedArgs()...)
	if err != nil {
		t.Fatalf("execution error on SQL:\n%s\nErr: %v", compiled.SQL, err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected at least 1 row")
	}

	var jsonStr string
	if err := rows.Scan(&jsonStr); err != nil {
		t.Fatalf("scan error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("failed to parse returned JSON object %q: %v", jsonStr, err)
	}

	if parsed["name"] != "Billing" {
		t.Errorf("expected name 'Billing', got %v", parsed["name"])
	}
	if parsed["env"] != "prod" {
		t.Errorf("expected env 'prod', got %v", parsed["env"])
	}
	if parsed["role"] != "backend" {
		t.Errorf("expected role 'backend', got %v", parsed["role"])
	}
	if parsed["port"] != float64(8080) {
		t.Errorf("expected port 8080, got %v", parsed["port"])
	}
}

// 6. Label expression in WHERE: WHERE n:Label and chained WHERE n:Label1:Label2
func TestFeature_HasLabel_WherePredicate(t *testing.T) {
	db := setupMemoryDB(t)
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO nodes (id, kind, properties) VALUES 
		('1', 'Service', '{"name": "OrderService"}'),
		('2', 'Database', '{"name": "OrderDB"}'),
		('3', 'External', '{"name": "Stripe"}');
	`)
	if err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	t.Run("Single label check in WHERE", func(t *testing.T) {
		cypher := `MATCH (n) WHERE n:Service RETURN n.name AS name`
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			t.Fatalf("compilation error: %v", err)
		}

		if !strings.Contains(compiled.SQL, "n.kind = 'Service'") {
			t.Fatalf("expected n.kind = 'Service' in SQL:\n%s", compiled.SQL)
		}

		rows, err := db.Query(compiled.SQL, compiled.NamedArgs()...)
		if err != nil {
			t.Fatalf("execution error on SQL:\n%s\nErr: %v", compiled.SQL, err)
		}
		defer rows.Close()

		var names []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatalf("scan error: %v", err)
			}
			names = append(names, n)
		}

		if len(names) != 1 || names[0] != "OrderService" {
			t.Fatalf("unexpected names: %v", names)
		}
	})

	t.Run("Chained label checks in WHERE", func(t *testing.T) {
		cypher := `MATCH (n) WHERE n:Service:Worker RETURN n.name AS name`
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			t.Fatalf("compilation error: %v", err)
		}

		if !strings.Contains(compiled.SQL, "n.kind = 'Service'") || !strings.Contains(compiled.SQL, "n.kind = 'Worker'") {
			t.Fatalf("expected both label checks in SQL:\n%s", compiled.SQL)
		}
	})
}

// 7. reduce(...) expression
func TestFeature_ReduceExpression(t *testing.T) {
	db := setupMemoryDB(t)
	defer db.Close()

	cypher := `RETURN reduce(total = 50, x IN [10, 20, 30] | total + x) AS sumResult`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("compilation error: %v", err)
	}

	rows, err := db.Query(compiled.SQL, compiled.NamedArgs()...)
	if err != nil {
		t.Fatalf("execution error on SQL:\n%s\nErr: %v", compiled.SQL, err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected 1 row")
	}

	var total int
	if err := rows.Scan(&total); err != nil {
		t.Fatalf("scan error: %v", err)
	}

	// 50 + 10 + 20 + 30 = 110
	if total != 110 {
		t.Fatalf("expected reduce total 110, got %d", total)
	}
}

// 8. Introspection: keys(n)
func TestFeature_Introspection_Keys(t *testing.T) {
	db := setupMemoryDB(t)
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO nodes (id, kind, properties) VALUES 
		('s1', 'Service', '{"name": "Auth", "tier": "gold", "active": true}');
	`)
	if err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	cypher := `MATCH (s:Service) RETURN keys(s) AS propKeys`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("compilation error: %v", err)
	}

	rows, err := db.Query(compiled.SQL, compiled.NamedArgs()...)
	if err != nil {
		t.Fatalf("execution error on SQL:\n%s\nErr: %v", compiled.SQL, err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected 1 row")
	}

	var keysJSON string
	if err := rows.Scan(&keysJSON); err != nil {
		t.Fatalf("scan error: %v", err)
	}

	var keyList []string
	if err := json.Unmarshal([]byte(keysJSON), &keyList); err != nil {
		t.Fatalf("failed to unmarshal keys JSON %q: %v", keysJSON, err)
	}

	if len(keyList) != 3 {
		t.Fatalf("expected 3 keys, got %d: %v", len(keyList), keyList)
	}
}

// 9. ClickHouse Dialect Parity
func TestFeature_ClickHouse_DialectParity(t *testing.T) {
	chConfig := cyphersql.ClickHouseSchemaConfig()

	t.Run("UNWIND generates arrayJoin in ClickHouse", func(t *testing.T) {
		cypher := `UNWIND [1, 2, 3] AS num RETURN num`
		compiled, err := cyphersql.CompileWithSchema(cypher, chConfig)
		if err != nil {
			t.Fatalf("compilation error: %v", err)
		}
		if !strings.Contains(compiled.SQL, "arrayJoin") {
			t.Fatalf("expected arrayJoin in ClickHouse UNWIND SQL:\n%s", compiled.SQL)
		}
	})

	t.Run("Map Projection generates map() in ClickHouse", func(t *testing.T) {
		cypher := `MATCH (s:Service) RETURN s { .name, status: 'ok' } AS m`
		compiled, err := cyphersql.CompileWithSchema(cypher, chConfig)
		if err != nil {
			t.Fatalf("compilation error: %v", err)
		}
		if !strings.Contains(compiled.SQL, "map(") {
			t.Fatalf("expected map() in ClickHouse Map Projection SQL:\n%s", compiled.SQL)
		}
	})
}
