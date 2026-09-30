// Package main demonstrates transpiling OpenCypher queries to ClickHouse SQL.
package main

import (
	"fmt"
	"log"

	"github.com/vmikhailov/cypher-sql-go"
)

func main() {
	cypherQuery := `
		MATCH (a:Service)-[r:CALLS]->(b:Service)
		WHERE any(t IN a.tags WHERE t = 'production') AND r.latency_ms > 50
		OPTIONAL MATCH (b)-[:USES_DB]->(d:Database)
		RETURN a.name AS caller,
		       b.name AS callee,
		       r.protocol AS protocol,
		       r.latency_ms AS latency,
		       collect(d.name) AS databases
		ORDER BY latency DESC
		LIMIT 10
	`

	// 1. Compile for standard SQLite (default)
	sqliteQuery, err := cyphersql.Compile(cypherQuery)
	if err != nil {
		log.Fatalf("sqlite compilation error: %v", err)
	}

	fmt.Println("==================================================")
	fmt.Println("1. SQLite Compiled SQL (Local / In-Process OLTP)")
	fmt.Println("==================================================")
	fmt.Println(sqliteQuery.SQL)
	fmt.Println()

	// 2. Compile for ClickHouse (Billion-scale Columnar OLAP)
	chConfig := cyphersql.ClickHouseSchemaConfig()
	chQuery, err := cyphersql.CompileWithSchema(cypherQuery, chConfig)
	if err != nil {
		log.Fatalf("clickhouse compilation error: %v", err)
	}

	fmt.Println("==================================================")
	fmt.Println("2. ClickHouse Compiled SQL (Large-Scale Analytics)")
	fmt.Println("==================================================")
	fmt.Println(chQuery.SQL)
}
