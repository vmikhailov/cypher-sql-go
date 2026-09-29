# CypherSQL for Go ⚡ (`cypher-sql-go`)

[![Go Reference](https://pkg.go.dev/badge/github.com/vmikhailov/cypher-sql-go.svg)](https://pkg.go.dev/github.com/vmikhailov/cypher-sql-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/vmikhailov/cypher-sql-go)](https://goreportcard.com/report/github.com/vmikhailov/cypher-sql-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

An **embedded OpenCypher to SQLite SQL compiler & query transpiler** written in pure Go.

`cyphersql` enables you to execute declarative graph queries (OpenCypher) directly on top of standard SQLite database tables (`nodes` and `edges`) using relational indexes and SQLite JSON1 functions. **Zero external services, zero native C++ binaries (100% CGO-free), zero memory overhead.**

---

## 🌟 Why CypherSQL?

Following the acquisition and deprecation of embedded graph engines like KùzuDB, building lightweight AI agents, personal knowledge graphs, or local code analyzers required either heavy client-server graph databases (Neo4j, Memgraph) or hand-writing complex 200-line SQL recursive CTEs.

`cyphersql` bridges this gap:
* **Write Cypher:** Clean, declarative graph queries that LLMs and AI agents excel at generating.
* **Run on SQLite:** Rock-solid, single-file, serverless relational database engine.
* **CGO-Free:** Fully compatible with pure-Go SQLite drivers (`modernc.org/sqlite`) for seamless cross-compilation to any OS/architecture.

---

## 🚀 Quickstart

### 1. Install

```bash
go get github.com/vmikhailov/cypher-sql-go
```

### 2. Usage with `database/sql`

```go
package main

import (
    "database/sql"
    "fmt"
    "log"

    "github.com/vmikhailov/cypher-sql-go"
    _ "modernc.org/sqlite"
)

func main() {
    db, err := sql.Open("sqlite", "graph.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // 1. Compile Cypher to SQLite SQL
    cypherQuery := `
        MATCH (a:Service)-[r:SERVICE_CALL]->(b:Service)
        WHERE any(t IN a.tags WHERE t = 'production')
        RETURN a.name AS caller, type(r) AS relType, r.via AS protocol, b.name AS callee
        ORDER BY a.name ASC
        LIMIT 10
    `

    compiled, err := cyphersql.Compile(cypherQuery)
    if err != nil {
        log.Fatalf("Compilation error: %v", err)
    }

    // 2. Execute on SQLite
    rows, err := db.Query(compiled.SQL)
    if err != nil {
        log.Fatalf("Execution error: %v", err)
    }
    defer rows.Close()

    for rows.Next() {
        var caller, relType, protocol, callee string
        if err := rows.Scan(&caller, &relType, &protocol, &callee); err != nil {
            log.Fatal(err)
        }
        fmt.Printf("%s -[%s (via %s)]-> %s\n", caller, relType, protocol, callee)
    }
}
```

---

## 🗄️ Standard SQLite Graph Schema

`cyphersql` operates on two standard tables:

```sql
CREATE TABLE nodes (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    properties TEXT NOT NULL -- JSON object
);

CREATE TABLE edges (
    from_id TEXT NOT NULL,
    to_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    properties TEXT NOT NULL -- JSON object
);

CREATE INDEX idx_edges_from ON edges(from_id);
CREATE INDEX idx_edges_to ON edges(to_id);
CREATE INDEX idx_edges_kind ON edges(kind);
CREATE INDEX idx_nodes_kind ON nodes(kind);
```

---

## ⚡ Core Capabilities

* **Graph Pattern Matching:**
  * Outgoing: `(a)-[:CALLS]->(b)`
  * Incoming: `(a)<-[:CALLS]-(b)`
  * Undirected: `(a)-[:CALLS]-(b)`
  * Multi-type: `(a)-[:CALLS|DEPENDS_ON]->(b)`
* **Cartesian Product Decomposition:**
  * Independent multi-branch `OPTIONAL MATCH` clauses are automatically decomposed into isolated correlated subqueries, preventing intermediate $O(N \cdot M \cdot K)$ row explosion.
* **Relationship Functions & Attributes:**
  * `type(r)`, `properties(r)`, `startNode(r)`, `endNode(r)`
  * Direct property access: `r.via`, `r.call_chain`, `r.latency`
* **Path & List Predicates:**
  * `WHERE EXISTS((a)-[:CALLS]->(b))` and bare path patterns `WHERE (a)-[:CALLS]->(b)`
  * `any(x IN list WHERE pred)`, `all(...)`, `none(...)`, `single(...)` guarded with `json_valid` checks.
* **Comprehensions & Functions:**
  * Pattern Comprehensions: `[(a)-[:DEPENDS_ON]->(b) | b.name]`
  * List Comprehensions: `[x IN list WHERE x > 0 | x * 2]`
  * Case expressions: `CASE WHEN ... THEN ... ELSE ... END`
  * Aggregations: `count()`, `collect()`, `sum()`, `avg()`, `min()`, `max()`

---

## 🧪 Running Tests

```bash
go test -v ./...
```

---

## 📄 License

MIT License. Copyright (c) 2026 Viacheslav Mikhailov.
