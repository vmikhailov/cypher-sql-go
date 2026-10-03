package test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/vmikhailov/cypher-sql-go"
	_ "modernc.org/sqlite"
)

func setupVarLenDB(t *testing.T) *sql.DB {
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

	// Anton -> Slava -> Istanbul Apt
	// Slava -> Anton (cycle!)
	// Anton -> Bob -> Carol
	data := `
		INSERT INTO nodes VALUES ('p:anton', 'Person', '{"name": "Anton"}');
		INSERT INTO nodes VALUES ('p:slava', 'Person', '{"name": "Slava"}');
		INSERT INTO nodes VALUES ('apt:1', 'Apartment', '{"name": "Istanbul Apt"}');
		INSERT INTO nodes VALUES ('p:bob', 'Person', '{"name": "Bob"}');
		INSERT INTO nodes VALUES ('p:carol', 'Person', '{"name": "Carol"}');

		INSERT INTO edges VALUES ('p:anton', 'p:slava', 'KNOWS', '{"status": "active"}');
		INSERT INTO edges VALUES ('p:slava', 'apt:1', 'OWNS', '{"status": "active"}');
		INSERT INTO edges VALUES ('p:slava', 'p:anton', 'KNOWS', '{"status": "active"}');

		INSERT INTO edges VALUES ('p:anton', 'p:bob', 'KNOWS', '{"status": "active"}');
		INSERT INTO edges VALUES ('p:bob', 'p:carol', 'KNOWS', '{"status": "archived"}');
	`
	if _, err := db.Exec(data); err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	return db
}

func TestVarLen_CyclePreventionAndAnchor(t *testing.T) {
	db := setupVarLenDB(t)
	defer db.Close()

	// 1. Forward traversal with start anchor: Anton -> Slava -> Apt (cycle Slava -> Anton does not loop infinitely)
	cypher := `MATCH (a:Person {name: 'Anton'})-[*1..3]->(b:Apartment) RETURN a.name, b.name`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	if !strings.Contains(compiled.SQL, "WITH RECURSIVE") {
		t.Fatalf("expected WITH RECURSIVE in SQL, got: %s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "instr(v.trail") {
		t.Fatalf("expected instr(v.trail) cycle prevention in SQL, got: %s", compiled.SQL)
	}

	rows, err := db.Query(compiled.SQL)
	if err != nil {
		t.Fatalf("query execution failed: %v\nSQL:\n%s", err, compiled.SQL)
	}
	defer rows.Close()

	var results [][2]string
	for rows.Next() {
		var aName, bName string
		if err := rows.Scan(&aName, &bName); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		results = append(results, [2]string{aName, bName})
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	if results[0][0] != "Anton" || results[0][1] != "Istanbul Apt" {
		t.Fatalf("unexpected result: %+v", results[0])
	}
}

func TestVarLen_TargetAnchorBackward(t *testing.T) {
	db := setupVarLenDB(t)
	defer db.Close()

	// 2. Traversal seeded from target anchor
	cypher := `MATCH (a:Person)-[*1..3]->(b:Apartment {name: 'Istanbul Apt'}) RETURN DISTINCT a.name, b.name`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	rows, err := db.Query(compiled.SQL)
	if err != nil {
		t.Fatalf("query execution failed: %v\nSQL:\n%s", err, compiled.SQL)
	}
	defer rows.Close()

	var results [][2]string
	for rows.Next() {
		var aName, bName string
		if err := rows.Scan(&aName, &bName); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		results = append(results, [2]string{aName, bName})
	}

	// Should reach Istanbul Apt from Anton and Slava
	if len(results) != 2 {
		t.Fatalf("expected 2 distinct results (Anton, Slava), got %d: %+v", len(results), results)
	}
}

func TestVarLen_IncomingDirection(t *testing.T) {
	db := setupVarLenDB(t)
	defer db.Close()

	// 3. Incoming direction (<-[*1..3]-)
	cypher := `MATCH (b:Apartment)<-[*1..3]-(a:Person {name: 'Anton'}) RETURN a.name, b.name`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	rows, err := db.Query(compiled.SQL)
	if err != nil {
		t.Fatalf("query execution failed: %v\nSQL:\n%s", err, compiled.SQL)
	}
	defer rows.Close()

	var results [][2]string
	for rows.Next() {
		var aName, bName string
		if err := rows.Scan(&aName, &bName); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		results = append(results, [2]string{aName, bName})
	}

	if len(results) != 1 || results[0][0] != "Anton" || results[0][1] != "Istanbul Apt" {
		t.Fatalf("expected [Anton, Istanbul Apt], got %+v", results)
	}
}

func TestVarLen_RelationshipTypeFilter(t *testing.T) {
	db := setupVarLenDB(t)
	defer db.Close()

	// Only KNOWS relationships -> Should NOT reach Apt (since Slava -> Apt is OWNS)
	cypher := `MATCH (a:Person {name: 'Anton'})-[:KNOWS*1..3]->(b) RETURN b.id`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	rows, err := db.Query(compiled.SQL)
	if err != nil {
		t.Fatalf("query execution failed: %v\nSQL:\n%s", err, compiled.SQL)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		ids = append(ids, id)
	}

	for _, id := range ids {
		if id == "apt:1" {
			t.Fatalf("apt:1 should not be reached via only KNOWS relationships: got %v", ids)
		}
	}
}

func TestVarLen_RelationshipPropertyFilter(t *testing.T) {
	db := setupVarLenDB(t)
	defer db.Close()

	// Only {status: 'active'} relationships -> Bob -> Carol is 'archived', so Carol should not be reached
	cypher := `MATCH (a:Person {name: 'Anton'})-[*1..3 {status: 'active'}]->(b:Person) RETURN b.id`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	rows, err := db.Query(compiled.SQL)
	if err != nil {
		t.Fatalf("query execution failed: %v\nSQL:\n%s", err, compiled.SQL)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		ids = append(ids, id)
	}

	for _, id := range ids {
		if id == "p:carol" {
			t.Fatalf("p:carol should not be reached with active relationship filter: got %v", ids)
		}
	}
}

func TestVarLen_UnanchoredExplosionProtection(t *testing.T) {
	// 1. Neither endpoint has filters and no LIMIT -> must return error requiring anchor
	cypherNoLimit := `MATCH (a)-[*1..2]->(b) RETURN a, b`
	_, err := cyphersql.Compile(cypherNoLimit)
	if err == nil {
		t.Fatalf("expected error for unanchored path without LIMIT, got nil")
	}
	if !strings.Contains(err.Error(), "anchored") {
		t.Fatalf("expected error mentioning anchored, got: %v", err)
	}

	// 2. Unanchored path even with LIMIT -> must also return error requiring anchor
	cypherWithLimit := `MATCH (a)-[*1..2]->(b) RETURN a, b LIMIT 50`
	_, err = cyphersql.Compile(cypherWithLimit)
	if err == nil {
		t.Fatalf("expected error for unanchored path with LIMIT, got nil")
	}
	if !strings.Contains(err.Error(), "anchored") {
		t.Fatalf("expected error mentioning anchored, got: %v", err)
	}
}
