package test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/vmikhailov/cypher-sql-go"
	_ "modernc.org/sqlite"
)

func setupMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}

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
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}
	return db
}

// TODO 4: Reject unquoted semicolons (prevent multi-statement execution)
func TestTODO4_UnquotedSemicolonsRejected(t *testing.T) {
	invalidQueries := []string{
		"MATCH (n) RETURN n;",
		"MATCH (n) RETURN n; DROP TABLE nodes;",
		"RETURN 1;",
		"MATCH (n); RETURN n",
	}

	for _, q := range invalidQueries {
		_, err := cyphersql.Compile(q)
		if err == nil {
			t.Fatalf("expected query %q with unquoted semicolon to fail, but it succeeded", q)
		}
	}

	// Semicolons inside string literals must still be allowed
	validQuery := "RETURN ';', 'hello; world' AS msg"
	compiled, err := cyphersql.Compile(validQuery)
	if err != nil {
		t.Fatalf("query with semicolon inside string literal should succeed, got error: %v", err)
	}
	if !strings.Contains(compiled.SQL, "';'") {
		t.Fatalf("expected compiled SQL to retain string literal with semicolon, got: %s", compiled.SQL)
	}
}

// TODO 6: Fix {id: ...} and {kind: ...} in node patterns by checking root schema columns
func TestTODO6_NodePatternProperties_RootColumns(t *testing.T) {
	cypher := "MATCH (p:Person {id: 'person:slava', kind: 'Person'}) RETURN p.id"
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	// Should match root columns, NOT json_extract(..., '$.id')
	if strings.Contains(compiled.SQL, "json_extract(p.properties, '$.id')") {
		t.Fatalf("expected root column id comparison, got JSON extract: %s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "p.id = 'person:slava'") {
		t.Fatalf("expected 'p.id = 'person:slava'', got SQL: %s", compiled.SQL)
	}

	// Test execution in SQLite
	db := setupMemoryDB(t)
	defer db.Close()

	_, err = db.Exec(`INSERT INTO nodes (id, kind, properties) VALUES ('person:slava', 'Person', '{"name":"Slava"}')`)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}
	_, err = db.Exec(`INSERT INTO nodes (id, kind, properties) VALUES ('person:other', 'Person', '{"name":"Other"}')`)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	rows, err := db.Query(compiled.SQL)
	if err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		count++
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("failed to scan: %v", err)
		}
		if id != "person:slava" {
			t.Fatalf("expected id 'person:slava', got %q", id)
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row, got %d", count)
	}
}

// TODO 7: Relationship property filter handling (rel.Properties) in processPathChain
func TestTODO7_RelationshipPropertiesHandled(t *testing.T) {
	cypher := "MATCH (a:Person)-[r:CALLS {status: 'active'}]->(b:Person) RETURN a.id, b.id"
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	if !strings.Contains(compiled.SQL, "json_extract(r.properties, '$.status') = 'active'") {
		t.Fatalf("expected SQL to contain rel property condition, got: %s", compiled.SQL)
	}

	// Test execution in SQLite
	db := setupMemoryDB(t)
	defer db.Close()

	_, err = db.Exec(`INSERT INTO nodes (id, kind, properties) VALUES ('p1', 'Person', '{}'), ('p2', 'Person', '{}')`)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}
	_, err = db.Exec(`INSERT INTO edges (from_id, to_id, kind, properties) VALUES ('p1', 'p2', 'CALLS', '{"status":"active"}')`)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}
	_, err = db.Exec(`INSERT INTO edges (from_id, to_id, kind, properties) VALUES ('p1', 'p2', 'CALLS', '{"status":"inactive"}')`)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	rows, err := db.Query(compiled.SQL)
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		count++
	}
	if count != 1 {
		t.Fatalf("expected 1 active edge matched, got %d", count)
	}
}

// TODO 8: Resolve target-bound multi-pattern JOIN bug where headVar is omitted from FROM/JOIN
func TestTODO8_TargetBoundMultiPatternJoin(t *testing.T) {
	cypher := `
		MATCH (a:Person)-[:WORKS_AT]->(b:Company)
		MATCH (c:Person)-[:MANAGES]->(b)
		RETURN a.id, b.id, c.id
	`
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	// Verify c is joined in the SQL
	if !strings.Contains(compiled.SQL, "nodes c") && !strings.Contains(compiled.SQL, "nodes AS c") {
		t.Fatalf("expected SQL to join nodes c, got: %s", compiled.SQL)
	}

	db := setupMemoryDB(t)
	defer db.Close()

	_, err = db.Exec(`
		INSERT INTO nodes (id, kind, properties) VALUES
			('p:alice', 'Person', '{"name":"Alice"}'),
			('p:boss', 'Person', '{"name":"Bob"}'),
			('c:acme', 'Company', '{"name":"Acme"}');
		INSERT INTO edges (from_id, to_id, kind, properties) VALUES
			('p:alice', 'c:acme', 'WORKS_AT', '{}'),
			('p:boss', 'c:acme', 'MANAGES', '{}');
	`)
	if err != nil {
		t.Fatalf("failed to seed: %v", err)
	}

	rows, err := db.Query(compiled.SQL)
	if err != nil {
		t.Fatalf("query execution failed: %v\nSQL: %s", err, compiled.SQL)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		count++
		var a, b, c string
		if err := rows.Scan(&a, &b, &c); err != nil {
			t.Fatalf("failed to scan: %v", err)
		}
		if a != "p:alice" || b != "c:acme" || c != "p:boss" {
			t.Fatalf("unexpected row: %s, %s, %s", a, b, c)
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
}

// TODO 9: Make FROM clause conditional in queryTemplate to support standalone RETURN
func TestTODO9_StandaloneReturnWithoutMatch(t *testing.T) {
	cypher := "RETURN 1 + 1 AS res, 'hello' AS msg"
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	if strings.Contains(compiled.SQL, "FROM") {
		t.Fatalf("standalone RETURN should not contain FROM clause, got: %s", compiled.SQL)
	}

	db := setupMemoryDB(t)
	defer db.Close()

	var res int
	var msg string
	err = db.QueryRow(compiled.SQL).Scan(&res, &msg)
	if err != nil {
		t.Fatalf("failed to execute standalone RETURN query %s: %v", compiled.SQL, err)
	}
	if res != 2 || msg != "hello" {
		t.Fatalf("expected 2, 'hello', got %d, %q", res, msg)
	}
}

// TODO 11: Built-in translation for id(n) and elementId(n) to n.id
func TestTODO11_IdAndElementIdFunctions(t *testing.T) {
	cypher := "MATCH (p:Person) RETURN id(p) AS pid, elementId(p) AS eid"
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	if strings.Contains(compiled.SQL, "id(p)") || strings.Contains(compiled.SQL, "elementId(p)") {
		t.Fatalf("expected id(p)/elementId(p) to be mapped to p.id, got: %s", compiled.SQL)
	}

	db := setupMemoryDB(t)
	defer db.Close()

	_, err = db.Exec(`INSERT INTO nodes (id, kind, properties) VALUES ('p:123', 'Person', '{}')`)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	var pid, eid string
	err = db.QueryRow(compiled.SQL).Scan(&pid, &eid)
	if err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}
	if pid != "p:123" || eid != "p:123" {
		t.Fatalf("expected 'p:123', 'p:123', got %q, %q", pid, eid)
	}
}

// TODO 12: Escape % and _ wildcards in STARTS WITH, ENDS WITH, CONTAINS using ESCAPE '\'
func TestTODO12_WildcardEscapingInLike(t *testing.T) {
	cypherContains := "MATCH (p:Person) WHERE p.name CONTAINS '100%' RETURN p.id"
	compiledContains, err := cyphersql.Compile(cypherContains)
	if err != nil {
		t.Fatalf("failed to compile CONTAINS: %v", err)
	}
	if !strings.Contains(compiledContains.SQL, `ESCAPE '\'`) {
		t.Fatalf("expected SQL to specify ESCAPE '\\', got: %s", compiledContains.SQL)
	}

	cypherStarts := "MATCH (p:Person) WHERE p.name STARTS WITH 'a_b' RETURN p.id"
	compiledStarts, err := cyphersql.Compile(cypherStarts)
	if err != nil {
		t.Fatalf("failed to compile STARTS WITH: %v", err)
	}
	if !strings.Contains(compiledStarts.SQL, `ESCAPE '\'`) {
		t.Fatalf("expected SQL to specify ESCAPE '\\', got: %s", compiledStarts.SQL)
	}

	db := setupMemoryDB(t)
	defer db.Close()

	_, err = db.Exec(`
		INSERT INTO nodes (id, kind, properties) VALUES
			('p1', 'Person', '{"name":"Save 100% now"}'),
			('p2', 'Person', '{"name":"Save 1000 items"}'),
			('p3', 'Person', '{"name":"a_b_c"}'),
			('p4', 'Person', '{"name":"axb_c"}');
	`)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// CONTAINS '100%' should match p1, but NOT p2
	rows, err := db.Query(compiledContains.SQL)
	if err != nil {
		t.Fatalf("failed to execute CONTAINS query: %v", err)
	}
	defer rows.Close()

	var containsIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		containsIDs = append(containsIDs, id)
	}
	if len(containsIDs) != 1 || containsIDs[0] != "p1" {
		t.Fatalf("CONTAINS: expected only ['p1'], got %v", containsIDs)
	}

	// STARTS WITH 'a_b' should match p3, but NOT p4
	rowsStarts, err := db.Query(compiledStarts.SQL)
	if err != nil {
		t.Fatalf("failed to execute STARTS WITH query: %v", err)
	}
	defer rowsStarts.Close()

	var startsIDs []string
	for rowsStarts.Next() {
		var id string
		if err := rowsStarts.Scan(&id); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		startsIDs = append(startsIDs, id)
	}
	if len(startsIDs) != 1 || startsIDs[0] != "p3" {
		t.Fatalf("STARTS WITH: expected only ['p3'], got %v", startsIDs)
	}
}

// TODO 13: Abstract IN expression rendering through Dialect to support ClickHouse
func TestTODO13_DialectInArray(t *testing.T) {
	cypher := "MATCH (p:Person) WHERE p.name IN ['Alice', 'Bob'] RETURN p.id"

	// SQLite
	sqliteComp, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("failed to compile for sqlite: %v", err)
	}
	if !strings.Contains(sqliteComp.SQL, "json_each") {
		t.Fatalf("expected sqlite SQL to use json_each, got: %s", sqliteComp.SQL)
	}

	// ClickHouse
	chComp, err := cyphersql.CompileWithSchema(cypher, cyphersql.ClickHouseSchemaConfig())
	if err != nil {
		t.Fatalf("failed to compile for clickhouse: %v", err)
	}
	if strings.Contains(chComp.SQL, "json_each") {
		t.Fatalf("ClickHouse SQL should not contain json_each, got: %s", chComp.SQL)
	}
	if !strings.Contains(chComp.SQL, "has(") {
		t.Fatalf("expected ClickHouse SQL to use has(...), got: %s", chComp.SQL)
	}
}

func TestReturnPropertyAccess_DefaultAlias(t *testing.T) {
	cypher := "MATCH (p:Person) RETURN p.name, p.id"
	compiled, err := cyphersql.Compile(cypher)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	if !strings.Contains(compiled.SQL, `AS "p.name"`) {
		t.Fatalf("expected column alias AS \"p.name\", got SQL: %s", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, `AS "p.id"`) {
		t.Fatalf("expected column alias AS \"p.id\", got SQL: %s", compiled.SQL)
	}
}

