//go:build windows && bench

package main

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

type duckResultRaw struct {
	colCount    uint64
	rowCount    uint64
	rowsChanged uint64
	columns     uintptr
	errorMsg    uintptr
	internal    uintptr
}

type DuckDriver struct {
	dll                *syscall.LazyDLL
	procCreateConfig   *syscall.LazyProc
	procSetConfig      *syscall.LazyProc
	procDestroyConfig  *syscall.LazyProc
	procOpenExt        *syscall.LazyProc
	procClose          *syscall.LazyProc
	procConnect        *syscall.LazyProc
	procDisconnect     *syscall.LazyProc
	procQuery          *syscall.LazyProc
	procPrepare        *syscall.LazyProc
	procDestroyPrepare *syscall.LazyProc
	procPrepareError   *syscall.LazyProc
	procExecPrepared   *syscall.LazyProc
	procDestroyResult  *syscall.LazyProc
	procResultError    *syscall.LazyProc
	procRowCount       *syscall.LazyProc
	procColumnCount    *syscall.LazyProc
	procValueVarchar   *syscall.LazyProc
	procFree           *syscall.LazyProc
}

type DuckDatabase struct {
	driver *DuckDriver
	ptr    uintptr
}

type DuckConnection struct {
	driver *DuckDriver
	ptr    uintptr
}

type DuckPreparedStatement struct {
	driver *DuckDriver
	ptr    uintptr
}

type DuckResult struct {
	driver *DuckDriver
	raw    duckResultRaw
	closed bool
}

func cString(s string) []byte {
	return append([]byte(s), 0)
}

func goString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	p := *(*unsafe.Pointer)(unsafe.Pointer(&ptr))
	var bytes []byte
	for i := uintptr(0); ; i++ {
		b := *(*byte)(unsafe.Add(p, i))
		if b == 0 {
			break
		}
		bytes = append(bytes, b)
	}
	return string(bytes)
}

func NewDuckDriver(binDir string) (*DuckDriver, error) {
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

	dllPath := filepath.Join(absBin, "duckdb.dll")
	dll := syscall.NewLazyDLL(dllPath)

	return &DuckDriver{
		dll:                dll,
		procCreateConfig:   dll.NewProc("duckdb_create_config"),
		procSetConfig:      dll.NewProc("duckdb_set_config"),
		procDestroyConfig:  dll.NewProc("duckdb_destroy_config"),
		procOpenExt:        dll.NewProc("duckdb_open_ext"),
		procClose:          dll.NewProc("duckdb_close"),
		procConnect:        dll.NewProc("duckdb_connect"),
		procDisconnect:     dll.NewProc("duckdb_disconnect"),
		procQuery:          dll.NewProc("duckdb_query"),
		procPrepare:        dll.NewProc("duckdb_prepare"),
		procDestroyPrepare: dll.NewProc("duckdb_destroy_prepare"),
		procPrepareError:   dll.NewProc("duckdb_prepare_error"),
		procExecPrepared:   dll.NewProc("duckdb_execute_prepared"),
		procDestroyResult:  dll.NewProc("duckdb_destroy_result"),
		procResultError:    dll.NewProc("duckdb_result_error"),
		procRowCount:       dll.NewProc("duckdb_row_count"),
		procColumnCount:    dll.NewProc("duckdb_column_count"),
		procValueVarchar:   dll.NewProc("duckdb_value_varchar"),
		procFree:           dll.NewProc("duckdb_free"),
	}, nil
}

func (d *DuckDriver) OpenDatabase(dbPath string) (*DuckDatabase, error) {
	var cfg uintptr
	r, _, _ := d.procCreateConfig.Call(uintptr(unsafe.Pointer(&cfg)))
	if r != 0 {
		return nil, fmt.Errorf("duckdb_create_config failed")
	}
	defer d.procDestroyConfig.Call(uintptr(unsafe.Pointer(&cfg)))

	optName := cString("allow_unsigned_extensions")
	optVal := cString("true")
	d.procSetConfig.Call(cfg, uintptr(unsafe.Pointer(&optName[0])), uintptr(unsafe.Pointer(&optVal[0])))

	var dbPtr uintptr
	var errPtr uintptr
	pBytes := cString(dbPath)
	r, _, _ = d.procOpenExt.Call(
		uintptr(unsafe.Pointer(&pBytes[0])),
		uintptr(unsafe.Pointer(&dbPtr)),
		cfg,
		uintptr(unsafe.Pointer(&errPtr)),
	)
	if r != 0 {
		msg := "unknown error"
		if errPtr != 0 {
			msg = goString(errPtr)
		}
		return nil, fmt.Errorf("duckdb_open_ext failed: %s", msg)
	}

	return &DuckDatabase{
		driver: d,
		ptr:    dbPtr,
	}, nil
}

func (db *DuckDatabase) Close() {
	if db != nil && db.ptr != 0 {
		db.driver.procClose.Call(uintptr(unsafe.Pointer(&db.ptr)))
		db.ptr = 0
	}
}

func (db *DuckDatabase) Connect() (*DuckConnection, error) {
	var connPtr uintptr
	r, _, _ := db.driver.procConnect.Call(db.ptr, uintptr(unsafe.Pointer(&connPtr)))
	if r != 0 {
		return nil, fmt.Errorf("duckdb_connect failed")
	}
	return &DuckConnection{
		driver: db.driver,
		ptr:    connPtr,
	}, nil
}

func (c *DuckConnection) Close() {
	if c != nil && c.ptr != 0 {
		c.driver.procDisconnect.Call(uintptr(unsafe.Pointer(&c.ptr)))
		c.ptr = 0
	}
}

func (c *DuckConnection) Query(sqlStr string) (*DuckResult, error) {
	qBytes := cString(sqlStr)
	res := &DuckResult{
		driver: c.driver,
	}
	r, _, _ := c.driver.procQuery.Call(
		c.ptr,
		uintptr(unsafe.Pointer(&qBytes[0])),
		uintptr(unsafe.Pointer(&res.raw)),
	)
	if r != 0 {
		defer res.Close()
		errMsg := ""
		errCStr, _, _ := c.driver.procResultError.Call(uintptr(unsafe.Pointer(&res.raw)))
		if errCStr != 0 {
			errMsg = goString(errCStr)
		}
		return nil, fmt.Errorf("duckdb_query failed: %s", errMsg)
	}
	return res, nil
}

func (c *DuckConnection) Prepare(sqlStr string) (*DuckPreparedStatement, error) {
	qBytes := cString(sqlStr)
	var psPtr uintptr
	r, _, _ := c.driver.procPrepare.Call(
		c.ptr,
		uintptr(unsafe.Pointer(&qBytes[0])),
		uintptr(unsafe.Pointer(&psPtr)),
	)
	if r != 0 {
		errMsg := ""
		if psPtr != 0 {
			errPtr, _, _ := c.driver.procPrepareError.Call(psPtr)
			if errPtr != 0 {
				errMsg = goString(errPtr)
			}
			c.driver.procDestroyPrepare.Call(uintptr(unsafe.Pointer(&psPtr)))
		}
		return nil, fmt.Errorf("duckdb_prepare failed: %s", errMsg)
	}
	return &DuckPreparedStatement{
		driver: c.driver,
		ptr:    psPtr,
	}, nil
}

func (ps *DuckPreparedStatement) Execute() (*DuckResult, error) {
	res := &DuckResult{
		driver: ps.driver,
	}
	r, _, _ := ps.driver.procExecPrepared.Call(
		ps.ptr,
		uintptr(unsafe.Pointer(&res.raw)),
	)
	if r != 0 {
		defer res.Close()
		errMsg := ""
		errCStr, _, _ := ps.driver.procResultError.Call(uintptr(unsafe.Pointer(&res.raw)))
		if errCStr != 0 {
			errMsg = goString(errCStr)
		}
		return nil, fmt.Errorf("duckdb_execute_prepared failed: %s", errMsg)
	}
	return res, nil
}

func (ps *DuckPreparedStatement) Close() {
	if ps != nil && ps.ptr != 0 {
		ps.driver.procDestroyPrepare.Call(uintptr(unsafe.Pointer(&ps.ptr)))
		ps.ptr = 0
	}
}

func (r *DuckResult) Close() {
	if r != nil && !r.closed {
		r.driver.procDestroyResult.Call(uintptr(unsafe.Pointer(&r.raw)))
		r.closed = true
	}
}

func (r *DuckResult) RowCount() int {
	if r == nil || r.closed {
		return 0
	}
	rc, _, _ := r.driver.procRowCount.Call(uintptr(unsafe.Pointer(&r.raw)))
	return int(rc)
}

func (r *DuckResult) ColumnCount() int {
	if r == nil || r.closed {
		return 0
	}
	cc, _, _ := r.driver.procColumnCount.Call(uintptr(unsafe.Pointer(&r.raw)))
	return int(cc)
}
