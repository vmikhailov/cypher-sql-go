package test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/vmikhailov/cypher-sql-go"
	_ "modernc.org/sqlite"
)

// setupScaledBenchmarkGraph creates an in-memory graph with N nodes and M edges.
func setupScaledBenchmarkGraph(b testing.TB, numNodes int) *sql.DB {
	b.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		b.Fatalf("failed to open sqlite: %v", err)
	}

	pragmas := `
		PRAGMA synchronous = OFF;
		PRAGMA journal_mode = MEMORY;
		PRAGMA temp_store = MEMORY;
		PRAGMA cache_size = -64000;

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
		CREATE INDEX idx_edges_from_kind ON edges(from_id, kind);
		CREATE INDEX idx_edges_to_kind ON edges(to_id, kind);
		CREATE INDEX idx_nodes_kind ON nodes(kind);
	`
	if _, err := db.Exec(pragmas); err != nil {
		b.Fatalf("pragmas failed: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		b.Fatalf("tx begin failed: %v", err)
	}
	defer tx.Rollback()

	insertNode, err := tx.Prepare("INSERT INTO nodes VALUES (?, ?, ?)")
	if err != nil {
		b.Fatalf("prep node: %v", err)
	}
	defer insertNode.Close()

	insertEdge, err := tx.Prepare("INSERT INTO edges VALUES (?, ?, ?, ?)")
	if err != nil {
		b.Fatalf("prep edge: %v", err)
	}
	defer insertEdge.Close()

	kinds := []string{"Service", "Database", "Endpoint", "Library"}
	teams := []string{"core", "payments", "search", "shopping", "platform"}

	for i := 0; i < numNodes; i++ {
		id := "node:" + strconv.Itoa(i)
		kind := kinds[i%len(kinds)]
		team := teams[i%len(teams)]
		props := fmt.Sprintf(`{"name":"svc_%d","team":"%s","tier":%d,"tags":["production","api"]}`,
			i, team, (i%3)+1)

		if _, err := insertNode.Exec(id, kind, props); err != nil {
			b.Fatalf("insert node: %v", err)
		}
	}

	// 3 edges per node on average
	for i := 0; i < numNodes; i++ {
		fromID := "node:" + strconv.Itoa(i)
		targetCalls := "node:" + strconv.Itoa((i*7+1)%numNodes)
		targetDB := "node:" + strconv.Itoa((i*13+3)%numNodes)
		targetDep := "node:" + strconv.Itoa((i*17+5)%numNodes)

		edgeProp := `{"protocol":"gRPC","latency_ms":15,"via":"direct"}`

		if _, err := insertEdge.Exec(fromID, targetCalls, "CALLS", edgeProp); err != nil {
			b.Fatalf("insert edge calls: %v", err)
		}
		if _, err := insertEdge.Exec(fromID, targetDB, "USES_DB", `{"mode":"read-write"}`); err != nil {
			b.Fatalf("insert edge db: %v", err)
		}
		if _, err := insertEdge.Exec(fromID, targetDep, "DEPENDS_ON", `{"version":"v1.0"}`); err != nil {
			b.Fatalf("insert edge dep: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		b.Fatalf("tx commit: %v", err)
	}

	return db
}

// 1. Compilation Phase Benchmarks
func BenchmarkPhase1_Compilation(b *testing.B) {
	queries := map[string]string{
		"SimpleMatch": `
			MATCH (s:Service) WHERE s.team = 'payments'
			RETURN s.name, s.tier
		`,
		"OneHopTraversal": `
			MATCH (a:Service)-[r:CALLS]->(b:Service)
			WHERE r.latency_ms > 10
			RETURN a.name AS caller, b.name AS callee, r.protocol AS proto
		`,
		"MultiHopTraversal": `
			MATCH (a:Service)-[r1:CALLS]->(b:Service)-[r2:CALLS]->(c:Service)
			WHERE a.name = 'svc_0'
			RETURN a.name, b.name, c.name
		`,
		"DecomposedOptionalMatch": `
			MATCH (s:Service) WHERE s.name = 'svc_0'
			OPTIONAL MATCH (s)-[:CALLS]->(target:Service)
			OPTIONAL MATCH (s)-[:USES_DB]->(d:Database)
			RETURN s.name, collect(target.name) AS calls, collect(d.name) AS dbs
		`,
		"QuantifiersAndCase": `
			MATCH (s:Service)
			WHERE any(t IN s.tags WHERE t = 'production')
			RETURN s.name,
			       CASE WHEN s.tier = 1 THEN 'critical' ELSE 'standard' END AS priority
		`,
	}

	for name, q := range queries {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := cyphersql.Compile(q)
				if err != nil {
					b.Fatalf("compile error: %v", err)
				}
			}
		})
	}
}

// 2. Execution Phase Benchmarks (SQLite on 5,000 Nodes / 15,000 Edges)
func BenchmarkPhase2_ExecutionOn5kGraph(b *testing.B) {
	db := setupScaledBenchmarkGraph(b, 5000)
	defer db.Close()

	cases := []struct {
		name   string
		cypher string
	}{
		{
			name: "PointLookup_ByID",
			cypher: `
				MATCH (s:Service) WHERE s.id = 'node:42'
				RETURN s.name, s.team
			`,
		},
		{
			name: "OneHop_IndexedTraversal",
			cypher: `
				MATCH (s:Service)-[r:CALLS]->(target:Service)
				WHERE s.id = 'node:42'
				RETURN s.name, target.name, r.protocol
			`,
		},
		{
			name: "TwoHop_DeepTraversal",
			cypher: `
				MATCH (a:Service)-[:CALLS]->(b:Service)-[:CALLS]->(c:Service)
				WHERE a.id = 'node:42'
				RETURN a.name, b.name, c.name
			`,
		},
		{
			name: "Decomposed_MultiBranchSubqueries",
			cypher: `
				MATCH (s:Service) WHERE s.id = 'node:42'
				OPTIONAL MATCH (s)-[:CALLS]->(t:Service)
				OPTIONAL MATCH (s)-[:USES_DB]->(d:Database)
				RETURN s.name, collect(t.name) AS calls, collect(d.name) AS dbs
			`,
		},
		{
			name: "FilteredScan_WithQuantifiers",
			cypher: `
				MATCH (s:Service)
				WHERE s.team = 'payments' AND any(t IN s.tags WHERE t = 'production')
				RETURN s.name, s.tier
				LIMIT 25
			`,
		},
		{
			name: "Aggregation_GroupByTeam",
			cypher: `
				MATCH (s:Service)-[:CALLS]->(t:Service)
				RETURN s.team AS team, count(t) AS callCount
				ORDER BY callCount DESC
			`,
		},
	}

	for _, tc := range cases {
		compiled, err := cyphersql.Compile(tc.cypher)
		if err != nil {
			b.Fatalf("compile error in %s: %v", tc.name, err)
		}

		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				rows, err := db.Query(compiled.SQL)
				if err != nil {
					b.Fatalf("exec error: %v", err)
				}
				for rows.Next() {
				}
				rows.Close()
			}
		})
	}
}

// 3. Performance Summary Test (Generates human-readable metrics table)
func TestPerformanceMetricsSummary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance summary in short mode")
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "metrics_5k.db")
	db := setupRealisticGraphDB(t, dbPath)
	defer db.Close()

	measure := func(name string, cypher string, iterations int) {
		// 1. Measure Compilation
		t0 := time.Now()
		var compiled *cyphersql.CompiledQuery
		var err error
		for i := 0; i < iterations; i++ {
			compiled, err = cyphersql.Compile(cypher)
			if err != nil {
				t.Fatalf("compile error: %v", err)
			}
		}
		compileLatency := time.Since(t0) / time.Duration(iterations)

		// 2. Measure Execution
		t1 := time.Now()
		rowCount := 0
		for i := 0; i < iterations; i++ {
			rows, err := db.Query(compiled.SQL)
			if err != nil {
				t.Fatalf("query error: %v\nSQL:\n%s", err, compiled.SQL)
			}
			for rows.Next() {
				rowCount++
			}
			rows.Close()
		}
		execLatency := time.Since(t1) / time.Duration(iterations)

		t.Logf("| %-32s | %10v | %10v | %8.0f qps |",
			name,
			compileLatency,
			execLatency,
			float64(time.Second)/float64(execLatency),
		)
	}

	t.Logf("\n=== PERFORMANCE METRICS SUMMARY ===")
	t.Logf("| %-32s | %10s | %10s | %12s |", "Query Type", "Compile", "Exec (SQLite)", "Throughput")
	t.Logf("|----------------------------------|------------|---------------|--------------|")

	measure("Point Lookup (by name)",
		"MATCH (s:Service) WHERE s.name = 'orders-service' RETURN s.name, s.team", 500)

	measure("1-Hop Traversal (r:CALLS)",
		"MATCH (a:Service)-[r:CALLS]->(b:Service) WHERE a.name = 'checkout-service' RETURN a.name, b.name, r.protocol", 500)

	measure("2-Hop Traversal (a->b->c)",
		"MATCH (a:Service)-[:CALLS]->(b:Service)-[:CALLS]->(c:Service) "+
			"WHERE a.name = 'checkout-service' RETURN a.name, c.name", 500)

	measure("Decomposed (No Cartesian)",
		"MATCH (s:Service) WHERE s.name = 'orders-service' "+
			"OPTIONAL MATCH (s)-[:USES_DB]->(d) OPTIONAL MATCH (s)-[:EXPOSES]->(e) "+
			"RETURN s.name, collect(d.name), collect(e.name)", 500)

	measure("Quantifier (any in tags)",
		"MATCH (s:Service) WHERE any(t IN s.tags WHERE t = 'pci-dss') RETURN s.name", 500)

	measure("Aggregation (GroupBy Team)",
		"MATCH (s:Service)-[:CALLS]->(b) RETURN s.team, count(b) AS cnt ORDER BY cnt DESC", 500)
}
