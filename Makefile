.PHONY: test bench fmt vet check example-sqlite example-clickhouse

all: check

test:
	go test -v -cover ./...

bench:
	go test -tags bench -v -run=^$$ -bench=. -benchmem ./...

fmt:
	gofmt -w -s .

vet:
	go vet ./...

check: fmt vet test

example-sqlite:
	go run examples/sqlite_quickstart/main.go

example-clickhouse:
	go run examples/clickhouse_transpiler/main.go
