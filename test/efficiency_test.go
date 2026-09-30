package test

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/vmikhailov/cypher-sql-go"
	_ "modernc.org/sqlite"
)

// explainQueryPlan returns the human-readable execution plan from SQLite.
func explainQueryPlan(t testing.TB, db *sql.DB, querySQL string) []string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN " + querySQL)
	if err != nil {
		t.Fatalf("failed to explain query plan: %v\nSQL:\n%s", err, querySQL)
	}
	defer rows.Close()

	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("failed to scan query plan row: %v", err)
		}
		plan = append(plan, detail)
	}
	return plan
}

func TestQueryEfficiency_IndexUsageAndPlan(t *testing.T) {
	tempDir := t.TempDir()
	dbFile := tempDir + "/efficiency_test.db"
	db := setupRealisticGraphDB(t, dbFile)
	defer db.Close()

	t.Run("VerifyIndexUsage_MultiHopTraversal", func(t *testing.T) {
		cypher := `
			MATCH (c:Service)-[r1:CALLS]->(o:Service)-[r2:CALLS]->(u:Service)
			WHERE c.name = 'checkout-service'
			RETURN c.name, o.name, u.name
		`
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		plan := explainQueryPlan(t, db, compiled.SQL)
		t.Logf("=== EXPLAIN QUERY PLAN (MultiHop) ===")
		for _, step := range plan {
			t.Logf("  %s", step)
		}

		// Verify that all relationship hops and node lookups use indexes
		hasEdgeIndex := false
		hasNodeIndex := false
		hasTableScanOnEdges := false

		for _, step := range plan {
			lower := strings.ToLower(step)
			if strings.Contains(lower, "edges") {
				if strings.Contains(lower, "using index") || strings.Contains(lower, "using covering index") {
					hasEdgeIndex = true
				}
				if strings.Contains(lower, "scan edges") {
					hasTableScanOnEdges = true
				}
			}
			if strings.Contains(lower, "nodes") && strings.Contains(lower, "using") {
				hasNodeIndex = true
			}
		}

		if !hasEdgeIndex {
			t.Errorf("expected query plan to use index on edges table")
		}
		if hasTableScanOnEdges {
			t.Errorf("query plan has full scan on edges table: %v", plan)
		}
		if !hasNodeIndex {
			t.Errorf("expected query plan to use index on nodes table")
		}
	})

	t.Run("CartesianExplosionMitigation_PlanVerification", func(t *testing.T) {
		cypher := `
			MATCH (s:Service) WHERE s.name = 'orders-service'
			OPTIONAL MATCH (s)-[:EXPOSES]->(ep:Endpoint)
			OPTIONAL MATCH (s)-[:USES_DB]->(db:Database)
			RETURN s.name, collect(ep.name) AS eps, collect(db.name) AS dbs
		`
		compiled, err := cyphersql.Compile(cypher)
		if err != nil {
			t.Fatalf("compile error: %v", err)
		}

		plan := explainQueryPlan(t, db, compiled.SQL)
		t.Logf("=== EXPLAIN QUERY PLAN (Decomposed Subqueries) ===")
		for _, step := range plan {
			t.Logf("  %s", step)
		}

		// Verify plan isolates subqueries as CORRELATED SCALAR SUBQUERY
		subqueryCount := 0
		for _, step := range plan {
			if strings.Contains(step, "CORRELATED SCALAR SUBQUERY") {
				subqueryCount++
			}
		}

		if subqueryCount < 2 {
			t.Errorf("expected at least 2 correlated scalar subqueries, got %d", subqueryCount)
		}
	})
}

func BenchmarkQueryEfficiency_NaiveVsDecomposed(b *testing.B) {
	tempDir := b.TempDir()
	dbFile := tempDir + "/cartesian_bench.db"
	db := setupRealisticGraphDB(b, dbFile)
	defer db.Close()

	// 1. Decomposed Cypher query
	decomposedCypher := `
		MATCH (s:Service)
		OPTIONAL MATCH (s)-[:EXPOSES]->(ep:Endpoint)
		OPTIONAL MATCH (s)-[:USES_DB]->(db:Database)
		RETURN s.name, collect(ep.name) AS eps, collect(db.name) AS dbs
	`
	compiled, err := cyphersql.Compile(decomposedCypher)
	if err != nil {
		b.Fatalf("compile error: %v", err)
	}

	// 2. Naive SQL with Cartesian product explosion
	naiveSQL := `
		SELECT s.id, ep.id, d.id
		FROM nodes s
		LEFT JOIN edges e1 ON e1.from_id = s.id AND e1.kind = 'EXPOSES'
		LEFT JOIN nodes ep ON ep.id = e1.to_id AND ep.kind = 'Endpoint'
		LEFT JOIN edges e2 ON e2.from_id = s.id AND e2.kind = 'USES_DB'
		LEFT JOIN nodes d ON d.id = e2.to_id AND d.kind = 'Database'
		WHERE s.kind = 'Service'
	`

	b.Run("DecomposedSubqueries_CypherSQL", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rows, err := db.Query(compiled.SQL)
			if err != nil {
				b.Fatalf("query error: %v", err)
			}
			rowCount := 0
			for rows.Next() {
				var s, eps, dbs string
				_ = rows.Scan(&s, &eps, &dbs)
				rowCount++
			}
			rows.Close()
		}
	})

	b.Run("NaiveCartesianProduct_RawSQL", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rows, err := db.Query(naiveSQL)
			if err != nil {
				b.Fatalf("query error: %v", err)
			}
			rowCount := 0
			for rows.Next() {
				var s, ep, d sql.NullString
				_ = rows.Scan(&s, &ep, &d)
				rowCount++
			}
			rows.Close()
		}
	})
}

// BenchmarkScale_DeepTraversal measures traversal performance on scaled graph
func BenchmarkScale_DeepTraversal(b *testing.B) {
	tempDir := b.TempDir()
	dbFile := tempDir + "/scale_bench.db"
	db := setupRealisticGraphDB(b, dbFile)
	defer db.Close()

	cypher := `
		MATCH (s:Service)-[r:CALLS]->(target:Service)
		WHERE any(t IN s.tags WHERE t = 'production')
		RETURN s.name, target.name, r.protocol, r.latency_ms
	`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		b.Fatalf("compile error: %v", err)
	}

	b.ReportAllocs()
	start := time.Now()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rows, err := db.Query(compiled.SQL)
		if err != nil {
			b.Fatalf("query error: %v", err)
		}
		for rows.Next() {
			var s, t, proto string
			var lat int64
			_ = rows.Scan(&s, &t, &proto, &lat)
		}
		rows.Close()
	}
	b.StopTimer()

	opsPerSec := float64(b.N) / time.Since(start).Seconds()
	b.Logf("Throughput: %.0f queries/sec", opsPerSec)
}
