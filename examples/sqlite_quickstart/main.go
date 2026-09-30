// Package main demonstrates running OpenCypher queries on SQLite using cypher-sql-go.
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/vmikhailov/cypher-sql-go"
	_ "modernc.org/sqlite"
)

func main() {
	// 1. Open in-memory SQLite database (100% pure Go, Zero CGO)
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// 2. Initialize standard Universal Property Graph schema
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
		CREATE INDEX idx_edges_from_kind ON edges(from_id, kind);
		CREATE INDEX idx_edges_to_kind ON edges(to_id, kind);
		CREATE INDEX idx_nodes_kind ON nodes(kind);
	`
	if _, err := db.Exec(schema); err != nil {
		log.Fatalf("failed to create schema: %v", err)
	}

	// 3. Insert sample microservices graph data
	insertNode := func(id, kind string, props map[string]any) {
		b, _ := json.Marshal(props)
		_, err := db.Exec("INSERT INTO nodes VALUES (?, ?, ?)", id, kind, string(b))
		if err != nil {
			log.Fatalf("insert node %s: %v", id, err)
		}
	}
	insertEdge := func(from, to, kind string, props map[string]any) {
		b, _ := json.Marshal(props)
		_, err := db.Exec("INSERT INTO edges VALUES (?, ?, ?, ?)", from, to, kind, string(b))
		if err != nil {
			log.Fatalf("insert edge: %v", err)
		}
	}

	insertNode("svc:orders", "Service", map[string]any{"name": "orders-service", "team": "shopping"})
	insertNode("svc:payment", "Service", map[string]any{"name": "payment-service", "team": "payments"})
	insertNode("svc:billing", "Service", map[string]any{"name": "billing-service", "team": "payments"})
	insertNode("db:orders-pg", "Database", map[string]any{"name": "orders-postgres"})
	insertNode("db:redis", "Database", map[string]any{"name": "session-cache"})

	insertEdge("svc:orders", "svc:payment", "CALLS", map[string]any{"protocol": "gRPC", "via": "envoy"})
	insertEdge("svc:payment", "svc:billing", "CALLS", map[string]any{"protocol": "gRPC", "via": "direct"})
	insertEdge("svc:orders", "db:orders-pg", "USES_DB", map[string]any{"mode": "read-write"})
	insertEdge("svc:orders", "db:redis", "USES_DB", map[string]any{"mode": "read-write"})

	// 4. Write declarative OpenCypher query with multi-hop and decomposed OPTIONAL MATCH
	cypherQuery := `
		MATCH (s:Service)-[r:CALLS]->(target:Service)
		WHERE s.name = 'orders-service'
		OPTIONAL MATCH (s)-[:USES_DB]->(db:Database)
		RETURN s.name AS caller,
		       target.name AS callee,
		       r.protocol AS protocol,
		       collect(db.name) AS databases,
		       count(db) AS dbCount
	`

	// 5. Compile Cypher to SQLite SQL
	compiled, err := cyphersql.Compile(cypherQuery)
	if err != nil {
		log.Fatalf("compilation error: %v", err)
	}

	fmt.Println("=== Generated SQLite SQL ===")
	fmt.Println(compiled.SQL)
	fmt.Println()

	// 6. Execute compiled query on SQLite
	rows, err := db.Query(compiled.SQL)
	if err != nil {
		log.Fatalf("execution error: %v", err)
	}
	defer rows.Close()

	fmt.Println("=== Query Results ===")
	for rows.Next() {
		var caller, callee, protocol, dbsJSON string
		var dbCount int64
		if err := rows.Scan(&caller, &callee, &protocol, &dbsJSON, &dbCount); err != nil {
			log.Fatalf("scan error: %v", err)
		}
		fmt.Printf("%s -[%s]-> %s | Databases (%d): %s\n", caller, protocol, callee, dbCount, dbsJSON)
	}
}
