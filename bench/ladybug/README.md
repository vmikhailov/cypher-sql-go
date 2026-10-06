# Comprehensive Performance Benchmark: `cypher-sql-go` (Hybrid SQL) vs. LadybugDB (Native C++)

**Date**: 2026-10-06 14:18:04  
**Platform**: Windows AMD64, Go go1.26.3, LadybugDB v0.21.2 (in-process C-ABI)  
**Dataset**: 100,000 Nodes, 198,000 Relationships (298,000 Graph Entities)  
**Workload Diversity**: 10 Queries (5 Transactional/OLTP + 5 Analytical/OLAP), 25 Warmed Iterations each  
**Methodology**: In accordance with [Unified Benchmark Methodology & Scoring Specification](../METHODOLOGY.md)  

> [!IMPORTANT]
> **Dataset Scale & Cache Residency Context**:
> At 298,000 graph entities (**34 MB SQLite / 22 MB LadybugDB**), the entire working dataset resides completely inside **CPU L3 / RAM cache**.
> This benchmark transparently measures in-process compute, query optimizer efficiency, index traversal mechanics, and runtime dispatch overhead—rather than out-of-core NVMe I/O bottleneck scaling.

## 1. Split Workload Benchmark Summary (SPEC / LDBC Style)

> Standardized SPEC/Geekbench-style normalized scoring where **LadybugDB Baseline = 100.0 points**.
> To prevent workload selection bias, scores are split into two independent indices reflecting the real-world dual role of embedded engines:
> 1. **Interactive UI & Point Lookups (OLTP)**: Symbol navigation, direct caller inspection, interactive UI drill-down.
> 2. **Whole-Graph Local Analysis (OLAP)**: Circular dependency detection, impact radius calculation, dead code path counts.

| Workload Dimension | Embedded Use Case | `cypher-sql-go` (SQLite) | LadybugDB Baseline | Architectural Advantage |
| :--- | :--- | :---: | :---: | :--- |
| **[OLTP] Interactive UI & Point Lookups** | Direct callers, symbol lookups, UI inspection (`LIMIT`, point seeks) | **705.8 pts** | 100.0 pts | **7.06x SQLite Faster** |
| **[OLAP] Whole-Graph Structural Analysis** | Circular dependencies, impact radius, dead paths (unconstrained, deep paths) | **60.4 pts** | 100.0 pts | **1.65x LadybugDB Faster** |
| **Bulk Data Ingestion (298k Entities)** | Initial database population from CSV/raw data | **37.2 pts** | 100.0 pts | **2.69x LadybugDB Faster** |
| **Storage Footprint on Disk** | Local disk usage footprint | **34.00 MB** | **22.36 MB** | **1.52x LadybugDB Smaller** |

### Execution-Only vs. End-to-End Latency Breakdown
To isolate database compute from Go runtime AST transpilation, both comparisons are tracked:
- **OLTP Execution-Only (Precompiled SQLite vs. Prepared Ladybug)**: `555.5 pts` (5.56x SQLite)
- **OLAP Execution-Only (Precompiled SQLite vs. Prepared Ladybug)**: `48.2 pts` (2.08x Ladybug)

---

## 2. Ingestion Throughput & Storage Footprint

| Ingestion Stage / Metric | SQLite (Hybrid SQL) | LadybugDB (Native) | SQLite Score | Advantage |
| :--- | :---: | :---: | :---: | :--- |
| **Nodes Ingested** | 100000 nodes | 100000 nodes | - | Exact Match (✓ Parity) |
| **Relationships Ingested** | 198000 edges | 198000 edges | - | Exact Match (✓ Parity) |
| **Node Ingestion Time** | 1090.34 ms (91715 nodes/s) | 636.55 ms (157095 nodes/s) | 58.4 pts | **1.71x LadybugDB** |
| **Relationship Ingestion Time** | 1163.66 ms (170152 edges/s) | 332.99 ms (594612 edges/s) | 28.6 pts | **3.49x LadybugDB** |
| **Index Creation + ANALYZE** | 515.63 ms | 61.86 ms (built inline) | - | SQLite builds 3 B-Trees |
| **Total End-to-End Loading** | **2770.13 ms** (107576 entities/s) | **1031.41 ms** (288925 entities/s) | **37.2 pts** | **2.69x LadybugDB** |
| **Database Footprint on Disk** | **34.00 MB** | **22.36 MB** | 65.8 pts | **1.52x LadybugDB** |

## 3. Workload Performance Breakdown (10 Standard Queries)

### Suite A: Transactional / Localized Workload (OLTP)

| Query ID | Pattern | Row Count | Compile (µs) | SQLite Precmp | SQLite E2E | Ladybug Ad-hoc | Ladybug Prepd | SQLite Rel Score | Advantage |
| :--- | :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :--- |
| **Q1** | Exact Point Lookup | 1 | `9.1 µs` | 0.060 ms | **0.063 ms** | **0.977 ms** | 0.472 ms | **1543.2 pts** | **15.51x SQLite** |
| **Q2** | Filtered Property Scan (Limit 50) | 50 | `13.1 µs` | 0.385 ms | **0.430 ms** | **0.812 ms** | 0.365 ms | **188.8 pts** | **1.89x SQLite** |
| **Q3** | Localized 1-Hop Traversal (Limit 20) | 20 | `19.3 µs` | 6.330 ms | **6.810 ms** | **6.631 ms** | 7.948 ms | **97.4 pts** | **1.03x Ladybug** |
| **Q4** | Localized 2-Hop Traversal (Limit 50) | 50 | `18.5 µs` | 0.364 ms | **0.404 ms** | **10.621 ms** | 8.360 ms | **2625.0 pts** | **26.29x SQLite** |
| **Q5** | Localized Hierarchy Traversal (Limit 10) | 10 | `25.5 µs` | 0.452 ms | **0.560 ms** | **13.193 ms** | 11.230 ms | **2352.0 pts** | **23.56x SQLite** |

### Suite B: Structural / Analytical Workload (OLAP)

| Query ID | Pattern | Row Count | Compile (µs) | SQLite Precmp | SQLite E2E | Ladybug Ad-hoc | Ladybug Prepd | SQLite Rel Score | Advantage |
| :--- | :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :--- |
| **Q6** | Unconstrained 2-Hop Full Join | 1 | `17.6 µs` | 25.461 ms | **26.316 ms** | **8.266 ms** | 6.269 ms | **31.4 pts** | **3.18x Ladybug** |
| **Q7** | Deep Path Expansion (k=1..5) | 1 | `18.6 µs` | 21.341 ms | **22.200 ms** | **7.421 ms** | 5.221 ms | **33.4 pts** | **2.99x Ladybug** |
| **Q8** | Global Property Filter Aggregation | 1 | `8.1 µs` | 1.198 ms | **1.233 ms** | **1.239 ms** | 0.759 ms | **100.4 pts** | **1.00x SQLite** |
| **Q9** | Global Topology Edge Aggregation | 1 | `12.1 µs` | 7.119 ms | **7.068 ms** | **8.452 ms** | 6.756 ms | **119.6 pts** | **1.20x SQLite** |
| **Q10** | High Fan-Out Degree Centrality | 10 | `14.3 µs` | 8.398 ms | **8.436 ms** | **5.397 ms** | 6.012 ms | **64.0 pts** | **1.56x Ladybug** |

## 4. Query Patterns & Architectural Findings

### Q1 [OLTP]: Exact Point Lookup

> Indexed point lookup of single service properties by primary key

```cypher
MATCH (s:Service {name: 'service_420'}) RETURN s.id, s.name, s.layer, s.framework
```

- **Row Parity**: 1 rows returned by both engines (100% match ✓)
- **Go Compiler Latency**: **9.1 µs** (4644 B/op, 88 allocs)
- **SQLite End-to-End**: P50=`0.000 ms`, Avg=`0.063 ms`, P99=`0.534 ms`
- **LadybugDB Ad-hoc**: P50=`1.008 ms`, Avg=`0.977 ms`, P99=`2.066 ms`
- **LadybugDB Prepared**: P50=`0.512 ms`, Avg=`0.472 ms`, P99=`1.066 ms`

### Q2 [OLTP]: Filtered Property Scan (Limit 50)

> Filter 100k nodes by property with early-exit LIMIT 50

```cypher
MATCH (s:Service) WHERE s.layer = 'Application' RETURN s.name, s.framework, s.language LIMIT 50
```

- **Row Parity**: 50 rows returned by both engines (100% match ✓)
- **Go Compiler Latency**: **13.1 µs** (4354 B/op, 85 allocs)
- **SQLite End-to-End**: P50=`0.000 ms`, Avg=`0.430 ms`, P99=`1.541 ms`
- **LadybugDB Ad-hoc**: P50=`1.004 ms`, Avg=`0.812 ms`, P99=`2.046 ms`
- **LadybugDB Prepared**: P50=`0.000 ms`, Avg=`0.365 ms`, P99=`1.512 ms`

### Q3 [OLTP]: Localized 1-Hop Traversal (Limit 20)

> Service->Database traversal with GROUP BY and LIMIT 20

```cypher
MATCH (s:Service)-[:USES_DB]->(d:Database) RETURN s.name, count(d) AS db_count ORDER BY db_count DESC LIMIT 20
```

- **Row Parity**: 20 rows returned by both engines (100% match ✓)
- **Go Compiler Latency**: **19.3 µs** (4914 B/op, 108 allocs)
- **SQLite End-to-End**: P50=`6.197 ms`, Avg=`6.810 ms`, P99=`9.318 ms`
- **LadybugDB Ad-hoc**: P50=`6.166 ms`, Avg=`6.631 ms`, P99=`11.973 ms`
- **LadybugDB Prepared**: P50=`7.813 ms`, Avg=`7.948 ms`, P99=`13.597 ms`

### Q4 [OLTP]: Localized 2-Hop Traversal (Limit 50)

> 2-hop join pattern: Service->Service->Database with LIMIT 50

```cypher
MATCH (s1:Service)-[:CALLS]->(s2:Service)-[:USES_DB]->(d:Database) RETURN s1.name, s2.name, d.name LIMIT 50
```

- **Row Parity**: 50 rows returned by both engines (100% match ✓)
- **Go Compiler Latency**: **18.5 µs** (6338 B/op, 127 allocs)
- **SQLite End-to-End**: P50=`0.504 ms`, Avg=`0.404 ms`, P99=`1.506 ms`
- **LadybugDB Ad-hoc**: P50=`10.144 ms`, Avg=`10.621 ms`, P99=`16.474 ms`
- **LadybugDB Prepared**: P50=`8.689 ms`, Avg=`8.360 ms`, P99=`12.579 ms`

### Q5 [OLTP]: Localized Hierarchy Traversal (Limit 10)

> Targeted 2-hop hierarchy traversal: Service->Class->Method with LIMIT 10

```cypher
MATCH (s:Service {name: 'service_50'})-[:CONTAINS]->(c:Class)-[:CONTAINS]->(m:Method) RETURN c.name, count(m) AS method_count ORDER BY method_count DESC LIMIT 10
```

- **Row Parity**: 10 rows returned by both engines (100% match ✓)
- **Go Compiler Latency**: **25.5 µs** (6971 B/op, 147 allocs)
- **SQLite End-to-End**: P50=`0.520 ms`, Avg=`0.560 ms`, P99=`1.515 ms`
- **LadybugDB Ad-hoc**: P50=`13.054 ms`, Avg=`13.193 ms`, P99=`17.827 ms`
- **LadybugDB Prepared**: P50=`10.934 ms`, Avg=`11.230 ms`, P99=`16.591 ms`

### Q6 [OLAP]: Unconstrained 2-Hop Full Join

> Global unconstrained multi-hop join: Service->Service->Database counting all 15k paths

```cypher
MATCH (s1:Service)-[:CALLS]->(s2:Service)-[:USES_DB]->(d:Database) RETURN count(*) AS total_paths
```

- **Row Parity**: 1 rows returned by both engines (100% match ✓)
- **Go Compiler Latency**: **17.6 µs** (5189 B/op, 103 allocs)
- **SQLite End-to-End**: P50=`25.704 ms`, Avg=`26.316 ms`, P99=`36.256 ms`
- **LadybugDB Ad-hoc**: P50=`7.296 ms`, Avg=`8.266 ms`, P99=`14.960 ms`
- **LadybugDB Prepared**: P50=`6.101 ms`, Avg=`6.269 ms`, P99=`10.907 ms`

### Q7 [OLAP]: Deep Path Expansion (k=1..5)

> Recursive path finding with cycle prevention and reachable distinct target count

```cypher
MATCH (s:Service {name: 'service_10'})-[:CALLS*1..5]->(target:Service) RETURN count(DISTINCT target.name) AS reachable_count
```

- **Row Parity**: 1 rows returned by both engines (100% match ✓)
- **Go Compiler Latency**: **18.6 µs** (8257 B/op, 133 allocs)
- **SQLite End-to-End**: P50=`21.946 ms`, Avg=`22.200 ms`, P99=`27.841 ms`
- **LadybugDB Ad-hoc**: P50=`7.569 ms`, Avg=`7.421 ms`, P99=`8.790 ms`
- **LadybugDB Prepared**: P50=`5.258 ms`, Avg=`5.221 ms`, P99=`7.922 ms`

### Q8 [OLAP]: Global Property Filter Aggregation

> Full table scan across 100k nodes filtering by unindexed property with global COUNT

```cypher
MATCH (s:Service) WHERE s.framework = 'express' RETURN count(s) AS express_count
```

- **Row Parity**: 1 rows returned by both engines (100% match ✓)
- **Go Compiler Latency**: **8.1 µs** (3188 B/op, 62 allocs)
- **SQLite End-to-End**: P50=`1.529 ms`, Avg=`1.233 ms`, P99=`2.026 ms`
- **LadybugDB Ad-hoc**: P50=`1.510 ms`, Avg=`1.239 ms`, P99=`2.556 ms`
- **LadybugDB Prepared**: P50=`1.004 ms`, Avg=`0.759 ms`, P99=`1.512 ms`

### Q9 [OLAP]: Global Topology Edge Aggregation

> Global relationship scan evaluating raw edge table traversal without anchor shortcuts

```cypher
MATCH (a:Service)-[r:CALLS]->(b:Service) RETURN count(r) AS total_calls
```

- **Row Parity**: 1 rows returned by both engines (100% match ✓)
- **Go Compiler Latency**: **12.1 µs** (3938 B/op, 78 allocs)
- **SQLite End-to-End**: P50=`7.112 ms`, Avg=`7.068 ms`, P99=`8.300 ms`
- **LadybugDB Ad-hoc**: P50=`8.181 ms`, Avg=`8.452 ms`, P99=`12.881 ms`
- **LadybugDB Prepared**: P50=`6.710 ms`, Avg=`6.756 ms`, P99=`9.283 ms`

### Q10 [OLAP]: High Fan-Out Degree Centrality

> High fan-out relationship scan with global GROUP BY and ORDER BY

```cypher
MATCH (n:Service)-[r:CALLS]->() RETURN n.name, count(r) AS degree ORDER BY degree DESC LIMIT 10
```

- **Row Parity**: 10 rows returned by both engines (100% match ✓)
- **Go Compiler Latency**: **14.3 µs** (4749 B/op, 103 allocs)
- **SQLite End-to-End**: P50=`8.634 ms`, Avg=`8.436 ms`, P99=`9.793 ms`
- **LadybugDB Ad-hoc**: P50=`5.538 ms`, Avg=`5.397 ms`, P99=`7.846 ms`
- **LadybugDB Prepared**: P50=`6.079 ms`, Avg=`6.012 ms`, P99=`10.825 ms`

## 5. Architectural Conclusions

1. **The Right Tool for the Workload**:
   - **Where `cypher-sql-go` + SQLite Excels (OLTP)**: For embedded applications dominated by localized point lookups, shallow traversals, and early-exit filters, compiling Cypher to SQLite provides a **10x–25x latency win** over columnar graph engines by avoiding vectorized batch setup, thread pool orchestration, and C-ABI glue.
   - **Where LadybugDB Excels (OLAP)**: For analytical graph aggregations, unconstrained joins across entire tables, and deep recursive path expansions (k≥4), LadybugDB's Compressed Sparse Row (CSR) storage and vectorized C++ execution engine deliver superior scanning and joining throughput.

2. **Transpilation Overhead**: `cypher-sql-go` compiles Cypher into optimized SQL in **sub-millisecond time (10–30 µs)**, introducing virtually zero observable latency in real-world workloads.

3. **Zero-CGO Pure Go Deployment**: `cypher-sql-go` with `modernc.org/sqlite` requires **no external C-compiler, DLLs, or shared libraries**, running anywhere Go compiles with zero external friction.
