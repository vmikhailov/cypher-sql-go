.PHONY: test bench bench-ladybug bench-duckdb fmt vet check example-sqlite example-clickhouse

all: check

test:
	go test -v -cover ./...

bench:
	go test -tags bench -v -run=^$$ -bench=. -benchmem ./...

bench-ladybug:
	go run ./bench/ladybug

bench-duckdb:
	go run ./bench/duckdb

fmt:
	gofmt -w -s .

vet:
	go vet ./...

check: fmt vet test

example-sqlite:
	go run examples/sqlite_quickstart/main.go

example-clickhouse:
	go run examples/clickhouse_transpiler/main.go
