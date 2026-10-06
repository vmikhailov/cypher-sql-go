//go:build bench

package main

import (
	"math"
	"sort"
	"time"
)

type QuerySpec struct {
	ID          string
	Category    string // "OLTP" or "OLAP"
	Name        string
	Description string
	Cypher      string
}

type LatencyStats struct {
	Min   time.Duration
	Max   time.Duration
	Avg   time.Duration
	P50   time.Duration
	P95   time.Duration
	P99   time.Duration
	Count int
}

func calcStats(durations []time.Duration) LatencyStats {
	if len(durations) == 0 {
		return LatencyStats{}
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total time.Duration
	for _, d := range sorted {
		total += d
	}
	avg := total / time.Duration(len(sorted))
	p50 := sorted[len(sorted)*50/100]
	p95 := sorted[len(sorted)*95/100]
	p99 := sorted[len(sorted)*99/100]
	return LatencyStats{
		Min:   sorted[0],
		Max:   sorted[len(sorted)-1],
		Avg:   avg,
		P50:   p50,
		P95:   p95,
		P99:   p99,
		Count: len(sorted),
	}
}

type IngestResult struct {
	Engine          string
	NodeCount       int
	EdgeCount       int
	NodeLoadTime    time.Duration
	EdgeLoadTime    time.Duration
	IndexTime       time.Duration
	TotalTime       time.Duration
	NodeThroughput  float64 // nodes/sec
	EdgeThroughput  float64 // edges/sec
	TotalThroughput float64 // entities/sec
	DbSizeMb        float64
	Score           float64 // normalized score (100.0 = baseline)
}

type BenchmarkQueryResult struct {
	Query                  QuerySpec
	SqliteRows             int
	LadybugRows            int
	CompileTimeUs          float64
	CompileBytesOp         uint64
	CompileAllocsOp        uint64
	SqlitePrecompiled      LatencyStats
	SqliteEndToEnd         LatencyStats
	LadybugAdHoc           LatencyStats
	LadybugPrepared        LatencyStats
	ScoreSqlite            float64 // (Ladybug Adhoc / Sqlite E2E) * 100.0 (End-to-End)
	ScoreSqlitePrecompiled float64 // (Ladybug Prepared / Sqlite Precompiled) * 100.0 (Execution-only)
	ScoreLadybug           float64 // 100.0 (baseline)
}

func geometricMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sumLn float64
	for _, v := range values {
		if v <= 0 {
			v = 0.0001
		}
		sumLn += math.Log(v)
	}
	return math.Exp(sumLn / float64(len(values)))
}

func weightedGeometricMean(values []float64, weights []float64) float64 {
	if len(values) == 0 || len(values) != len(weights) {
		return 0
	}
	var sumWeight, sumWeightedLn float64
	for i, v := range values {
		w := weights[i]
		if v <= 0 {
			v = 0.0001
		}
		sumWeight += w
		sumWeightedLn += w * math.Log(v)
	}
	if sumWeight == 0 {
		return 0
	}
	return math.Exp(sumWeightedLn / sumWeight)
}

func toMs(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

func speedRatio(a, b float64) float64 {
	if a > b {
		return a / b
	}
	return b / a
}

func speedWinner(lbug, sql float64) string {
	if sql < lbug {
		return "SQLite"
	}
	return "LadybugDB"
}

func sizeWinner(sql, lbug float64) string {
	if sql < lbug {
		return "SQLite"
	}
	return "LadybugDB"
}
