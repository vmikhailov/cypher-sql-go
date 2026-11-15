package test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/vmikhailov/cypher-sql-go"
	_ "modernc.org/sqlite"
)

func setupPortedFeaturesDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	schema := `
		CREATE TABLE nodes (id TEXT PRIMARY KEY, kind TEXT, properties TEXT);
		CREATE TABLE edges (from_id TEXT, to_id TEXT, kind TEXT, properties TEXT);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	data := `
		INSERT INTO nodes VALUES ('org:1', 'Org', '{"name": "Acme"}');
		INSERT INTO nodes VALUES ('p:alice', 'Person', '{"name": "Alice", "age": 30, "role": "eng", "tags": ["go", "sql", "cypher"]}');
		INSERT INTO nodes VALUES ('p:bob', 'Person', '{"name": "Bob", "age": 25, "role": "eng", "tags": ["rust", "go"]}');
		INSERT INTO nodes VALUES ('p:carol', 'Person', '{"name": "Carol", "age": 35, "role": "mgr", "tags": ["python"]}');
		INSERT INTO nodes VALUES ('p:dave', 'Person', '{"name": "Dave", "age": 20, "role": "intern", "tags": []}');

		INSERT INTO edges VALUES ('org:1', 'p:alice', 'HAS_MEMBER', '{}');
		INSERT INTO edges VALUES ('p:dave', 'p:alice', 'KNOWS', '{"weight": 1}');
		INSERT INTO edges VALUES ('p:alice', 'p:bob', 'KNOWS', '{"weight": 5}');
		INSERT INTO edges VALUES ('p:alice', 'p:carol', 'KNOWS', '{"weight": 10}');
		INSERT INTO edges VALUES ('p:bob', 'p:carol', 'KNOWS', '{"weight": 3}');
	`
	if _, err := db.Exec(data); err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	return db
}

// 1. Postfix property/index/slice access & list functions (head, last, tail)
func TestPorted_PostfixPropertyIndexSliceAndListFuncs(t *testing.T) {
	db := setupPortedFeaturesDB(t)
	defer db.Close()

	// startNode(r).name and endNode(r).name
	q1 := `MATCH (a:Person {name: 'Alice'})-[r:KNOWS]->(b:Person {name: 'Bob'})
	       RETURN startNode(r).name, endNode(r).name, startNode(r).id, endNode(r).id`
	c1, err := cyphersql.Compile(q1)
	if err != nil {
		t.Fatalf("q1 compile error: %v", err)
	}
	var sName, eName, sID, eID string
	if err := db.QueryRow(c1.SQL).Scan(&sName, &eName, &sID, &eID); err != nil {
		t.Fatalf("q1 exec error: %v\nSQL: %s", err, c1.SQL)
	}
	if sName != "Alice" || eName != "Bob" || sID != "p:alice" || eID != "p:bob" {
		t.Fatalf("unexpected startNode/endNode result: %s, %s, %s, %s", sName, eName, sID, eID)
	}

	// List indexing, negative indexing, slicing, head(), last(), tail()
	q2 := `MATCH (a:Person {name: 'Alice'})
	       RETURN a.tags[0], a.tags[-1], a.tags[1..3], head(a.tags), last(a.tags), tail(a.tags), ({x: 'hello'}).x`
	c2, err := cyphersql.Compile(q2)
	if err != nil {
		t.Fatalf("q2 compile error: %v", err)
	}
	var first, negLast, slice13, hd, lst, tl, mapX string
	if err := db.QueryRow(c2.SQL).Scan(&first, &negLast, &slice13, &hd, &lst, &tl, &mapX); err != nil {
		t.Fatalf("q2 exec error: %v\nSQL: %s", err, c2.SQL)
	}
	if first != "go" || negLast != "cypher" || slice13 != `["sql","cypher"]` || hd != "go" || lst != "cypher" || tl != `["sql","cypher"]` || mapX != "hello" {
		t.Fatalf("unexpected list/postfix results: first=%q negLast=%q slice=%q head=%q last=%q tail=%q mapX=%q",
			first, negLast, slice13, hd, lst, tl, mapX)
	}
}

// 2. OPTIONAL MATCH ... WHERE variable scope (type(r), r.prop, b.prop)
func TestPorted_OptionalMatchWhereScope(t *testing.T) {
	db := setupPortedFeaturesDB(t)
	defer db.Close()

	q := `MATCH (a:Person {name: 'Alice'})
	      OPTIONAL MATCH (a)-[r:KNOWS]->(b:Person)
	      WHERE type(r) = 'KNOWS' AND r.weight >= 8 AND b.age > 30
	      RETURN a.name, b.name, r.weight`
	compiled, err := cyphersql.Compile(q)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var aName, bName string
	var weight int
	if err := db.QueryRow(compiled.SQL).Scan(&aName, &bName, &weight); err != nil {
		t.Fatalf("exec error: %v\nSQL: %s", err, compiled.SQL)
	}
	if aName != "Alice" || bName != "Carol" || weight != 10 {
		t.Fatalf("expected Alice -> Carol (10), got %s -> %s (%d)", aName, bName, weight)
	}
}

// 3. Pattern Comprehension variable scope and named inner variables
func TestPorted_PatternComprehensionScope(t *testing.T) {
	db := setupPortedFeaturesDB(t)
	defer db.Close()

	q := `MATCH (a:Person {name: 'Alice'})
	      RETURN [(a)-[r:KNOWS]->(b:Person) WHERE b.age >= 25 AND r.weight >= 5 | b.name] AS friends`
	compiled, err := cyphersql.Compile(q)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var friendsJSON string
	if err := db.QueryRow(compiled.SQL).Scan(&friendsJSON); err != nil {
		t.Fatalf("exec error: %v\nSQL: %s", err, compiled.SQL)
	}
	if !strings.Contains(friendsJSON, "Bob") || !strings.Contains(friendsJSON, "Carol") {
		t.Fatalf("expected Bob and Carol in pattern comprehension result, got %s", friendsJSON)
	}
}

// 4. Bidirectional path chain expansion from anchorIndex (no ON 1=1 cartesian join)
func TestPorted_BidirectionalAnchorPathExpansion(t *testing.T) {
	db := setupPortedFeaturesDB(t)
	defer db.Close()

	q := `MATCH (a:Person {name: 'Alice'})
	      WITH a
	      MATCH (x:Person)-[:KNOWS]->(a)-[:KNOWS]->(y:Person)
	      RETURN x.name, y.name ORDER BY y.name`
	compiled, err := cyphersql.Compile(q)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	if strings.Contains(compiled.SQL, "ON 1=1") {
		t.Fatalf("expected bidirectional anchor expansion without ON 1=1 cartesian join, got SQL:\n%s", compiled.SQL)
	}

	rows, err := db.Query(compiled.SQL)
	if err != nil {
		t.Fatalf("exec error: %v\nSQL: %s", err, compiled.SQL)
	}
	defer rows.Close()

	var pairs [][2]string
	for rows.Next() {
		var x, y string
		if err := rows.Scan(&x, &y); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		pairs = append(pairs, [2]string{x, y})
	}
	if len(pairs) != 2 || pairs[0] != ([2]string{"Dave", "Bob"}) || pairs[1] != ([2]string{"Dave", "Carol"}) {
		t.Fatalf("expected [[Dave Bob] [Dave Carol]], got %+v\nSQL: %s", pairs, compiled.SQL)
	}
}

// 5. RETURN GroupBy alias collision with reserved columns (AS id, AS kind, AS properties)
func TestPorted_GroupByReservedColumnAliasCollision(t *testing.T) {
	db := setupPortedFeaturesDB(t)
	defer db.Close()

	q := `MATCH (n:Person)-[r:KNOWS]->(m:Person)
	      WHERE n.name = 'Alice'
	      RETURN n.name AS id, n.role AS kind, count(m) AS cnt`
	compiled, err := cyphersql.Compile(q)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	if strings.Contains(compiled.SQL, `GROUP BY "id"`) || strings.Contains(compiled.SQL, `GROUP BY id`) {
		t.Fatalf("GROUP BY should not use reserved column alias 'id':\n%s", compiled.SQL)
	}

	var idVal, kindVal string
	var cnt int
	if err := db.QueryRow(compiled.SQL).Scan(&idVal, &kindVal, &cnt); err != nil {
		t.Fatalf("exec error: %v\nSQL: %s", err, compiled.SQL)
	}
	if idVal != "Alice" || kindVal != "eng" || cnt != 2 {
		t.Fatalf("expected Alice, eng, 2; got %s, %s, %d", idVal, kindVal, cnt)
	}
}

// 6. Multi-stage WITH pipeline (ORDER BY, SKIP, LIMIT, chained aggregations via _stage_N CTEs)
func TestPorted_MultiStageWithPipeline(t *testing.T) {
	db := setupPortedFeaturesDB(t)
	defer db.Close()

	// Chained aggregations across two WITH clauses
	q1 := `MATCH (a:Person)-[:KNOWS]->(b:Person)
	       WITH a, count(b) AS out_deg
	       ORDER BY out_deg DESC
	       LIMIT 2
	       RETURN a.name, out_deg`
	c1, err := cyphersql.Compile(q1)
	if err != nil {
		t.Fatalf("q1 compile error: %v", err)
	}
	rows, err := db.Query(c1.SQL)
	if err != nil {
		t.Fatalf("q1 exec error: %v\nSQL: %s", err, c1.SQL)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		var deg int
		if err := rows.Scan(&name, &deg); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		names = append(names, name)
	}
	if len(names) != 2 || names[0] != "Alice" {
		t.Fatalf("expected top 2 starting with Alice, got %+v\nSQL: %s", names, c1.SQL)
	}

	// Stage split CTE when chaining aggregation -> aggregation
	q2 := `MATCH (a:Person)-[:KNOWS]->(b:Person)
	       WITH a, count(b) AS deg
	       WITH sum(deg) AS total_edges, count(a) AS active_people
	       RETURN total_edges, active_people`
	c2, err := cyphersql.Compile(q2)
	if err != nil {
		t.Fatalf("q2 compile error: %v", err)
	}
	var totalEdges, activePeople int
	if err := db.QueryRow(c2.SQL).Scan(&totalEdges, &activePeople); err != nil {
		t.Fatalf("q2 exec error: %v\nSQL: %s", err, c2.SQL)
	}
	if totalEdges != 4 || activePeople != 3 {
		t.Fatalf("expected total_edges=4, active_people=3; got %d, %d\nSQL: %s", totalEdges, activePeople, c2.SQL)
	}
}

// 7. EXISTS { ... } and COUNT { ... } Subquery Expressions (Cypher 5)
func TestPorted_ExistsAndCountSubqueries(t *testing.T) {
	db := setupPortedFeaturesDB(t)
	defer db.Close()

	// Shorthand pattern + WHERE inside EXISTS and COUNT
	q1 := `MATCH (p:Person)
	       WHERE EXISTS { (p)-[r:KNOWS]->(m:Person) WHERE r.weight >= 5 }
	       RETURN p.name, COUNT { (p)-[:KNOWS]->(m:Person) } AS out_cnt`
	c1, err := cyphersql.Compile(q1)
	if err != nil {
		t.Fatalf("q1 compile error: %v", err)
	}
	var pName string
	var outCnt int
	if err := db.QueryRow(c1.SQL).Scan(&pName, &outCnt); err != nil {
		t.Fatalf("q1 exec error: %v\nSQL: %s", err, c1.SQL)
	}
	if pName != "Alice" || outCnt != 2 {
		t.Fatalf("expected Alice with out_cnt=2, got %s, %d", pName, outCnt)
	}

	// Full MATCH ... WHERE ... RETURN ... subquery inside EXISTS and COUNT
	q2 := `MATCH (p:Person {name: 'Alice'})
	       RETURN EXISTS { MATCH (p)-[:KNOWS]->(b:Person) WHERE b.name = 'Carol' RETURN b } AS has_carol,
	              COUNT { MATCH (p)-[:KNOWS]->(b:Person) RETURN b LIMIT 1 } AS capped_cnt`
	c2, err := cyphersql.Compile(q2)
	if err != nil {
		t.Fatalf("q2 compile error: %v", err)
	}
	var hasCarol, cappedCnt int
	if err := db.QueryRow(c2.SQL).Scan(&hasCarol, &cappedCnt); err != nil {
		t.Fatalf("q2 exec error: %v\nSQL: %s", err, c2.SQL)
	}
	if hasCarol != 1 || cappedCnt != 1 {
		t.Fatalf("expected hasCarol=1, cappedCnt=1; got %d, %d\nSQL: %s", hasCarol, cappedCnt, c2.SQL)
	}
}

// 8. Variable-length path upgrades: undirected, bound variable + length(r)/size(r), 1-hop neighbor anchor, MaxPathTraversals
func TestPorted_VarLenUpgrades(t *testing.T) {
	db := setupPortedFeaturesDB(t)
	defer db.Close()

	// 8a. Bound variable [r*1..2] + length(r) + size(r) + MaxPathTraversals cap
	q1 := `MATCH (a:Person {name: 'Alice'})-[r:KNOWS*1..2]->(b:Person)
	       RETURN b.name, length(r), size(r) ORDER BY length(r), b.name`
	c1, err := cyphersql.Compile(q1)
	if err != nil {
		t.Fatalf("q1 compile error: %v", err)
	}
	if !strings.Contains(c1.SQL, "LIMIT 50000") {
		t.Fatalf("expected MaxPathTraversals LIMIT 50000 in SQL:\n%s", c1.SQL)
	}
	rows1, err := db.Query(c1.SQL)
	if err != nil {
		t.Fatalf("q1 exec error: %v\nSQL: %s", err, c1.SQL)
	}
	defer rows1.Close()

	var count1 int
	for rows1.Next() {
		var name string
		var l, s int
		if err := rows1.Scan(&name, &l, &s); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		if l != s || l < 1 || l > 2 {
			t.Fatalf("expected length(r)==size(r) in 1..2, got %d, %d", l, s)
		}
		count1++
	}
	if count1 != 3 { // Alice->Bob (1), Alice->Carol (1), Alice->Bob->Carol (2)
		t.Fatalf("expected 3 paths from Alice, got %d", count1)
	}

	// 8b. Undirected variable-length path (-[*1..2]-) with property anchor
	q2 := `MATCH (a:Person {name: 'Bob'})-[:KNOWS*1..2]-(b:Person)
	       WHERE b.name <> 'Bob'
	       RETURN DISTINCT b.name ORDER BY b.name`
	c2, err := cyphersql.Compile(q2)
	if err != nil {
		t.Fatalf("q2 compile error: %v", err)
	}
	rows2, err := db.Query(c2.SQL)
	if err != nil {
		t.Fatalf("q2 exec error: %v\nSQL: %s", err, c2.SQL)
	}
	defer rows2.Close()

	var reached []string
	for rows2.Next() {
		var name string
		if err := rows2.Scan(&name); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		reached = append(reached, name)
	}
	// From Bob (undirected 1..2 hops via KNOWS): Alice (1), Carol (1), Dave (2 via Alice)
	if len(reached) != 3 || reached[0] != "Alice" || reached[1] != "Carol" || reached[2] != "Dave" {
		t.Fatalf("expected [Alice Carol Dave], got %+v\nSQL: %s", reached, c2.SQL)
	}

	// 8c. 1-hop neighbor property anchor detection (a has no direct property filter, but is 1-hop from anchored org:1)
	q3 := `MATCH (o:Org {id: 'org:1'})-[:HAS_MEMBER]->(a:Person)-[:KNOWS*1..2]->(b:Person)
	       RETURN DISTINCT b.name ORDER BY b.name`
	c3, err := cyphersql.Compile(q3)
	if err != nil {
		t.Fatalf("q3 compile error (1-hop neighbor anchor should be accepted): %v", err)
	}
	rows3, err := db.Query(c3.SQL)
	if err != nil {
		t.Fatalf("q3 exec error: %v\nSQL: %s", err, c3.SQL)
	}
	defer rows3.Close()

	var orgReached []string
	for rows3.Next() {
		var name string
		if err := rows3.Scan(&name); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		orgReached = append(orgReached, name)
	}
	if len(orgReached) != 2 || orgReached[0] != "Bob" || orgReached[1] != "Carol" {
		t.Fatalf("expected [Bob Carol], got %+v\nSQL: %s", orgReached, c3.SQL)
	}
}
