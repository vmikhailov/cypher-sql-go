package test

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/vmikhailov/cypher-sql-go"
	_ "modernc.org/sqlite"
)

// setupRealisticGraphDB builds a realistic enterprise microservices graph
// with 20 services, 10 databases, 30 endpoints, 15 libraries, and 120+ edges.
func setupRealisticGraphDB(t testing.TB, dbPath string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite database at %s: %v", dbPath, err)
	}

	schema := `
		PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;

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
		t.Fatalf("failed to execute schema: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	insertNodeStmt, err := tx.Prepare("INSERT INTO nodes (id, kind, properties) VALUES (?, ?, ?)")
	if err != nil {
		t.Fatalf("failed to prepare insert node: %v", err)
	}
	defer insertNodeStmt.Close()

	insertEdgeStmt, err := tx.Prepare("INSERT INTO edges (from_id, to_id, kind, properties) VALUES (?, ?, ?, ?)")
	if err != nil {
		t.Fatalf("failed to prepare insert edge: %v", err)
	}
	defer insertEdgeStmt.Close()

	addNode := func(id, kind string, props map[string]any) {
		b, err := json.Marshal(props)
		if err != nil {
			t.Fatalf("marshal props: %v", err)
		}
		if _, err := insertNodeStmt.Exec(id, kind, string(b)); err != nil {
			t.Fatalf("insert node %s: %v", id, err)
		}
	}

	addEdge := func(from, to, kind string, props map[string]any) {
		b, err := json.Marshal(props)
		if err != nil {
			t.Fatalf("marshal props: %v", err)
		}
		if _, err := insertEdgeStmt.Exec(from, to, kind, string(b)); err != nil {
			t.Fatalf("insert edge %s->%s: %v", from, to, err)
		}
	}

	// 1. Services
	services := []struct {
		id   string
		name string
		team string
		tier int
		tags []string
	}{
		{"svc:auth", "auth-service", "security", 1, []string{"production", "core", "pci-dss"}},
		{"svc:billing", "billing-service", "payments", 1, []string{"production", "pci-dss"}},
		{"svc:catalog", "catalog-service", "shopping", 2, []string{"production", "high-read"}},
		{"svc:checkout", "checkout-service", "shopping", 1, []string{"production", "core"}},
		{"svc:notification", "notification-service", "platform", 3, []string{"internal", "async"}},
		{"svc:orders", "orders-service", "shopping", 1, []string{"production", "core"}},
		{"svc:payment", "payment-service", "payments", 1, []string{"production", "pci-dss", "sox"}},
		{"svc:search", "search-service", "shopping", 2, []string{"production", "high-read"}},
		{"svc:shipping", "shipping-service", "logistics", 2, []string{"production"}},
		{"svc:user", "user-service", "core", 1, []string{"production", "core", "gdpr"}},
		{"svc:inventory", "inventory-service", "logistics", 2, []string{"production"}},
		{"svc:recommend", "recommend-service", "growth", 3, []string{"internal", "ml"}},
		{"svc:reporting", "reporting-service", "analytics", 3, []string{"internal", "batch"}},
		{"svc:audit", "audit-service", "security", 2, []string{"production", "compliance"}},
		{"svc:mailer", "mailer-worker", "platform", 3, []string{"internal", "async"}},
	}

	for _, s := range services {
		addNode(s.id, "Service", map[string]any{
			"name": s.name,
			"team": s.team,
			"tier": s.tier,
			"tags": s.tags,
		})
	}

	// 2. Databases
	databases := []struct {
		id     string
		name   string
		engine string
	}{
		{"db:auth-pg", "auth-postgres", "postgres"},
		{"db:orders-pg", "orders-postgres", "postgres"},
		{"db:billing-aurora", "billing-aurora", "mysql"},
		{"db:redis-session", "session-cache", "redis"},
		{"db:search-es", "search-cluster", "opensearch"},
		{"db:event-kafka", "events-stream", "kafka"},
		{"db:analytics-ch", "analytics-dw", "clickhouse"},
	}

	for _, d := range databases {
		addNode(d.id, "Database", map[string]any{
			"name":   d.name,
			"engine": d.engine,
		})
	}

	// 3. Endpoints
	for i, s := range services {
		epPostID := "ep:post:" + s.id
		addNode(epPostID, "Endpoint", map[string]any{
			"name":   "POST /api/v1/" + s.name,
			"method": "POST",
			"p99_ms": 25 + (i * 3),
		})
		addEdge(s.id, epPostID, "EXPOSES", map[string]any{"public": true})

		epGetID := "ep:get:" + s.id
		addNode(epGetID, "Endpoint", map[string]any{
			"name":   "GET /api/v1/" + s.name + "/:id",
			"method": "GET",
			"p99_ms": 10 + (i * 2),
		})
		addEdge(s.id, epGetID, "EXPOSES", map[string]any{"public": true})
	}

	// 4. Libraries
	libs := []string{"pkg/auth", "pkg/tracing", "pkg/db", "pkg/events", "pkg/metrics"}
	for _, l := range libs {
		addNode("lib:"+l, "Library", map[string]any{
			"name":       l,
			"is_library": 1,
			"role":       "Library",
		})
	}

	// 5. Edges: SERVICE_CALLS
	callGraph := [][4]any{
		{"svc:checkout", "svc:orders", "gRPC", 15},
		{"svc:checkout", "svc:payment", "gRPC", 85},
		{"svc:checkout", "svc:inventory", "gRPC", 20},
		{"svc:orders", "svc:user", "gRPC", 10},
		{"svc:orders", "svc:notification", "HTTP", 45},
		{"svc:orders", "svc:audit", "gRPC", 12},
		{"svc:payment", "svc:billing", "gRPC", 60},
		{"svc:payment", "svc:audit", "gRPC", 15},
		{"svc:search", "svc:catalog", "gRPC", 18},
		{"svc:recommend", "svc:catalog", "HTTP", 30},
		{"svc:reporting", "svc:billing", "HTTP", 120},
		{"svc:shipping", "svc:orders", "gRPC", 25},
		{"svc:user", "svc:auth", "gRPC", 8},
	}

	for _, call := range callGraph {
		addEdge(call[0].(string), call[1].(string), "CALLS", map[string]any{
			"protocol":   call[2].(string),
			"latency_ms": call[3].(int),
			"via":        call[2].(string),
		})
	}

	// 6. Edges: USES_DB
	dbBindings := [][3]string{
		{"svc:auth", "db:auth-pg", "read-write"},
		{"svc:auth", "db:redis-session", "read-write"},
		{"svc:orders", "db:orders-pg", "read-write"},
		{"svc:orders", "db:event-kafka", "write-only"},
		{"svc:billing", "db:billing-aurora", "read-write"},
		{"svc:search", "db:search-es", "read-only"},
		{"svc:reporting", "db:analytics-ch", "read-only"},
		{"svc:recommend", "db:analytics-ch", "read-only"},
	}

	for _, b := range dbBindings {
		addEdge(b[0], b[1], "USES_DB", map[string]any{
			"mode": b[2],
		})
	}

	// 7. Edges: DEPENDS_ON Library
	for _, s := range services {
		addEdge(s.id, "lib:pkg/tracing", "DEPENDS_ON", map[string]any{"version": "v1.4.0"})
		addEdge(s.id, "lib:pkg/metrics", "DEPENDS_ON", map[string]any{"version": "v2.1.0"})
	}
	addEdge("svc:auth", "lib:pkg/auth", "DEPENDS_ON", map[string]any{"version": "v3.0.1"})
	addEdge("svc:orders", "lib:pkg/db", "DEPENDS_ON", map[string]any{"version": "v2.0.0"})
	addEdge("svc:billing", "lib:pkg/db", "DEPENDS_ON", map[string]any{"version": "v2.0.0"})
	addEdge("svc:orders", "lib:pkg/events", "DEPENDS_ON", map[string]any{"version": "v1.1.0"})

	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit transaction: %v", err)
	}

	return db
}

func TestRealisticGraph_DatabaseAndQueries(t *testing.T) {
	tempDir := t.TempDir()
	dbFile := filepath.Join(tempDir, "enterprise_graph.db")

	db := setupRealisticGraphDB(t, dbFile)
	defer db.Close()

	// Scenario 1: Multi-hop dependency traversal
	t.Run("MultiHopServiceDependency", func(t *testing.T) {
		cypher := `
			MATCH (c:Service)-[r1:CALLS]->(o:Service)-[r2:CALLS]->(u:Service)
			WHERE c.name = 'checkout-service'
			RETURN c.name AS client, o.name AS intermediate, u.name AS target, r1.protocol AS p1, r2.protocol AS p2
		`
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		rows, err := db.Query(compiled.SQL)
		if err != nil {
			t.Fatalf("query error: %v\nSQL:\n%s", err, compiled.SQL)
		}
		defer rows.Close()

		found := false
		for rows.Next() {
			var client, intermediate, target, p1, p2 string
			if err := rows.Scan(&client, &intermediate, &target, &p1, &p2); err != nil {
				t.Fatalf("scan error: %v", err)
			}
			if client == "checkout-service" && intermediate == "orders-service" && target == "user-service" {
				found = true
				if p1 != "gRPC" || p2 != "gRPC" {
					t.Errorf("unexpected protocols: %s, %s", p1, p2)
				}
			}
		}
		if !found {
			t.Errorf("expected checkout -> orders -> user path not found")
		}
	})

	// Scenario 2: Anti-Cartesian product multi-branch OPTIONAL MATCH
	t.Run("MultiBranchOptionalMatch_NoCartesianExplosion", func(t *testing.T) {
		cypher := `
			MATCH (s:Service)
			WHERE s.name = 'orders-service'
			OPTIONAL MATCH (s)-[:EXPOSES]->(ep:Endpoint)
			OPTIONAL MATCH (s)-[:USES_DB]->(db:Database)
			OPTIONAL MATCH (s)-[:DEPENDS_ON]->(lib:Library)
			RETURN s.name AS service,
			       collect(ep.name) AS endpoints,
			       collect(db.name) AS databases,
			       collect(lib.name) AS libraries,
			       count(ep) AS epCount,
			       count(db) AS dbCount,
			       count(lib) AS libCount
		`
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		rows, err := db.Query(compiled.SQL)
		if err != nil {
			t.Fatalf("query error: %v\nSQL:\n%s", err, compiled.SQL)
		}
		defer rows.Close()

		if !rows.Next() {
			t.Fatalf("expected 1 row returned")
		}

		var service, epsJSON, dbsJSON, libsJSON string
		var epCount, dbCount, libCount int64
		if err := rows.Scan(&service, &epsJSON, &dbsJSON, &libsJSON, &epCount, &dbCount, &libCount); err != nil {
			t.Fatalf("scan error: %v", err)
		}

		if service != "orders-service" {
			t.Errorf("expected orders-service, got %s", service)
		}
		if epCount != 2 {
			t.Errorf("expected 2 endpoints, got %d", epCount)
		}
		if dbCount != 2 {
			t.Errorf("expected 2 databases, got %d", dbCount)
		}
		if libCount != 4 { // pkg/tracing, pkg/metrics, pkg/db, pkg/events
			t.Errorf("expected 4 libraries, got %d", libCount)
		}

		var eps []string
		_ = json.Unmarshal([]byte(epsJSON), &eps)
		if len(eps) != 2 {
			t.Errorf("expected exact 2 collected endpoints without cartesian duplicates, got %v", eps)
		}
	})

	// Scenario 3: Quantifiers and Edge properties filter
	t.Run("QuantifiersAndEdgeProperties", func(t *testing.T) {
		cypher := `
			MATCH (s:Service)-[r:CALLS]->(target:Service)
			WHERE any(t IN s.tags WHERE t = 'pci-dss') AND r.latency_ms >= 50
			RETURN s.name AS caller, target.name AS callee, r.latency_ms AS latency, type(r) AS relType
			ORDER BY r.latency_ms DESC
		`
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		rows, err := db.Query(compiled.SQL)
		if err != nil {
			t.Fatalf("query error: %v\nSQL:\n%s", err, compiled.SQL)
		}
		defer rows.Close()

		count := 0
		for rows.Next() {
			var caller, callee, relType string
			var latency int64
			if err := rows.Scan(&caller, &callee, &latency, &relType); err != nil {
				t.Fatalf("scan error: %v", err)
			}
			if latency < 50 {
				t.Errorf("latency must be >= 50, got %d", latency)
			}
			if relType != "CALLS" {
				t.Errorf("expected CALLS, got %s", relType)
			}
			count++
		}
		if count == 0 {
			t.Errorf("expected at least 1 heavy latency PCI-DSS call")
		}
	})

	// Scenario 4: Grouping, Aggregation and Sorting
	t.Run("TeamCallsAggregation", func(t *testing.T) {
		cypher := `
			MATCH (s:Service)-[:CALLS]->(t:Service)
			RETURN s.team AS team, count(t) AS callCount
			ORDER BY callCount DESC
		`
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		rows, err := db.Query(compiled.SQL)
		if err != nil {
			t.Fatalf("query error: %v\nSQL:\n%s", err, compiled.SQL)
		}
		defer rows.Close()

		teamsFound := make(map[string]int64)
		for rows.Next() {
			var team string
			var count int64
			if err := rows.Scan(&team, &count); err != nil {
				t.Fatalf("scan error: %v", err)
			}
			teamsFound[team] = count
		}

		if teamsFound["shopping"] == 0 {
			t.Errorf("expected shopping team calls in aggregation")
		}
	})
}

func BenchmarkRealDataset_CompileAndExecute(b *testing.B) {
	tempDir := b.TempDir()
	dbFile := filepath.Join(tempDir, "bench_graph.db")
	db := setupRealisticGraphDB(b, dbFile)
	defer db.Close()

	cypher := `
		MATCH (s:Service)
		WHERE any(t IN s.tags WHERE t = 'production')
		OPTIONAL MATCH (s)-[:USES_DB]->(d:Database)
		RETURN s.name AS service, s.team AS team, collect(d.name) AS dbs, count(d) AS dbCount
		ORDER BY s.name ASC
		LIMIT 10
	`

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			b.Fatalf("compile error: %v", err)
		}

		rows, err := db.Query(compiled.SQL)
		if err != nil {
			b.Fatalf("query error: %v", err)
		}

		rowCount := 0
		for rows.Next() {
			var svc, team, dbs string
			var cnt int64
			if err := rows.Scan(&svc, &team, &dbs, &cnt); err != nil {
				rows.Close()
				b.Fatalf("scan error: %v", err)
			}
			rowCount++
		}
		rows.Close()

		if rowCount == 0 {
			b.Fatalf("expected rows")
		}
	}
}
