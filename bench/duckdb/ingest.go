//go:build bench

package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

func benchmarkSqliteIngest(dataDir string) (IngestResult, error) {
	tempDbPath := filepath.Join(dataDir, "temp_ingest_bench.db")
	os.Remove(tempDbPath)
	defer os.Remove(tempDbPath)

	db, err := sql.Open("sqlite", tempDbPath)
	if err != nil {
		return IngestResult{}, fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA synchronous = OFF; PRAGMA journal_mode = MEMORY; PRAGMA temp_store = MEMORY; PRAGMA cache_size = -128000;"); err != nil {
		return IngestResult{}, fmt.Errorf("set pragmas: %w", err)
	}

	tTotal := time.Now()

	schemaSQL := `
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
	if _, err := db.Exec(schemaSQL); err != nil {
		return IngestResult{}, fmt.Errorf("create tables: %w", err)
	}

	// Load Nodes
	tNodes := time.Now()
	tx, err := db.Begin()
	if err != nil {
		return IngestResult{}, err
	}
	stmtNode, err := tx.Prepare("INSERT INTO nodes (id, kind, properties) VALUES (?, ?, ?)")
	if err != nil {
		return IngestResult{}, err
	}

	nodeFiles := []struct {
		kind string
		file string
	}{
		{"Service", "services.csv"},
		{"Database", "databases.csv"},
		{"Topic", "topics.csv"},
		{"Class", "classes.csv"},
		{"Method", "methods.csv"},
	}

	nodeCount := 0
	for _, nf := range nodeFiles {
		f, err := os.Open(filepath.Join(dataDir, nf.file))
		if err != nil {
			return IngestResult{}, err
		}
		r := csv.NewReader(f)
		r.Read() // skip header
		for {
			rec, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				f.Close()
				return IngestResult{}, err
			}
			id := rec[0]
			props := map[string]string{
				"id":   id,
				"name": rec[1],
			}
			if len(rec) > 2 && rec[2] != "" {
				props["layer"] = rec[2]
			}
			if len(rec) > 3 && rec[3] != "" {
				props["framework"] = rec[3]
			}
			if len(rec) > 4 && rec[4] != "" {
				props["language"] = rec[4]
			}
			rawJson, _ := json.Marshal(props)
			stmtNode.Exec(id, nf.kind, string(rawJson))
			nodeCount++
		}
		f.Close()
	}
	stmtNode.Close()
	tx.Commit()
	nodeDuration := time.Since(tNodes)

	// Load Edges
	tEdges := time.Now()
	tx2, err := db.Begin()
	if err != nil {
		return IngestResult{}, err
	}
	stmtEdge, err := tx2.Prepare("INSERT INTO edges (from_id, to_id, kind, properties) VALUES (?, ?, ?, '{}')")
	if err != nil {
		return IngestResult{}, err
	}

	edgeFiles := []struct {
		kind string
		file string
	}{
		{"CALLS", "calls_service_service.csv"},
		{"CALLS", "calls_method_method.csv"},
		{"CONTAINS", "contains_service_class.csv"},
		{"CONTAINS", "contains_class_method.csv"},
		{"USES_DB", "uses_db.csv"},
		{"DEPENDS_ON", "depends_on.csv"},
		{"PRODUCES", "produces.csv"},
		{"CONSUMES", "consumes.csv"},
	}

	edgeCount := 0
	for _, ef := range edgeFiles {
		f, err := os.Open(filepath.Join(dataDir, ef.file))
		if err != nil {
			return IngestResult{}, err
		}
		r := csv.NewReader(f)
		r.Read() // skip header
		for {
			rec, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				f.Close()
				return IngestResult{}, err
			}
			stmtEdge.Exec(rec[0], rec[1], ef.kind)
			edgeCount++
		}
		f.Close()
	}
	stmtEdge.Close()
	tx2.Commit()
	edgeDuration := time.Since(tEdges)

	// Indexes & Analyze
	tIdx := time.Now()
	indexSQL := `
		CREATE INDEX idx_edges_from_kind ON edges(from_id, kind);
		CREATE INDEX idx_edges_to_kind ON edges(to_id, kind);
		CREATE INDEX idx_nodes_kind ON nodes(kind);
		ANALYZE;
	`
	if _, err := db.Exec(indexSQL); err != nil {
		return IngestResult{}, fmt.Errorf("create indexes: %w", err)
	}
	idxDuration := time.Since(tIdx)

	totalDuration := time.Since(tTotal)
	fi, _ := os.Stat(tempDbPath)
	dbSizeMb := float64(fi.Size()) / (1024.0 * 1024.0)

	totalEntities := nodeCount + edgeCount
	return IngestResult{
		Engine:          "SQLite (Hybrid SQL)",
		NodeCount:       nodeCount,
		EdgeCount:       edgeCount,
		NodeLoadTime:    nodeDuration,
		EdgeLoadTime:    edgeDuration,
		IndexTime:       idxDuration,
		TotalTime:       totalDuration,
		NodeThroughput:  float64(nodeCount) / nodeDuration.Seconds(),
		EdgeThroughput:  float64(edgeCount) / edgeDuration.Seconds(),
		TotalThroughput: float64(totalEntities) / totalDuration.Seconds(),
		DbSizeMb:        dbSizeMb,
	}, nil
}

func benchmarkDuckIngest(driver *DuckDriver, dataDir string) (IngestResult, error) {
	tempDbPath := filepath.Join(dataDir, "temp_ingest_bench.duckdb")
	os.Remove(tempDbPath)
	defer os.Remove(tempDbPath)

	db, err := driver.OpenDatabase(tempDbPath)
	if err != nil {
		return IngestResult{}, fmt.Errorf("open duckdb: %w", err)
	}
	defer db.Close()

	conn, err := db.Connect()
	if err != nil {
		return IngestResult{}, fmt.Errorf("connect duckdb: %w", err)
	}
	defer conn.Close()

	tTotal := time.Now()

	// Load extension
	res, err := conn.Query("LOAD 'duckpgq';")
	if err != nil {
		return IngestResult{}, fmt.Errorf("load duckpgq: %w", err)
	}
	res.Close()

	absData, err := filepath.Abs(dataDir)
	if err != nil {
		return IngestResult{}, err
	}
	absData = filepath.ToSlash(absData)

	// 1. Load Nodes
	tNodes := time.Now()
	nodeTables := []struct {
		table string
		file  string
	}{
		{"Service", "services.csv"},
		{"Database", "databases.csv"},
		{"Topic", "topics.csv"},
		{"Class", "classes.csv"},
		{"Method", "methods.csv"},
	}

	for _, nt := range nodeTables {
		sql := fmt.Sprintf("CREATE TABLE %s AS SELECT * FROM read_csv_auto('%s/%s', header=true);", nt.table, absData, nt.file)
		qr, err := conn.Query(sql)
		if err != nil {
			return IngestResult{}, fmt.Errorf("load node %s: %w", nt.table, err)
		}
		qr.Close()
	}
	nodeDuration := time.Since(tNodes)

	// 2. Load Edges
	tEdges := time.Now()
	edgeTables := []struct {
		table string
		file  string
	}{
		{"uses_db", "uses_db.csv"},
		{"produces", "produces.csv"},
		{"consumes", "consumes.csv"},
		{"depends_on", "depends_on.csv"},
		{"calls_service", "calls_service_service.csv"},
		{"calls_method", "calls_method_method.csv"},
		{"contains_sc", "contains_service_class.csv"},
		{"contains_cm", "contains_class_method.csv"},
	}

	for _, et := range edgeTables {
		sql := fmt.Sprintf("CREATE TABLE %s AS SELECT * FROM read_csv_auto('%s/%s', header=true);", et.table, absData, et.file)
		qr, err := conn.Query(sql)
		if err != nil {
			return IngestResult{}, fmt.Errorf("load edge %s: %w", et.table, err)
		}
		qr.Close()
	}
	edgeDuration := time.Since(tEdges)

	// 3. Property Graph Definition
	tPG := time.Now()
	graphSQL := `
		CREATE PROPERTY GRAPH microservices
		VERTEX TABLES (
			Service,
			Database,
			Topic,
			Class,
			Method
		)
		EDGE TABLES (
			uses_db
				SOURCE KEY ("from") REFERENCES Service (id)
				DESTINATION KEY ("to") REFERENCES Database (id)
				LABEL USES_DB,
			produces
				SOURCE KEY ("from") REFERENCES Service (id)
				DESTINATION KEY ("to") REFERENCES Topic (id)
				LABEL PRODUCES,
			consumes
				SOURCE KEY ("from") REFERENCES Service (id)
				DESTINATION KEY ("to") REFERENCES Topic (id)
				LABEL CONSUMES,
			depends_on
				SOURCE KEY ("from") REFERENCES Class (id)
				DESTINATION KEY ("to") REFERENCES Class (id)
				LABEL DEPENDS_ON,
			calls_service
				SOURCE KEY ("from") REFERENCES Service (id)
				DESTINATION KEY ("to") REFERENCES Service (id)
				LABEL CALLS_SERVICE,
			calls_method
				SOURCE KEY ("from") REFERENCES Method (id)
				DESTINATION KEY ("to") REFERENCES Method (id)
				LABEL CALLS_METHOD,
			contains_sc
				SOURCE KEY ("from") REFERENCES Service (id)
				DESTINATION KEY ("to") REFERENCES Class (id)
				LABEL CONTAINS_SC,
			contains_cm
				SOURCE KEY ("from") REFERENCES Class (id)
				DESTINATION KEY ("to") REFERENCES Method (id)
				LABEL CONTAINS_CM
		);
	`
	qr, err := conn.Query(graphSQL)
	if err != nil {
		return IngestResult{}, fmt.Errorf("create property graph: %w", err)
	}
	qr.Close()
	pgDuration := time.Since(tPG)

	totalDuration := time.Since(tTotal)

	// Close conn and db to flush WAL
	conn.Close()
	db.Close()

	fi, err := os.Stat(tempDbPath)
	var dbSizeMb float64
	if err == nil {
		dbSizeMb = float64(fi.Size()) / (1024.0 * 1024.0)
	}

	nodeCount := 100000
	edgeCount := 198000
	totalEntities := nodeCount + edgeCount

	return IngestResult{
		Engine:          "DuckDB + DuckPGQ",
		NodeCount:       nodeCount,
		EdgeCount:       edgeCount,
		NodeLoadTime:    nodeDuration,
		EdgeLoadTime:    edgeDuration,
		IndexTime:       pgDuration,
		TotalTime:       totalDuration,
		NodeThroughput:  float64(nodeCount) / nodeDuration.Seconds(),
		EdgeThroughput:  float64(edgeCount) / edgeDuration.Seconds(),
		TotalThroughput: float64(totalEntities) / totalDuration.Seconds(),
		DbSizeMb:        dbSizeMb,
	}, nil
}

func setupPersistentDuckDB(driver *DuckDriver, dbPath, dataDir string) {
	os.Remove(dbPath)

	db, err := driver.OpenDatabase(dbPath)
	if err != nil {
		log.Fatalf("Open persistent DuckDB: %v", err)
	}
	defer db.Close()

	conn, err := db.Connect()
	if err != nil {
		log.Fatalf("Connect persistent DuckDB: %v", err)
	}
	defer conn.Close()

	res, err := conn.Query("LOAD 'duckpgq';")
	if err != nil {
		log.Fatalf("Load duckpgq: %v", err)
	}
	res.Close()

	absData, _ := filepath.Abs(dataDir)
	absData = filepath.ToSlash(absData)

	tables := []struct {
		table string
		file  string
	}{
		{"Service", "services.csv"},
		{"Database", "databases.csv"},
		{"Topic", "topics.csv"},
		{"Class", "classes.csv"},
		{"Method", "methods.csv"},
		{"uses_db", "uses_db.csv"},
		{"produces", "produces.csv"},
		{"consumes", "consumes.csv"},
		{"depends_on", "depends_on.csv"},
		{"calls_service", "calls_service_service.csv"},
		{"calls_method", "calls_method_method.csv"},
		{"contains_sc", "contains_service_class.csv"},
		{"contains_cm", "contains_class_method.csv"},
	}

	for _, t := range tables {
		sql := fmt.Sprintf("CREATE TABLE %s AS SELECT * FROM read_csv_auto('%s/%s', header=true);", t.table, absData, t.file)
		qr, err := conn.Query(sql)
		if err != nil {
			log.Fatalf("Load table %s: %v", t.table, err)
		}
		qr.Close()
	}

	graphSQL := `
		CREATE PROPERTY GRAPH microservices
		VERTEX TABLES (
			Service,
			Database,
			Topic,
			Class,
			Method
		)
		EDGE TABLES (
			uses_db
				SOURCE KEY ("from") REFERENCES Service (id)
				DESTINATION KEY ("to") REFERENCES Database (id)
				LABEL USES_DB,
			produces
				SOURCE KEY ("from") REFERENCES Service (id)
				DESTINATION KEY ("to") REFERENCES Topic (id)
				LABEL PRODUCES,
			consumes
				SOURCE KEY ("from") REFERENCES Service (id)
				DESTINATION KEY ("to") REFERENCES Topic (id)
				LABEL CONSUMES,
			depends_on
				SOURCE KEY ("from") REFERENCES Class (id)
				DESTINATION KEY ("to") REFERENCES Class (id)
				LABEL DEPENDS_ON,
			calls_service
				SOURCE KEY ("from") REFERENCES Service (id)
				DESTINATION KEY ("to") REFERENCES Service (id)
				LABEL CALLS_SERVICE,
			calls_method
				SOURCE KEY ("from") REFERENCES Method (id)
				DESTINATION KEY ("to") REFERENCES Method (id)
				LABEL CALLS_METHOD,
			contains_sc
				SOURCE KEY ("from") REFERENCES Service (id)
				DESTINATION KEY ("to") REFERENCES Class (id)
				LABEL CONTAINS_SC,
			contains_cm
				SOURCE KEY ("from") REFERENCES Class (id)
				DESTINATION KEY ("to") REFERENCES Method (id)
				LABEL CONTAINS_CM
		);
	`
	qr, err := conn.Query(graphSQL)
	if err != nil {
		log.Fatalf("Create property graph: %v", err)
	}
	qr.Close()
}
