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

	"github.com/vmikhailov/cypher-sql-go"
	_ "modernc.org/sqlite"
)

func resolveDataDir(userDir string) string {
	if userDir != "" && userDir != "bench/ladybug/data" && userDir != "bench_data" && userDir != "data" {
		return userDir
	}
	candidates := []string{userDir, "bench/ladybug/data", "data", "bench_data"}
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

func resolveLbugPath(userPath string, dataDir string) string {
	if userPath != "" && userPath != "bench/ladybug/data/ladybug_100k.lbug" && userPath != "bench_data/ladybug_100k.lbug" {
		return userPath
	}
	candidates := []string{
		filepath.Join(dataDir, "ladybug_100k.lbug"),
		"bench/ladybug/data/ladybug_100k.lbug",
		"bench_data/ladybug_100k.lbug",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return filepath.Join(dataDir, "ladybug_100k.lbug")
}

func main() {
	var (
		iterations   int
		warmupRuns   int
		sqliteDbPath string
		lbugDbPath   string
		dataDir      string
		binDir       string
		forceIngest  bool
		benchIngest  bool
		reportPath   string
	)

	flag.IntVar(&iterations, "iterations", 100, "Number of timed benchmark iterations")
	flag.IntVar(&warmupRuns, "warmup", 10, "Number of warmup iterations")
	flag.StringVar(&sqliteDbPath, "sqlite", "C:/Work/Personal/bench_memgraph_sqlite/bench_graph_100k.db", "Path to SQLite 100k dataset")
	flag.StringVar(&lbugDbPath, "lbug", "bench/ladybug/data/ladybug_100k.lbug", "Path to LadybugDB database directory")
	flag.StringVar(&dataDir, "data", "bench/ladybug/data", "Path to CSV export directory")
	flag.StringVar(&binDir, "bin", "bin", "Directory containing LadybugDB binaries and DLLs")
	flag.BoolVar(&forceIngest, "reingest", false, "Force re-ingestion of LadybugDB dataset")
	flag.BoolVar(&benchIngest, "bench-ingest", true, "Run bulk ingestion benchmark comparing SQLite SQL vs LadybugDB")
	flag.StringVar(&reportPath, "report", "ladybug_perf_comparison.md", "Output markdown report path")
	flag.Parse()

	dataDir = resolveDataDir(dataDir)
	lbugDbPath = resolveLbugPath(lbugDbPath, dataDir)

	fmt.Println("==========================================================================")
	fmt.Println("  PERFORMANCE BENCHMARK: cypher-sql-go (SQLite) vs LadybugDB (Native C++)")
	fmt.Println("==========================================================================")
	fmt.Printf("Platform: Go %s on %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Dataset: 100,000 Nodes, 198,000 Relationships (298,000 Graph Entities)\n")
	fmt.Printf("Iterations: %d (Warmup: %d)\n\n", iterations, warmupRuns)

	// Initialize Ladybug Driver
	driver, err := NewLadybugDriver(binDir)
	if err != nil {
		log.Fatalf("Failed to initialize Ladybug driver: %v", err)
	}

	// =========================================================================
	// PHASE 1: BULK INGESTION BENCHMARK
	// =========================================================================
	var sqliteIngest, lbugIngest IngestResult
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

		fmt.Print("--> Benchmarking LadybugDB Ingestion (DDL + Native CSV Copy)... ")
		lbugIngest, err = benchmarkLadybugIngest(driver, dataDir)
		if err != nil {
			log.Fatalf("LadybugDB ingest benchmark failed: %v", err)
		}
		fmt.Printf("DONE in %.2f s (%.0f entities/sec) | DB Size: %.2f MB\n\n",
			lbugIngest.TotalTime.Seconds(), lbugIngest.TotalThroughput, lbugIngest.DbSizeMb)

		// Set scores for ingestion
		lbugIngest.Score = 100.0
		sqliteIngest.Score = (float64(lbugIngest.TotalTime) / float64(sqliteIngest.TotalTime)) * 100.0

		printIngestionConsoleReport(sqliteIngest, lbugIngest)
	}

	// Ensure persistent LadybugDB database exists for Phase 2
	if forceIngest {
		os.RemoveAll(lbugDbPath)
	}
	if _, err := os.Stat(lbugDbPath); os.IsNotExist(err) {
		fmt.Printf("Initializing persistent LadybugDB at %s...\n", lbugDbPath)
		t0 := time.Now()
		setupPersistentLadybug(driver, lbugDbPath, dataDir)
		fmt.Printf("Initialized LadybugDB in %v\n\n", time.Since(t0))
	}

	lbugDb, err := driver.OpenDatabase(lbugDbPath, 1024*1024*1024)
	if err != nil {
		log.Fatalf("Failed to open LadybugDB: %v", err)
	}
	defer lbugDb.Close()

	lbugConn, err := lbugDb.Connect()
	if err != nil {
		log.Fatalf("Failed to create LadybugDB connection: %v", err)
	}
	defer lbugConn.Close()

	// Open SQLite Database for Phase 2
	sqliteConnStr := fmt.Sprintf("file:%s?mode=ro&cache=shared", sqliteDbPath)
	sqliteDb, err := sql.Open("sqlite", sqliteConnStr)
	if err != nil {
		log.Fatalf("Failed to open SQLite: %v", err)
	}
	defer sqliteDb.Close()

	// Apply optimized query execution pragmas
	if _, err := sqliteDb.Exec("PRAGMA cache_size = -64000; PRAGMA mmap_size = 268435456; PRAGMA temp_store = MEMORY;"); err != nil {
		log.Printf("Warning: failed to set pragmas: %v", err)
	}

	queries := getBenchmarkQueries()
	results := make([]BenchmarkQueryResult, 0, len(queries))

	fmt.Println("--------------------------------------------------------------------------")
	fmt.Printf("  PHASE 2: CYPHER QUERY BENCHMARK (%d WARMED ITERATIONS EACH)\n", iterations)
	fmt.Println("--------------------------------------------------------------------------")

	for _, q := range queries {
		fmt.Printf("\n=== %s: %s ===\n", q.ID, q.Name)
		fmt.Printf("Description: %s\n", q.Description)
		fmt.Printf("Cypher: %s\n", q.Cypher)

		// Measure Compilation of cypher-sql-go
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

		// Prepare SQLite Stmt
		sqliteStmt, err := sqliteDb.Prepare(compiled.SQL)
		if err != nil {
			log.Fatalf("SQLite prepare failed for %s: %v\nSQL:\n%s", q.ID, err, compiled.SQL)
		}

		// Prepare Ladybug Stmt
		lbugStmt, err := lbugConn.Prepare(q.Cypher)
		if err != nil {
			log.Fatalf("Ladybug prepare failed for %s: %v", q.ID, err)
		}

		// Warmup
		for i := 0; i < warmupRuns; i++ {
			r, err := sqliteStmt.Query()
			if err == nil {
				for r.Next() {
				}
				r.Close()
			}
			qr, err := lbugConn.Query(q.Cypher)
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

		// Benchmark Ladybug Ad-hoc
		lbugAdHocDurations := make([]time.Duration, iterations)
		lbugRows := 0
		for i := 0; i < iterations; i++ {
			tStart := time.Now()
			qr, err := lbugConn.Query(q.Cypher)
			if err != nil {
				log.Fatalf("Ladybug query error: %v", err)
			}
			if !qr.IsSuccess() {
				log.Fatalf("Ladybug query failed: %s", qr.ErrorMessage())
			}
			lbugRows = qr.NumTuples()
			qr.Close()
			lbugAdHocDurations[i] = time.Since(tStart)
		}

		// Benchmark Ladybug Prepared
		lbugPrepDurations := make([]time.Duration, iterations)
		for i := 0; i < iterations; i++ {
			tStart := time.Now()
			qr, err := lbugStmt.Execute(lbugConn)
			if err != nil {
				log.Fatalf("Ladybug exec error: %v", err)
			}
			if !qr.IsSuccess() {
				log.Fatalf("Ladybug exec failed: %s", qr.ErrorMessage())
			}
			qr.Close()
			lbugPrepDurations[i] = time.Since(tStart)
		}

		sqliteStmt.Close()
		lbugStmt.Close()

		sqlStats := calcStats(sqlitePrecompiledDurations)
		sqlE2eStats := calcStats(sqliteE2eDurations)
		lbugStats := calcStats(lbugAdHocDurations)
		lbugPrepStats := calcStats(lbugPrepDurations)

		parity := "✓ MATCH"
		if sqlRows != lbugRows {
			parity = fmt.Sprintf("MISMATCH (SQLite: %d, Ladybug: %d)", sqlRows, lbugRows)
		}

		scoreSqlite := (float64(lbugStats.Avg) / float64(sqlE2eStats.Avg)) * 100.0

		res := BenchmarkQueryResult{
			Query:             q,
			SqliteRows:        sqlRows,
			LadybugRows:       lbugRows,
			CompileTimeUs:     compileUs,
			CompileBytesOp:    bytesOp,
			CompileAllocsOp:   allocsOp,
			SqlitePrecompiled: sqlStats,
			SqliteEndToEnd:    sqlE2eStats,
			LadybugAdHoc:      lbugStats,
			LadybugPrepared:   lbugPrepStats,
			ScoreSqlite:       scoreSqlite,
			ScoreLadybug:      100.0,
		}
		results = append(results, res)

		printConsoleResult(res, parity)
	}

	printSummaryReport(results, sqliteIngest, lbugIngest, benchIngest)
	if reportPath != "" {
		writeMarkdownReport(reportPath, sqliteIngest, lbugIngest, results, iterations, benchIngest)
		fmt.Printf("\nSaved detailed Markdown report to: %s\n", reportPath)
	}
}
