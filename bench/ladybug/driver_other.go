//go:build !windows

package main

import (
	"fmt"
)

type LadybugDriver struct{}
type Database struct{}
type Connection struct{}
type PreparedStatement struct{}
type QueryResult struct{}

func NewLadybugDriver(binDir string) (*LadybugDriver, error) {
	return nil, fmt.Errorf("LadybugDB driver is only supported on Windows")
}

func (d *LadybugDriver) OpenDatabase(dbPath string, bufferPoolBytes uint64) (*Database, error) {
	return nil, fmt.Errorf("LadybugDB driver is only supported on Windows")
}

func (db *Database) Close() {}

func (db *Database) Connect() (*Connection, error) {
	return nil, fmt.Errorf("LadybugDB driver is only supported on Windows")
}

func (c *Connection) Close() {}

func (c *Connection) Query(cypher string) (*QueryResult, error) {
	return nil, fmt.Errorf("LadybugDB driver is only supported on Windows")
}

func (c *Connection) Prepare(cypher string) (*PreparedStatement, error) {
	return nil, fmt.Errorf("LadybugDB driver is only supported on Windows")
}

func (ps *PreparedStatement) Execute(c *Connection) (*QueryResult, error) {
	return nil, fmt.Errorf("LadybugDB driver is only supported on Windows")
}

func (ps *PreparedStatement) Close() {}

func (qr *QueryResult) Close() {}

func (qr *QueryResult) IsSuccess() bool {
	return false
}

func (qr *QueryResult) NumTuples() int {
	return 0
}

func (qr *QueryResult) ErrorMessage() string {
	return "LadybugDB driver is only supported on Windows"
}

func (qr *QueryResult) ToString() string {
	return ""
}
