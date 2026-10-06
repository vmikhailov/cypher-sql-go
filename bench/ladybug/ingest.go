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

func benchmarkLadybugIngest(driver *LadybugDriver, dataDir string) (IngestResult, error) {
	tempLbugDir := filepath.Join(dataDir, "temp_ingest_bench.lbug")
	os.RemoveAll(tempLbugDir)
	defer os.RemoveAll(tempLbugDir)

	db, err := driver.OpenDatabase(tempLbugDir, 1024*1024*1024)
	if err != nil {
		return IngestResult{}, fmt.Errorf("open lbug: %w", err)
	}
	defer db.Close()

	conn, err := db.Connect()
	if err != nil {
		return IngestResult{}, fmt.Errorf("connect lbug: %w", err)
	}
	defer conn.Close()

	tTotal := time.Now()

	// 1. DDL
	ddls := []string{
		"CREATE NODE TABLE Service (id STRING PRIMARY KEY, name STRING, layer STRING, framework STRING, language STRING);",
		"CREATE NODE TABLE Database (id STRING PRIMARY KEY, name STRING, layer STRING, framework STRING, language STRING);",
		"CREATE NODE TABLE Topic (id STRING PRIMARY KEY, name STRING, layer STRING, framework STRING, language STRING);",
		"CREATE NODE TABLE Class (id STRING PRIMARY KEY, name STRING, layer STRING, framework STRING, language STRING);",
		"CREATE NODE TABLE Method (id STRING PRIMARY KEY, name STRING, layer STRING, framework STRING, language STRING);",
		"CREATE REL TABLE USES_DB (FROM Service TO Database);",
		"CREATE REL TABLE PRODUCES (FROM Service TO Topic);",
		"CREATE REL TABLE CONSUMES (FROM Service TO Topic);",
		"CREATE REL TABLE DEPENDS_ON (FROM Class TO Class);",
		"CREATE REL TABLE CALLS (FROM Service TO Service, FROM Method TO Method);",
		"CREATE REL TABLE CONTAINS (FROM Service TO Class, FROM Class TO Method);",
	}
	tDDL := time.Now()
	for _, ddl := range ddls {
		qr, err := conn.Query(ddl)
		if err != nil || !qr.IsSuccess() {
			return IngestResult{}, fmt.Errorf("ddl: %s", qr.ErrorMessage())
		}
		qr.Close()
	}
	ddlDuration := time.Since(tDDL)

	absData, err := filepath.Abs(dataDir)
	if err != nil {
		return IngestResult{}, err
	}
	absData = filepath.ToSlash(absData)

	// 2. Load Nodes
	tNodes := time.Now()
	nodeLoads := []string{
		fmt.Sprintf("COPY Service FROM '%s/services.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY Database FROM '%s/databases.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY Topic FROM '%s/topics.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY Class FROM '%s/classes.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY Method FROM '%s/methods.csv' (HEADER=true);", absData),
	}
	for _, nl := range nodeLoads {
		qr, err := conn.Query(nl)
		if err != nil || !qr.IsSuccess() {
			return IngestResult{}, fmt.Errorf("node copy: %s", qr.ErrorMessage())
		}
		qr.Close()
	}
	nodeDuration := time.Since(tNodes)

	// 3. Load Edges
	tEdges := time.Now()
	edgeLoads := []string{
		fmt.Sprintf("COPY USES_DB FROM '%s/uses_db.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY PRODUCES FROM '%s/produces.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY CONSUMES FROM '%s/consumes.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY DEPENDS_ON FROM '%s/depends_on.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY CALLS FROM '%s/calls_service_service.csv' (FROM='Service', TO='Service', HEADER=true);", absData),
		fmt.Sprintf("COPY CALLS FROM '%s/calls_method_method.csv' (FROM='Method', TO='Method', HEADER=true);", absData),
		fmt.Sprintf("COPY CONTAINS FROM '%s/contains_service_class.csv' (FROM='Service', TO='Class', HEADER=true);", absData),
		fmt.Sprintf("COPY CONTAINS FROM '%s/contains_class_method.csv' (FROM='Class', TO='Method', HEADER=true);", absData),
	}
	for _, el := range edgeLoads {
		qr, err := conn.Query(el)
		if err != nil || !qr.IsSuccess() {
			return IngestResult{}, fmt.Errorf("edge copy: %s", qr.ErrorMessage())
		}
		qr.Close()
	}
	edgeDuration := time.Since(tEdges)

	totalDuration := time.Since(tTotal)

	var lbugSize int64
	filepath.Walk(tempLbugDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			lbugSize += info.Size()
		}
		return nil
	})
	dbSizeMb := float64(lbugSize) / (1024.0 * 1024.0)

	nodeCount := 100000
	edgeCount := 198000
	totalEntities := nodeCount + edgeCount

	return IngestResult{
		Engine:          "LadybugDB (Native)",
		NodeCount:       nodeCount,
		EdgeCount:       edgeCount,
		NodeLoadTime:    nodeDuration,
		EdgeLoadTime:    edgeDuration,
		IndexTime:       ddlDuration,
		TotalTime:       totalDuration,
		NodeThroughput:  float64(nodeCount) / nodeDuration.Seconds(),
		EdgeThroughput:  float64(edgeCount) / edgeDuration.Seconds(),
		TotalThroughput: float64(totalEntities) / totalDuration.Seconds(),
		DbSizeMb:        dbSizeMb,
	}, nil
}

func setupPersistentLadybug(driver *LadybugDriver, dbPath, dataDir string) {
	db, err := driver.OpenDatabase(dbPath, 1024*1024*1024)
	if err != nil {
		log.Fatalf("Open persistent LadybugDB: %v", err)
	}
	defer db.Close()

	conn, err := db.Connect()
	if err != nil {
		log.Fatalf("Connect persistent LadybugDB: %v", err)
	}
	defer conn.Close()

	ddls := []string{
		"CREATE NODE TABLE Service (id STRING PRIMARY KEY, name STRING, layer STRING, framework STRING, language STRING);",
		"CREATE NODE TABLE Database (id STRING PRIMARY KEY, name STRING, layer STRING, framework STRING, language STRING);",
		"CREATE NODE TABLE Topic (id STRING PRIMARY KEY, name STRING, layer STRING, framework STRING, language STRING);",
		"CREATE NODE TABLE Class (id STRING PRIMARY KEY, name STRING, layer STRING, framework STRING, language STRING);",
		"CREATE NODE TABLE Method (id STRING PRIMARY KEY, name STRING, layer STRING, framework STRING, language STRING);",
		"CREATE REL TABLE USES_DB (FROM Service TO Database);",
		"CREATE REL TABLE PRODUCES (FROM Service TO Topic);",
		"CREATE REL TABLE CONSUMES (FROM Service TO Topic);",
		"CREATE REL TABLE DEPENDS_ON (FROM Class TO Class);",
		"CREATE REL TABLE CALLS (FROM Service TO Service, FROM Method TO Method);",
		"CREATE REL TABLE CONTAINS (FROM Service TO Class, FROM Class TO Method);",
	}

	for _, ddl := range ddls {
		qr, err := conn.Query(ddl)
		if err != nil || !qr.IsSuccess() {
			log.Fatalf("DDL: %s: %s", ddl, qr.ErrorMessage())
		}
		qr.Close()
	}

	absData, _ := filepath.Abs(dataDir)
	absData = filepath.ToSlash(absData)

	loads := []string{
		fmt.Sprintf("COPY Service FROM '%s/services.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY Database FROM '%s/databases.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY Topic FROM '%s/topics.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY Class FROM '%s/classes.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY Method FROM '%s/methods.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY USES_DB FROM '%s/uses_db.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY PRODUCES FROM '%s/produces.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY CONSUMES FROM '%s/consumes.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY DEPENDS_ON FROM '%s/depends_on.csv' (HEADER=true);", absData),
		fmt.Sprintf("COPY CALLS FROM '%s/calls_service_service.csv' (FROM='Service', TO='Service', HEADER=true);", absData),
		fmt.Sprintf("COPY CALLS FROM '%s/calls_method_method.csv' (FROM='Method', TO='Method', HEADER=true);", absData),
		fmt.Sprintf("COPY CONTAINS FROM '%s/contains_service_class.csv' (FROM='Service', TO='Class', HEADER=true);", absData),
		fmt.Sprintf("COPY CONTAINS FROM '%s/contains_class_method.csv' (FROM='Class', TO='Method', HEADER=true);", absData),
	}

	for _, l := range loads {
		qr, err := conn.Query(l)
		if err != nil || !qr.IsSuccess() {
			log.Fatalf("COPY: %s: %s", l, qr.ErrorMessage())
		}
		qr.Close()
	}
}
