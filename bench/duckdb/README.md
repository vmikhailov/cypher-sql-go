# cypher-sql-go (SQLite) vs DuckDB + DuckPGQ Benchmark

> **Comprehensive Performance Analysis**: Comparing `cypher-sql-go` (zero-overhead Cypher-to-SQL transpilation on embedded SQLite) against **DuckDB v1.2.2 + DuckPGQ** (embedded OLAP columnar engine with ISO SQL/PGQ property graph extension).

## Test Environment

| Component | Specification |
|:---|:---|
| **Operating System** | windows (amd64) |
| **Go Runtime** | `go1.26.3` |
| **CPU Logical Cores** | 12 |
| **cypher-sql-go Engine** | Embedded SQLite (`modernc.org/sqlite` pure Go, zero CGO) |
| **DuckDB Engine** | Embedded DuckDB v1.2.2 C-ABI (`duckdb.dll`, direct syscall lazy binding, zero CGO) |
| **Graph Extension** | DuckPGQ (ISO SQL:2023 `GRAPH_TABLE` Property Graph Extension) |
| **Dataset Scale** | 100,000 Nodes, 198,000 Edges (Synthetic Enterprise Microservice Architecture) |

## 1. Ingestion Throughput & Storage Footprint

| Metric | cypher-sql-go (SQLite) | DuckDB + DuckPGQ | Ratio / Winner |
|:---|:---:|:---:|:---:|
| **Node Load Throughput** | `91633` nodes/sec | `586133` nodes/sec | **DuckDB** (6.4x) |
| **Edge Load Throughput** | `159303` edges/sec | `1038626` edges/sec | **DuckDB** (6.5x) |
| **Total Ingest Duration** | `2.87 s` | `0.38 s` | **DuckDB** (7.6x) |
| **On-Disk Database Size** | `34.00 MB` | `5.51 MB` | **DuckDB** (6.2x smaller) |

## 2. Query Latency Breakdown

Evaluated across 7 standard microservice graph topology queries. Latencies reported in milliseconds (ms), lower is better.

| ID | Query Pattern | Compile (µs) | SQLite Precmp (P50) | SQLite E2E (Avg) | DuckDB Ad-Hoc (Avg) | DuckDB Prepd (Avg) | Winner | Relative Score |
|:--:|:---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| **Q1** | Exact Point Lookup | `11.2 µs` | `0.00 ms` | `0.07 ms` | `0.72 ms` | `0.16 ms` | **SQLite** (10.1x) | `1004.6%` |
| **Q2** | Filtered Property Scan | `14.5 µs` | `0.51 ms` | `0.45 ms` | `0.59 ms` | `0.21 ms` | **SQLite** (1.3x) | `130.2%` |
| **Q3** | 1-Hop Traversal + Aggregation | `17.4 µs` | `6.70 ms` | `6.69 ms` | `5.72 ms` | `3.47 ms` | **DuckDB** (1.2x) | `85.4%` |
| **Q4** | 2-Hop Multi-Join Traversal | `19.4 µs` | `0.50 ms` | `0.44 ms` | `3.85 ms` | `2.89 ms` | **SQLite** (8.7x) | `867.6%` |
| **Q5** | Variable-Length Path (1..3 hops) | `30.4 µs` | `3.05 ms` | `2.90 ms` | `17.40 ms` | `13.36 ms` | **SQLite** (6.0x) | `598.9%` |
| **Q6** | Degree Centrality Aggregation | `16.4 µs` | `8.14 ms` | `8.37 ms` | `3.55 ms` | `2.77 ms` | **DuckDB** (2.4x) | `42.4%` |
| **Q7** | 2-Tier Hierarchy (S->C->M) | `28.1 µs` | `0.51 ms` | `0.47 ms` | `16.28 ms` | `13.60 ms` | **SQLite** (34.3x) | `3425.9%` |

## 3. Query Details & SQL/PGQ Mapping

### Q1: Exact Point Lookup

> Indexed point lookup of single service properties

**Cypher Query (`cypher-sql-go`):**
```cypher
MATCH (s:Service {name: 'service_420'}) RETURN s.id, s.name, s.layer, s.framework
```

**SQL/PGQ Query (`DuckDB + DuckPGQ`):**
```sql
FROM GRAPH_TABLE (
  microservices
  MATCH (s:Service WHERE s.name = 'service_420')
  COLUMNS (s.id, s.name, s.layer, s.framework)
);
```

- **Row Count Consistency**: SQLite returned `1` rows, DuckPGQ returned `1` rows.
- **SQLite End-to-End**: P50=`0.00ms`, Avg=`0.07ms`, P99=`1.01ms`
- **DuckDB Ad-Hoc**: P50=`0.53ms`, Avg=`0.72ms`, P99=`1.52ms`
- **DuckDB Prepared**: P50=`0.00ms`, Avg=`0.16ms`, P99=`1.01ms`

### Q2: Filtered Property Scan

> Filter 100k nodes by property with LIMIT 50

**Cypher Query (`cypher-sql-go`):**
```cypher
MATCH (s:Service) WHERE s.layer = 'Application' RETURN s.name, s.framework, s.language LIMIT 50
```

**SQL/PGQ Query (`DuckDB + DuckPGQ`):**
```sql
FROM GRAPH_TABLE (
  microservices
  MATCH (s:Service WHERE s.layer = 'Application')
  COLUMNS (s.name, s.framework, s.language)
) LIMIT 50;
```

- **Row Count Consistency**: SQLite returned `50` rows, DuckPGQ returned `50` rows.
- **SQLite End-to-End**: P50=`0.00ms`, Avg=`0.45ms`, P99=`1.65ms`
- **DuckDB Ad-Hoc**: P50=`0.51ms`, Avg=`0.59ms`, P99=`1.54ms`
- **DuckDB Prepared**: P50=`0.00ms`, Avg=`0.21ms`, P99=`1.01ms`

### Q3: 1-Hop Traversal + Aggregation

> Join Service->Database with GROUP BY and ORDER BY

**Cypher Query (`cypher-sql-go`):**
```cypher
MATCH (s:Service)-[:USES_DB]->(d:Database) RETURN s.name, count(d) AS db_count ORDER BY db_count DESC LIMIT 20
```

**SQL/PGQ Query (`DuckDB + DuckPGQ`):**
```sql
SELECT s_name, count(d_name) AS db_count
FROM GRAPH_TABLE (
  microservices
  MATCH (s:Service)-[r:USES_DB]->(d:Database)
  COLUMNS (s.name AS s_name, d.name AS d_name)
)
GROUP BY s_name
ORDER BY db_count DESC
LIMIT 20;
```

- **Row Count Consistency**: SQLite returned `20` rows, DuckPGQ returned `20` rows.
- **SQLite End-to-End**: P50=`6.63ms`, Avg=`6.69ms`, P99=`9.35ms`
- **DuckDB Ad-Hoc**: P50=`5.71ms`, Avg=`5.72ms`, P99=`7.64ms`
- **DuckDB Prepared**: P50=`3.18ms`, Avg=`3.47ms`, P99=`6.89ms`

### Q4: 2-Hop Multi-Join Traversal

> 2-hop join pattern: Service->Service->Database

**Cypher Query (`cypher-sql-go`):**
```cypher
MATCH (s1:Service)-[:CALLS]->(s2:Service)-[:USES_DB]->(d:Database) RETURN s1.name, s2.name, d.name LIMIT 50
```

**SQL/PGQ Query (`DuckDB + DuckPGQ`):**
```sql
FROM GRAPH_TABLE (
  microservices
  MATCH (s1:Service)-[c:CALLS_SERVICE]->(s2:Service)-[u:USES_DB]->(d:Database)
  COLUMNS (s1.name AS s1_name, s2.name AS s2_name, d.name AS d_name)
) LIMIT 50;
```

- **Row Count Consistency**: SQLite returned `50` rows, DuckPGQ returned `50` rows.
- **SQLite End-to-End**: P50=`0.50ms`, Avg=`0.44ms`, P99=`1.53ms`
- **DuckDB Ad-Hoc**: P50=`4.02ms`, Avg=`3.85ms`, P99=`6.12ms`
- **DuckDB Prepared**: P50=`3.02ms`, Avg=`2.89ms`, P99=`4.10ms`

### Q5: Variable-Length Path (1..3 hops)

> Recursive path finding with cycle prevention and DISTINCT

**Cypher Query (`cypher-sql-go`):**
```cypher
MATCH (s:Service {name: 'service_10'})-[:CALLS*1..3]->(target:Service) RETURN DISTINCT target.name LIMIT 100
```

**SQL/PGQ Query (`DuckDB + DuckPGQ`):**
```sql
SELECT DISTINCT target_name
FROM GRAPH_TABLE (
  microservices
  MATCH (s:Service WHERE s.name = 'service_10')-[c:CALLS_SERVICE]->{1,3}(target:Service)
  COLUMNS (target.name AS target_name)
) LIMIT 100;
```

- **Row Count Consistency**: SQLite returned `15` rows, DuckPGQ returned `15` rows.
- **SQLite End-to-End**: P50=`3.05ms`, Avg=`2.90ms`, P99=`5.13ms`
- **DuckDB Ad-Hoc**: P50=`17.41ms`, Avg=`17.40ms`, P99=`21.62ms`
- **DuckDB Prepared**: P50=`13.32ms`, Avg=`13.36ms`, P99=`17.25ms`

### Q6: Degree Centrality Aggregation

> High fan-out relationship scan with GROUP BY and ORDER BY

**Cypher Query (`cypher-sql-go`):**
```cypher
MATCH (n:Service)-[r:CALLS]->() RETURN n.name, count(r) AS degree ORDER BY degree DESC LIMIT 10
```

**SQL/PGQ Query (`DuckDB + DuckPGQ`):**
```sql
SELECT s_name, count(*) AS degree
FROM GRAPH_TABLE (
  microservices
  MATCH (s:Service)-[c:CALLS_SERVICE]->(target:Service)
  COLUMNS (s.name AS s_name)
)
GROUP BY s_name
ORDER BY degree DESC
LIMIT 10;
```

- **Row Count Consistency**: SQLite returned `10` rows, DuckPGQ returned `10` rows.
- **SQLite End-to-End**: P50=`8.23ms`, Avg=`8.37ms`, P99=`12.44ms`
- **DuckDB Ad-Hoc**: P50=`3.53ms`, Avg=`3.55ms`, P99=`6.55ms`
- **DuckDB Prepared**: P50=`2.58ms`, Avg=`2.77ms`, P99=`6.09ms`

### Q7: 2-Tier Hierarchy (S->C->M)

> Targeted 2-hop hierarchy traversal: Service->Class->Method

**Cypher Query (`cypher-sql-go`):**
```cypher
MATCH (s:Service {name: 'service_50'})-[:CONTAINS]->(c:Class)-[:CONTAINS]->(m:Method) RETURN c.name, count(m) AS method_count ORDER BY method_count DESC LIMIT 10
```

**SQL/PGQ Query (`DuckDB + DuckPGQ`):**
```sql
SELECT c_name, count(m_name) AS method_count
FROM GRAPH_TABLE (
  microservices
  MATCH (s:Service WHERE s.name = 'service_50')-[r1:CONTAINS_SC]->(c:Class)-[r2:CONTAINS_CM]->(m:Method)
  COLUMNS (c.name AS c_name, m.name AS m_name)
)
GROUP BY c_name
ORDER BY method_count DESC
LIMIT 10;
```

- **Row Count Consistency**: SQLite returned `10` rows, DuckPGQ returned `10` rows.
- **SQLite End-to-End**: P50=`0.50ms`, Avg=`0.47ms`, P99=`1.53ms`
- **DuckDB Ad-Hoc**: P50=`15.47ms`, Avg=`16.28ms`, P99=`26.53ms`
- **DuckDB Prepared**: P50=`13.71ms`, Avg=`13.60ms`, P99=`15.58ms`

## 4. Overall Benchmark Score & Summary

**Geometric Mean Composite Score (Baseline DuckDB = 100.0%)**: `363.8%`

> `cypher-sql-go` + SQLite is **3.64x faster** on average across the end-to-end workload.

### Key Architectural Observations

1. **Transpilation Overhead**: `cypher-sql-go` compiles Cypher into optimized SQL in **sub-millisecond time (~20-100µs)**, introducing virtually undetectable latency overhead.
2. **OLTP vs OLAP Architecture**:
   - **SQLite + B-Tree Indexes**: Excels at point lookups (Q1), indexed traversals, and low-latency transactional graph queries.
   - **DuckDB + Vectorized Columnar Engine**: Highly efficient at parallel bulk scans and aggregations across large datasets.
3. **Zero-CGO Pure Go Deployment**: `cypher-sql-go` with `modernc.org/sqlite` requires **no external C-compiler, DLLs, or shared libraries**, running anywhere Go compiles with zero external friction.
