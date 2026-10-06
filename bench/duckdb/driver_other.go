//go:build !windows

package main

import (
	"errors"
)

type DuckDriver struct{}
type DuckDatabase struct{}
type DuckConnection struct{}
type DuckPreparedStatement struct{}
type DuckResult struct{}

func NewDuckDriver(binDir string) (*DuckDriver, error) {
	return nil, errors.New("embedded DuckDB driver currently only configured for Windows in this repository")
}

func (d *DuckDriver) OpenDatabase(dbPath string) (*DuckDatabase, error) {
	return nil, errors.New("not supported on this platform")
}

func (db *DuckDatabase) Close() {}

func (db *DuckDatabase) Connect() (*DuckConnection, error) {
	return nil, errors.New("not supported on this platform")
}

func (c *DuckConnection) Close() {}

func (c *DuckConnection) Query(sqlStr string) (*DuckResult, error) {
	return nil, errors.New("not supported on this platform")
}

func (c *DuckConnection) Prepare(sqlStr string) (*DuckPreparedStatement, error) {
	return nil, errors.New("not supported on this platform")
}

func (ps *DuckPreparedStatement) Execute() (*DuckResult, error) {
	return nil, errors.New("not supported on this platform")
}

func (ps *DuckPreparedStatement) Close() {}

func (r *DuckResult) Close() {}

func (r *DuckResult) RowCount() int {
	return 0
}

func (r *DuckResult) ColumnCount() int {
	return 0
}
