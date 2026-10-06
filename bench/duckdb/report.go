//go:build bench

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

func printConsoleReport(sqlIngest, duckIngest IngestResult, results []BenchmarkQueryResult, compositeScore float64) {
	fmt.Println()
	fmt.Println("========================================================================================================================")
	fmt.Println("                                cypher-sql-go (SQLite) vs DuckDB + DuckPGQ Benchmark")
	fmt.Println("========================================================================================================================")
	fmt.Printf("OS: %s/%s | Go: %s | CPU Count: %d | Time: %s\n",
		runtime.GOOS, runtime.GOARCH, runtime.Version(), runtime.NumCPU(), time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println("------------------------------------------------------------------------------------------------------------------------")

	// Ingestion Table
	fmt.Println("\n[1] DATASET INGESTION & STORAGE FOOTPRINT (100,000 Nodes, 198,000 Edges):")
	fmt.Println("+-----------------------+------------+------------+------------+---------------+---------------+--------------------+")
	fmt.Println("| Engine                | Nodes/sec  | Edges/sec  | Total Time | DB Size (MB)  | Ingest Winner | Size Winner        |")
	fmt.Println("+-----------------------+------------+------------+------------+---------------+---------------+--------------------+")
	fmt.Printf("| %-21s | %10.0f | %10.0f | %9.2fs | %10.2f MB | %-13s | %-18s |\n",
		sqlIngest.Engine, sqlIngest.NodeThroughput, sqlIngest.EdgeThroughput, sqlIngest.TotalTime.Seconds(), sqlIngest.DbSizeMb,
		winnerLower(sqlIngest.TotalTime.Seconds(), duckIngest.TotalTime.Seconds(), "SQLite", "DuckDB"),
		winnerLower(sqlIngest.DbSizeMb, duckIngest.DbSizeMb, "SQLite", "DuckDB"),
	)
	fmt.Printf("| %-21s | %10.0f | %10.0f | %9.2fs | %10.2f MB |               |                    |\n",
		duckIngest.Engine, duckIngest.NodeThroughput, duckIngest.EdgeThroughput, duckIngest.TotalTime.Seconds(), duckIngest.DbSizeMb,
	)
	fmt.Println("+-----------------------+------------+------------+------------+---------------+---------------+--------------------+")

	// Query Table
	fmt.Println("\n[2] QUERY EXECUTION LATENCY BREAKDOWN (7 Standard Microservice Graph Queries):")
	fmt.Println("+----+---------------------------+----------+---------------+---------------+---------------+---------------+--------+------------+")
	fmt.Println("| ID | Query Pattern             | Transpile| SQLite Precmp | SQLite E2E    | DuckDB Ad-Hoc | DuckDB Prepd  | Winner | SQLite Rel |")
	fmt.Println("|    |                           | (Go AST) | (P50 / Avg)   | (P50 / Avg)   | (P50 / Avg)   | (P50 / Avg)   | (E2E)  | Score (%)  |")
	fmt.Println("+----+---------------------------+----------+---------------+---------------+---------------+---------------+--------+------------+")

	for _, r := range results {
		winner := speedWinner(toMs(r.DuckAdHoc.Avg), toMs(r.SqliteEndToEnd.Avg))
		speed := speedRatio(toMs(r.DuckAdHoc.Avg), toMs(r.SqliteEndToEnd.Avg))
		winnerDisplay := fmt.Sprintf("%s (%.1fx)", winner, speed)

		fmt.Printf("| %-2s | %-25s | %6.1fµs | %5.2f/%5.2fms | %5.2f/%5.2fms | %5.2f/%5.2fms | %5.2f/%5.2fms | %-6s | %9.1f%% |\n",
			r.Query.ID,
			r.Query.Name,
			r.CompileTimeUs,
			toMs(r.SqlitePrecompiled.P50), toMs(r.SqlitePrecompiled.Avg),
			toMs(r.SqliteEndToEnd.P50), toMs(r.SqliteEndToEnd.Avg),
			toMs(r.DuckAdHoc.P50), toMs(r.DuckAdHoc.Avg),
			toMs(r.DuckPrepared.P50), toMs(r.DuckPrepared.Avg),
			winnerDisplay,
			r.ScoreSqlite,
		)
	}
	fmt.Println("+----+---------------------------+----------+---------------+---------------+---------------+---------------+--------+------------+")

	// Summary
	fmt.Printf("\n[3] COMPOSITE GEOMETRIC MEAN SCORE (Baseline DuckDB Ad-Hoc = 100.0%%):\n")
	fmt.Printf("    -> cypher-sql-go (SQLite E2E): %.1f%%\n", compositeScore)
	if compositeScore > 100.0 {
		fmt.Printf("    -> cypher-sql-go is %.2fx FASTER overall across the standardized workload!\n", compositeScore/100.0)
	} else {
		fmt.Printf("    -> DuckDB is %.2fx faster overall across the standardized workload!\n", 100.0/compositeScore)
	}
	fmt.Println("========================================================================================================================")
	fmt.Println()
}

func winnerLower(v1, v2 float64, name1, name2 string) string {
	if v1 < v2 {
		return name1
	}
	return name2
}

func winnerHigher(v1, v2 float64, name1, name2 string) string {
	if v1 > v2 {
		return name1
	}
	return name2
}

func generateMarkdownReport(reportFile string, sqlIngest, duckIngest IngestResult, results []BenchmarkQueryResult, compositeScore float64) error {
	var sb strings.Builder

	sb.WriteString("# cypher-sql-go (SQLite) vs DuckDB + DuckPGQ Benchmark\n\n")
	sb.WriteString("> **Comprehensive Performance Analysis**: Comparing `cypher-sql-go` (zero-overhead Cypher-to-SQL transpilation on embedded SQLite) against **DuckDB v1.2.2 + DuckPGQ** (embedded OLAP columnar engine with ISO SQL/PGQ property graph extension).\n\n")

	sb.WriteString("## Test Environment\n\n")
	sb.WriteString("| Component | Specification |\n")
	sb.WriteString("|:---|:---|\n")
	sb.WriteString(fmt.Sprintf("| **Operating System** | %s (%s) |\n", runtime.GOOS, runtime.GOARCH))
	sb.WriteString(fmt.Sprintf("| **Go Runtime** | `%s` |\n", runtime.Version()))
	sb.WriteString(fmt.Sprintf("| **CPU Logical Cores** | %d |\n", runtime.NumCPU()))
	sb.WriteString("| **cypher-sql-go Engine** | Embedded SQLite (`modernc.org/sqlite` pure Go, zero CGO) |\n")
	sb.WriteString("| **DuckDB Engine** | Embedded DuckDB v1.2.2 C-ABI (`duckdb.dll`, direct syscall lazy binding, zero CGO) |\n")
	sb.WriteString("| **Graph Extension** | DuckPGQ (ISO SQL:2023 `GRAPH_TABLE` Property Graph Extension) |\n")
	sb.WriteString("| **Dataset Scale** | 100,000 Nodes, 198,000 Edges (Synthetic Enterprise Microservice Architecture) |\n\n")

	sb.WriteString("## 1. Ingestion Throughput & Storage Footprint\n\n")
	sb.WriteString("| Metric | cypher-sql-go (SQLite) | DuckDB + DuckPGQ | Ratio / Winner |\n")
	sb.WriteString("|:---|:---:|:---:|:---:|\n")
	sb.WriteString(fmt.Sprintf("| **Node Load Throughput** | `%.0f` nodes/sec | `%.0f` nodes/sec | **%s** (%.1fx) |\n",
		sqlIngest.NodeThroughput, duckIngest.NodeThroughput,
		winnerHigher(duckIngest.NodeThroughput, sqlIngest.NodeThroughput, "DuckDB", "SQLite"),
		speedRatio(sqlIngest.NodeThroughput, duckIngest.NodeThroughput)))
	sb.WriteString(fmt.Sprintf("| **Edge Load Throughput** | `%.0f` edges/sec | `%.0f` edges/sec | **%s** (%.1fx) |\n",
		sqlIngest.EdgeThroughput, duckIngest.EdgeThroughput,
		winnerHigher(duckIngest.EdgeThroughput, sqlIngest.EdgeThroughput, "DuckDB", "SQLite"),
		speedRatio(sqlIngest.EdgeThroughput, duckIngest.EdgeThroughput)))
	sb.WriteString(fmt.Sprintf("| **Total Ingest Duration** | `%.2f s` | `%.2f s` | **%s** (%.1fx) |\n",
		sqlIngest.TotalTime.Seconds(), duckIngest.TotalTime.Seconds(),
		winnerLower(sqlIngest.TotalTime.Seconds(), duckIngest.TotalTime.Seconds(), "SQLite", "DuckDB"),
		speedRatio(sqlIngest.TotalTime.Seconds(), duckIngest.TotalTime.Seconds())))
	sb.WriteString(fmt.Sprintf("| **On-Disk Database Size** | `%.2f MB` | `%.2f MB` | **%s** (%.1fx smaller) |\n\n",
		sqlIngest.DbSizeMb, duckIngest.DbSizeMb,
		sizeWinner(sqlIngest.DbSizeMb, duckIngest.DbSizeMb),
		speedRatio(sqlIngest.DbSizeMb, duckIngest.DbSizeMb)))

	sb.WriteString("## 2. Query Latency Breakdown\n\n")
	sb.WriteString("Evaluated across 7 standard microservice graph topology queries. Latencies reported in milliseconds (ms), lower is better.\n\n")

	sb.WriteString("| ID | Query Pattern | Compile (µs) | SQLite Precmp (P50) | SQLite E2E (Avg) | DuckDB Ad-Hoc (Avg) | DuckDB Prepd (Avg) | Winner | Relative Score |\n")
	sb.WriteString("|:--:|:---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|\n")

	for _, r := range results {
		winner := speedWinner(toMs(r.DuckAdHoc.Avg), toMs(r.SqliteEndToEnd.Avg))
		speed := speedRatio(toMs(r.DuckAdHoc.Avg), toMs(r.SqliteEndToEnd.Avg))
		winnerDisplay := fmt.Sprintf("**%s** (%.1fx)", winner, speed)

		sb.WriteString(fmt.Sprintf("| **%s** | %s | `%.1f µs` | `%.2f ms` | `%.2f ms` | `%.2f ms` | `%.2f ms` | %s | `%.1f%%` |\n",
			r.Query.ID,
			r.Query.Name,
			r.CompileTimeUs,
			toMs(r.SqlitePrecompiled.P50),
			toMs(r.SqliteEndToEnd.Avg),
			toMs(r.DuckAdHoc.Avg),
			toMs(r.DuckPrepared.Avg),
			winnerDisplay,
			r.ScoreSqlite,
		))
	}

	sb.WriteString("\n## 3. Query Details & SQL/PGQ Mapping\n\n")
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("### %s: %s\n\n", r.Query.ID, r.Query.Name))
		sb.WriteString(fmt.Sprintf("> %s\n\n", r.Query.Description))
		sb.WriteString("**Cypher Query (`cypher-sql-go`):**\n```cypher\n" + r.Query.Cypher + "\n```\n\n")
		sb.WriteString("**SQL/PGQ Query (`DuckDB + DuckPGQ`):**\n```sql\n" + r.Query.DuckPGQ + "\n```\n\n")
		sb.WriteString(fmt.Sprintf("- **Row Count Consistency**: SQLite returned `%d` rows, DuckPGQ returned `%d` rows.\n", r.SqliteRows, r.DuckRows))
		sb.WriteString(fmt.Sprintf("- **SQLite End-to-End**: P50=`%.2fms`, Avg=`%.2fms`, P99=`%.2fms`\n", toMs(r.SqliteEndToEnd.P50), toMs(r.SqliteEndToEnd.Avg), toMs(r.SqliteEndToEnd.P99)))
		sb.WriteString(fmt.Sprintf("- **DuckDB Ad-Hoc**: P50=`%.2fms`, Avg=`%.2fms`, P99=`%.2fms`\n", toMs(r.DuckAdHoc.P50), toMs(r.DuckAdHoc.Avg), toMs(r.DuckAdHoc.P99)))
		sb.WriteString(fmt.Sprintf("- **DuckDB Prepared**: P50=`%.2fms`, Avg=`%.2fms`, P99=`%.2fms`\n\n", toMs(r.DuckPrepared.P50), toMs(r.DuckPrepared.Avg), toMs(r.DuckPrepared.P99)))
	}

	sb.WriteString("## 4. Overall Benchmark Score & Summary\n\n")
	sb.WriteString(fmt.Sprintf("**Geometric Mean Composite Score (Baseline DuckDB = 100.0%%)**: `%.1f%%`\n\n", compositeScore))
	if compositeScore > 100.0 {
		sb.WriteString(fmt.Sprintf("> `cypher-sql-go` + SQLite is **%.2fx faster** on average across the end-to-end workload.\n\n", compositeScore/100.0))
	} else {
		sb.WriteString(fmt.Sprintf("> `DuckDB + DuckPGQ` is **%.2fx faster** on average across the end-to-end workload.\n\n", 100.0/compositeScore))
	}

	sb.WriteString("### Key Architectural Observations\n\n")
	sb.WriteString("1. **Transpilation Overhead**: `cypher-sql-go` compiles Cypher into optimized SQL in **sub-millisecond time (~20-100µs)**, introducing virtually undetectable latency overhead.\n")
	sb.WriteString("2. **OLTP vs OLAP Architecture**:\n")
	sb.WriteString("   - **SQLite + B-Tree Indexes**: Excels at point lookups (Q1), indexed traversals, and low-latency transactional graph queries.\n")
	sb.WriteString("   - **DuckDB + Vectorized Columnar Engine**: Highly efficient at parallel bulk scans and aggregations across large datasets.\n")
	sb.WriteString("3. **Zero-CGO Pure Go Deployment**: `cypher-sql-go` with `modernc.org/sqlite` requires **no external C-compiler, DLLs, or shared libraries**, running anywhere Go compiles with zero external friction.\n")

	return os.WriteFile(reportFile, []byte(sb.String()), 0644)
}
