package main

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

type SystemConfig struct {
	BufferPoolSize          uint64
	MaxNumThreads           uint64
	EnableCompression       bool
	ReadOnly                bool
	MaxDbSize               uint64
	AutoCheckpoint          bool
	CheckpointThreshold     uint64
	ThrowOnWalReplayFailure bool
	EnableChecksums         bool
	EnableMultiWrites       bool
	EnableDefaultHashIndex  bool
}

type LadybugDriver struct {
	dll                   *syscall.LazyDLL
	defaultConfigProc     *syscall.LazyProc
	dbInitProc            *syscall.LazyProc
	dbDestroyProc         *syscall.LazyProc
	connInitProc          *syscall.LazyProc
	connDestroyProc       *syscall.LazyProc
	connQueryProc         *syscall.LazyProc
	connPrepareProc       *syscall.LazyProc
	connExecuteProc       *syscall.LazyProc
	psDestroyProc         *syscall.LazyProc
	qrDestroyProc         *syscall.LazyProc
	qrIsSuccessProc       *syscall.LazyProc
	qrGetNumTuplesProc    *syscall.LazyProc
	qrGetErrorMessageProc *syscall.LazyProc
	qrToStringProc        *syscall.LazyProc
}

type Database struct {
	driver *LadybugDriver
	ptr    uintptr
}

type Connection struct {
	driver *LadybugDriver
	ptr    uintptr
}

type PreparedStatement struct {
	driver      *LadybugDriver
	ptr         uintptr
	boundValues uintptr
}

type QueryResult struct {
	driver  *LadybugDriver
	ptr     uintptr
	isOwned bool
}

func cStringToGo(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	var bytes []byte
	for i := uintptr(0); ; i++ {
		b := *(*byte)(unsafe.Pointer(ptr + i))
		if b == 0 {
			break
		}
		bytes = append(bytes, b)
	}
	return string(bytes)
}

func NewLadybugDriver(binDir string) (*LadybugDriver, error) {
	absBin, err := filepath.Abs(binDir)
	if err != nil {
		return nil, fmt.Errorf("resolve binDir: %w", err)
	}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setDllDirectory := kernel32.NewProc("SetDllDirectoryW")
	binPtr, err := syscall.UTF16PtrFromString(absBin)
	if err != nil {
		return nil, fmt.Errorf("utf16 ptr: %w", err)
	}
	setDllDirectory.Call(uintptr(unsafe.Pointer(binPtr)))

	dllPath := filepath.Join(absBin, "lbug_shared.dll")
	dll := syscall.NewLazyDLL(dllPath)

	return &LadybugDriver{
		dll:                   dll,
		defaultConfigProc:     dll.NewProc("lbug_default_system_config"),
		dbInitProc:            dll.NewProc("lbug_database_init"),
		dbDestroyProc:         dll.NewProc("lbug_database_destroy"),
		connInitProc:          dll.NewProc("lbug_connection_init"),
		connDestroyProc:       dll.NewProc("lbug_connection_destroy"),
		connQueryProc:         dll.NewProc("lbug_connection_query"),
		connPrepareProc:       dll.NewProc("lbug_connection_prepare"),
		connExecuteProc:       dll.NewProc("lbug_connection_execute"),
		psDestroyProc:         dll.NewProc("lbug_prepared_statement_destroy"),
		qrDestroyProc:         dll.NewProc("lbug_query_result_destroy"),
		qrIsSuccessProc:       dll.NewProc("lbug_query_result_is_success"),
		qrGetNumTuplesProc:    dll.NewProc("lbug_query_result_get_num_tuples"),
		qrGetErrorMessageProc: dll.NewProc("lbug_query_result_get_error_message"),
		qrToStringProc:        dll.NewProc("lbug_query_result_to_string"),
	}, nil
}

func (d *LadybugDriver) OpenDatabase(dbPath string, bufferPoolBytes uint64) (*Database, error) {
	var cfg SystemConfig
	d.defaultConfigProc.Call(uintptr(unsafe.Pointer(&cfg)))
	if bufferPoolBytes > 0 {
		cfg.BufferPoolSize = bufferPoolBytes
	}

	pathBytes, err := syscall.BytePtrFromString(dbPath)
	if err != nil {
		return nil, fmt.Errorf("byte ptr from path: %w", err)
	}

	var db Database
	db.driver = d
	r, _, err := d.dbInitProc.Call(
		uintptr(unsafe.Pointer(pathBytes)),
		uintptr(unsafe.Pointer(&cfg)),
		uintptr(unsafe.Pointer(&db.ptr)),
	)
	if r != 0 {
		return nil, fmt.Errorf("lbug_database_init failed with code %d: %v", r, err)
	}
	return &db, nil
}

func (db *Database) Close() {
	if db != nil && db.ptr != 0 {
		db.driver.dbDestroyProc.Call(uintptr(unsafe.Pointer(&db.ptr)))
		db.ptr = 0
	}
}

func (db *Database) Connect() (*Connection, error) {
	var conn Connection
	conn.driver = db.driver
	r, _, err := db.driver.connInitProc.Call(
		uintptr(unsafe.Pointer(&db.ptr)),
		uintptr(unsafe.Pointer(&conn.ptr)),
	)
	if r != 0 {
		return nil, fmt.Errorf("lbug_connection_init failed with code %d: %v", r, err)
	}
	return &conn, nil
}

func (c *Connection) Close() {
	if c != nil && c.ptr != 0 {
		c.driver.connDestroyProc.Call(uintptr(unsafe.Pointer(&c.ptr)))
		c.ptr = 0
	}
}

func (c *Connection) Query(cypher string) (*QueryResult, error) {
	qStr, err := syscall.BytePtrFromString(cypher)
	if err != nil {
		return nil, fmt.Errorf("convert cypher: %w", err)
	}

	var qr QueryResult
	qr.driver = c.driver
	r, _, err := c.driver.connQueryProc.Call(
		uintptr(unsafe.Pointer(&c.ptr)),
		uintptr(unsafe.Pointer(qStr)),
		uintptr(unsafe.Pointer(&qr.ptr)),
	)
	if r != 0 {
		return nil, fmt.Errorf("lbug_connection_query failed with code %d: %v", r, err)
	}
	return &qr, nil
}

func (c *Connection) Prepare(cypher string) (*PreparedStatement, error) {
	qStr, err := syscall.BytePtrFromString(cypher)
	if err != nil {
		return nil, fmt.Errorf("convert cypher: %w", err)
	}

	var ps PreparedStatement
	ps.driver = c.driver
	r, _, err := c.driver.connPrepareProc.Call(
		uintptr(unsafe.Pointer(&c.ptr)),
		uintptr(unsafe.Pointer(qStr)),
		uintptr(unsafe.Pointer(&ps.ptr)),
	)
	if r != 0 {
		return nil, fmt.Errorf("lbug_connection_prepare failed with code %d: %v", r, err)
	}
	return &ps, nil
}

func (ps *PreparedStatement) Execute(c *Connection) (*QueryResult, error) {
	var qr QueryResult
	qr.driver = ps.driver
	r, _, err := ps.driver.connExecuteProc.Call(
		uintptr(unsafe.Pointer(&c.ptr)),
		uintptr(unsafe.Pointer(&ps.ptr)),
		uintptr(unsafe.Pointer(&qr.ptr)),
	)
	if r != 0 {
		return nil, fmt.Errorf("lbug_connection_execute failed with code %d: %v", r, err)
	}
	return &qr, nil
}

func (ps *PreparedStatement) Close() {
	if ps != nil && ps.ptr != 0 {
		ps.driver.psDestroyProc.Call(uintptr(unsafe.Pointer(&ps.ptr)))
		ps.ptr = 0
	}
}

func (qr *QueryResult) Close() {
	if qr != nil && qr.ptr != 0 {
		qr.driver.qrDestroyProc.Call(uintptr(unsafe.Pointer(&qr.ptr)))
		qr.ptr = 0
	}
}

func (qr *QueryResult) IsSuccess() bool {
	if qr == nil || qr.ptr == 0 {
		return false
	}
	r, _, _ := qr.driver.qrIsSuccessProc.Call(uintptr(unsafe.Pointer(&qr.ptr)))
	return r != 0
}

func (qr *QueryResult) NumTuples() int {
	if qr == nil || qr.ptr == 0 {
		return 0
	}
	r, _, _ := qr.driver.qrGetNumTuplesProc.Call(uintptr(unsafe.Pointer(&qr.ptr)))
	return int(r)
}

func (qr *QueryResult) ErrorMessage() string {
	if qr == nil || qr.ptr == 0 {
		return ""
	}
	r, _, _ := qr.driver.qrGetErrorMessageProc.Call(uintptr(unsafe.Pointer(&qr.ptr)))
	if r == 0 {
		return ""
	}
	return cStringToGo(r)
}

func (qr *QueryResult) ToString() string {
	if qr == nil || qr.ptr == 0 {
		return ""
	}
	r, _, _ := qr.driver.qrToStringProc.Call(uintptr(unsafe.Pointer(&qr.ptr)))
	if r == 0 {
		return ""
	}
	return cStringToGo(r)
}
