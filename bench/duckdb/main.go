//go:build bench

package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	cyphersql "github.com/vmikhailov/cypher-sql-go"
	_ "modernc.org/sqlite"
)

func resolveDataDir(userDir string) string {
	candidates := []string{userDir, "bench/ladybug/data", "../ladybug/data", "data", "bench_data"}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return "bench/ladybug/data"
}

func ensureSqliteDb(userPath, dataDir string) (string, error) {
	candidates := []string{
		userPath,
		"C:/Work/Personal/bench_memgraph_sqlite/bench_graph_100k.db",
		filepath.Join(dataDir, "bench_graph_100k.db"),
		"bench/ladybug/data/bench_graph_100k.db",
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	target := filepath.Join(dataDir, "bench_graph_100k.db")
	fmt.Printf("--> SQLite database not found, generating at %s...\n", target)
	ingestRes, err := benchmarkSqliteIngest(dataDir)
	if err != nil {
		return "", fmt.Errorf("generate sqlite db: %w", err)
	}
	fmt.Printf("--> Generated SQLite database in %.2fs (%.2f MB)\n", ingestRes.TotalTime.Seconds(), ingestRes.DbSizeMb)
	return target, nil
}

func main() {
	var (
		iterations   int
		warmupRuns   int
		sqliteDbPath string
		duckDbPath   string
		dataDir      string
		binDir       string
		forceIngest  bool
		benchIngest  bool
		reportPath   string
	)

	flag.IntVar(&iterations, "iterations", 50, "Number of timed benchmark iterations per query")
	flag.IntVar(&warmupRuns, "warmup", 5, "Number of warmup iterations per query")
	flag.StringVar(&sqliteDbPath, "sqlite", "C:/Work/Personal/bench_memgraph_sqlite/bench_graph_100k.db", "Path to SQLite 100k database")
	flag.StringVar(&duckDbPath, "duck", "bench/duckdb/duck_100k.duckdb", "Path to DuckDB database file")
	flag.StringVar(&dataDir, "data", "bench/ladybug/data", "Path to benchmark CSV data directory")
	flag.StringVar(&binDir, "bin", "bin", "Directory containing DuckDB binaries and DLLs")
	flag.BoolVar(&forceIngest, "reingest", false, "Force re-ingestion of DuckDB dataset")
	flag.BoolVar(&benchIngest, "bench-ingest", true, "Run bulk ingestion benchmark comparing SQLite vs DuckDB")
	flag.StringVar(&reportPath, "report", "bench/duckdb/README.md", "Output Markdown report path")
	flag.Parse()

	dataDir = resolveDataDir(dataDir)

	fmt.Println("==========================================================================")
	fmt.Println("  BENCHMARK: cypher-sql-go (SQLite) vs DuckDB + DuckPGQ (Embedded C-ABI)")
	fmt.Println("==========================================================================")
	fmt.Printf("Platform: Go %s on %s/%s (%d CPUs)\n", runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	fmt.Printf("Dataset: 100,000 Nodes, 198,000 Edges (298,000 Graph Entities)\n")
	fmt.Printf("Iterations: %d timed runs (Warmup: %d runs)\n\n", iterations, warmupRuns)

	// Initialize Embedded DuckDB Driver
	driver, err := NewDuckDriver(binDir)
	if err != nil {
		log.Fatalf("Failed to initialize DuckDB driver: %v", err)
	}

	// =========================================================================
	// PHASE 1: BULK INGESTION BENCHMARK
	// =========================================================================
	var sqliteIngest, duckIngest IngestResult
	if benchIngest {
		fmt.Println("--------------------------------------------------------------------------")
		fmt.Println("  PHASE 1: BULK INGESTION & STORAGE BENCHMARK (100K NODES + 198K EDGES)")
		fmt.Println("--------------------------------------------------------------------------")

		fmt.Print("--> Benchmarking SQLite Ingestion (Hybrid SQL Batch + Indexing)... ")
		sqliteIngest, err = benchmarkSqliteIngest(dataDir)
		if err != nil {
			log.Fatalf("SQLite ingest benchmark failed: %v", err)
		}
		fmt.Printf("DONE in %.2f s (%.0f entities/sec) | DB Size: %.2f MB\n",
			sqliteIngest.TotalTime.Seconds(), sqliteIngest.TotalThroughput, sqliteIngest.DbSizeMb)

		fmt.Print("--> Benchmarking DuckDB Ingestion (read_csv_auto + Property Graph)... ")
		duckIngest, err = benchmarkDuckIngest(driver, dataDir)
		if err != nil {
			log.Fatalf("DuckDB ingest benchmark failed: %v", err)
		}
		fmt.Printf("DONE in %.2f s (%.0f entities/sec) | DB Size: %.2f MB\n\n",
			duckIngest.TotalTime.Seconds(), duckIngest.TotalThroughput, duckIngest.DbSizeMb)

		duckIngest.Score = 100.0
		sqliteIngest.Score = (float64(duckIngest.TotalTime) / float64(sqliteIngest.TotalTime)) * 100.0
	}

	// Ensure Persistent DuckDB Exists for Phase 2
	if forceIngest {
		os.Remove(duckDbPath)
	}
	if _, err := os.Stat(duckDbPath); os.IsNotExist(err) {
		fmt.Printf("Initializing persistent DuckDB database at %s...\n", duckDbPath)
		t0 := time.Now()
		setupPersistentDuckDB(driver, duckDbPath, dataDir)
		fmt.Printf("Initialized DuckDB in %v\n\n", time.Since(t0))
	}

	duckDb, err := driver.OpenDatabase(duckDbPath)
	if err != nil {
		log.Fatalf("Failed to open DuckDB: %v", err)
	}
	defer duckDb.Close()

	duckConn, err := duckDb.Connect()
	if err != nil {
		log.Fatalf("Failed to create DuckDB connection: %v", err)
	}
	defer duckConn.Close()

	// Ensure DuckPGQ is loaded in this connection
	loadRes, err := duckConn.Query("LOAD 'duckpgq';")
	if err != nil {
		log.Fatalf("Failed to load duckpgq extension in connection: %v", err)
	}
	loadRes.Close()

	// Ensure SQLite Database Exists for Phase 2
	actualSqlitePath, err := ensureSqliteDb(sqliteDbPath, dataDir)
	if err != nil {
		log.Fatalf("SQLite DB preparation failed: %v", err)
	}

	sqliteConnStr := fmt.Sprintf("file:%s?mode=ro&cache=shared", actualSqlitePath)
	sqliteDb, err := sql.Open("sqlite", sqliteConnStr)
	if err != nil {
		log.Fatalf("Failed to open SQLite: %v", err)
	}
	defer sqliteDb.Close()

	if _, err := sqliteDb.Exec("PRAGMA cache_size = -64000; PRAGMA mmap_size = 268435456; PRAGMA temp_store = MEMORY;"); err != nil {
		log.Printf("Warning: failed to set pragmas: %v", err)
	}

	queries := getBenchmarkQueries()
	results := make([]BenchmarkQueryResult, 0, len(queries))

	fmt.Println("--------------------------------------------------------------------------")
	fmt.Printf("  PHASE 2: QUERY BENCHMARK (%d TIMED ITERATIONS EACH)\n", iterations)
	fmt.Println("--------------------------------------------------------------------------")

	for _, q := range queries {
		fmt.Printf("\n=== %s: %s ===\n", q.ID, q.Name)
		fmt.Printf("Description: %s\n", q.Description)
		fmt.Printf("Cypher: %s\n", q.Cypher)

		// 1. Measure Cypher Compilation
		compileIters := 500
		for i := 0; i < 50; i++ {
			cyphersql.Compile(q.Cypher)
		}
		var m1, m2 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m1)
		t0 := time.Now()
		var compiled *cyphersql.CompiledQuery
		for i := 0; i < compileIters; i++ {
			compiled, err = cyphersql.Compile(q.Cypher)
			if err != nil {
				log.Fatalf("Compilation failed for %s: %v", q.ID, err)
			}
		}
		compileDuration := time.Since(t0) / time.Duration(compileIters)
		runtime.ReadMemStats(&m2)
		allocsOp := (m2.Mallocs - m1.Mallocs) / uint64(compileIters)
		bytesOp := (m2.TotalAlloc - m1.TotalAlloc) / uint64(compileIters)
		compileUs := float64(compileDuration.Nanoseconds()) / 1000.0

		// Prepare SQLite statement
		sqliteStmt, err := sqliteDb.Prepare(compiled.SQL)
		if err != nil {
			log.Fatalf("SQLite prepare failed for %s: %v\nSQL:\n%s", q.ID, err, compiled.SQL)
		}

		// Prepare DuckDB statement
		duckStmt, err := duckConn.Prepare(q.DuckPGQ)
		if err != nil {
			log.Fatalf("DuckDB prepare failed for %s: %v\nSQL/PGQ:\n%s", q.ID, err, q.DuckPGQ)
		}

		// Warmup runs
		for i := 0; i < warmupRuns; i++ {
			r, err := sqliteStmt.Query()
			if err == nil {
				for r.Next() {
				}
				r.Close()
			}
			qr, err := duckConn.Query(q.DuckPGQ)
			if err == nil {
				qr.Close()
			}
		}

		// Benchmark SQLite Precompiled
		sqlitePrecompiledDurations := make([]time.Duration, iterations)
		sqlRows := 0
		for i := 0; i < iterations; i++ {
			tStart := time.Now()
			rows, err := sqliteStmt.Query()
			if err != nil {
				log.Fatalf("SQLite query error: %v", err)
			}
			rc := 0
			for rows.Next() {
				rc++
			}
			rows.Close()
			sqlitePrecompiledDurations[i] = time.Since(tStart)
			sqlRows = rc
		}

		// Benchmark SQLite End-to-End (Compile + Exec)
		sqliteE2eDurations := make([]time.Duration, iterations)
		for i := 0; i < iterations; i++ {
			tStart := time.Now()
			c, err := cyphersql.Compile(q.Cypher)
			if err != nil {
				log.Fatalf("Compile error: %v", err)
			}
			rows, err := sqliteDb.Query(c.SQL)
			if err != nil {
				log.Fatalf("SQLite query error: %v", err)
			}
			for rows.Next() {
			}
			rows.Close()
			sqliteE2eDurations[i] = time.Since(tStart)
		}

		// Benchmark DuckDB Ad-Hoc
		duckAdHocDurations := make([]time.Duration, iterations)
		duckRows := 0
		for i := 0; i < iterations; i++ {
			tStart := time.Now()
			qr, err := duckConn.Query(q.DuckPGQ)
			if err != nil {
				log.Fatalf("DuckDB query error: %v", err)
			}
			duckRows = qr.RowCount()
			qr.Close()
			duckAdHocDurations[i] = time.Since(tStart)
		}

		// Benchmark DuckDB Prepared
		duckPrepDurations := make([]time.Duration, iterations)
		for i := 0; i < iterations; i++ {
			tStart := time.Now()
			qr, err := duckStmt.Execute()
			if err != nil {
				log.Fatalf("DuckDB exec error: %v", err)
			}
			qr.Close()
			duckPrepDurations[i] = time.Since(tStart)
		}

		sqliteStmt.Close()
		duckStmt.Close()

		sqlStats := calcStats(sqlitePrecompiledDurations)
		sqlE2eStats := calcStats(sqliteE2eDurations)
		duckStats := calcStats(duckAdHocDurations)
		duckPrepStats := calcStats(duckPrepDurations)

		scoreSqlite := (float64(duckStats.Avg) / float64(sqlE2eStats.Avg)) * 100.0

		res := BenchmarkQueryResult{
			Query:             q,
			SqliteRows:        sqlRows,
			DuckRows:          duckRows,
			CompileTimeUs:     compileUs,
			CompileBytesOp:    bytesOp,
			CompileAllocsOp:   allocsOp,
			SqlitePrecompiled: sqlStats,
			SqliteEndToEnd:    sqlE2eStats,
			DuckAdHoc:         duckStats,
			DuckPrepared:      duckPrepStats,
			ScoreSqlite:       scoreSqlite,
			ScoreDuck:         100.0,
		}
		results = append(results, res)

		winner := speedWinner(toMs(res.DuckAdHoc.Avg), toMs(res.SqliteEndToEnd.Avg))
		speed := speedRatio(toMs(res.DuckAdHoc.Avg), toMs(res.SqliteEndToEnd.Avg))
		fmt.Printf("--> SQLite E2E Avg: %.2f ms | DuckDB Ad-Hoc Avg: %.2f ms | Winner: %s (%.1fx) | Rows: SQL=%d, Duck=%d\n",
			toMs(res.SqliteEndToEnd.Avg), toMs(res.DuckAdHoc.Avg), winner, speed, sqlRows, duckRows)
	}

	// Calculate Composite Geometric Mean Score
	scores := make([]float64, len(results))
	for i, r := range results {
		scores[i] = r.ScoreSqlite
	}
	compositeScore := geometricMean(scores)

	printConsoleReport(sqliteIngest, duckIngest, results, compositeScore)

	if reportPath != "" {
		if err := generateMarkdownReport(reportPath, sqliteIngest, duckIngest, results, compositeScore); err != nil {
			log.Fatalf("Failed to write Markdown report: %v", err)
		}
		fmt.Printf("Report saved to %s\n", reportPath)
	}
}
