package mock

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MockDuckDBClient 是DuckDB客户端的Mock实现
type MockDuckDBClient struct {
	mu           sync.RWMutex
	tables       map[string]map[string][]map[string]interface{} // db -> table -> rows
	schemas      map[string]map[string]map[string]interface{} // db -> table -> schema
	connections  int
	config       MockConfig
	operationLog []string
	lastOperation  string
	queryLog     []string // 记录查询语句
}

// NewMockDuckDBClient 创建新的Mock DuckDB客户端
func NewMockDuckDBClient() *MockDuckDBClient {
	return &MockDuckDBClient{
		tables:       make(map[string]map[string][]map[string]interface{}),
		schemas:      make(map[string]map[string]map[string]interface{}),
		operationLog: make([]string, 0),
		queryLog:     make([]string, 0),
	}
}

// SetConfig 设置Mock配置
func (d *MockDuckDBClient) SetConfig(config MockConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.config = config
}

// Reset 重置Mock状态
func (d *MockDuckDBClient) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.tables = make(map[string]map[string][]map[string]interface{})
	d.schemas = make(map[string]map[string]map[string]interface{})
	d.connections = 0
	d.operationLog = make([]string, 0)
	d.queryLog = make([]string, 0)
	d.lastOperation = ""
}

// Connect 连接到数据库
func (d *MockDuckDBClient) Connect(ctx context.Context, dsn string) error {
	d.logOperation("Connect", dsn)

	if d.config.DuckDB.ShouldFailConnect {
		return fmt.Errorf("mock DuckDB: failed to connect to %s", dsn)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.connections++

	// Extract database name from DSN
	parts := strings.Split(dsn, ":")
	if len(parts) > 0 {
		dbName := parts[len(parts)-1]
		if dbName == "" {
			dbName = "memory"
		}
		if d.tables[dbName] == nil {
			d.tables[dbName] = make(map[string][]map[string]interface{})
			d.schemas[dbName] = make(map[string]map[string]interface{})
		}
	}

	return nil
}

// Close 关闭连接
func (d *MockDuckDBClient) Close() error {
	d.logOperation("Close", "")
	return nil
}

// Exec 执行SQL语句
func (d *MockDuckDBClient) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	d.logOperation("Exec", query)
	d.logQuery(query)

	if d.config.DuckDB.ShouldFailExec {
		return nil, fmt.Errorf("mock DuckDB: failed to execute query: %s", query)
	}

	if d.config.DuckDB.ExecDelay > 0 {
		time.Sleep(d.config.DuckDB.ExecDelay)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Parse CREATE TABLE statements
	if strings.Contains(strings.ToUpper(query), "CREATE TABLE") {
		return d.execCreateTable(query)
	}

	// Parse INSERT statements
	if strings.Contains(strings.ToUpper(query), "INSERT") {
		return d.execInsert(query, args...)
	}

	// Parse UPDATE statements
	if strings.Contains(strings.ToUpper(query), "UPDATE") {
		return d.execUpdate(query, args...)
	}

	// Parse DELETE statements
	if strings.Contains(strings.ToUpper(query), "DELETE") {
		return d.execDelete(query, args...)
	}

	// Return empty result for other statements
	return &MockResult{}, nil
}

// Query 执行查询
func (d *MockDuckDBClient) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	d.logOperation("Query", query)
	d.logQuery(query)

	if d.config.DuckDB.ShouldFailQuery {
		return nil, fmt.Errorf("mock DuckDB: failed to query: %s", query)
	}

	if d.config.DuckDB.QueryDelay > 0 {
		time.Sleep(d.config.DuckDB.QueryDelay)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Parse SELECT statements
	if strings.Contains(strings.ToUpper(query), "SELECT") {
		return d.execSelect(query, args...)
	}

	// Return empty rows for other statements
	return nil, nil
}

// CreateDatabase 创建数据库
func (d *MockDuckDBClient) CreateDatabase(ctx context.Context, dbName string) error {
	d.logOperation("CreateDatabase", dbName)

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.tables[dbName] != nil {
		return fmt.Errorf("database %s already exists", dbName)
	}

	d.tables[dbName] = make(map[string][]map[string]interface{})
	d.schemas[dbName] = make(map[string]map[string]interface{})
	return nil
}

// DropDatabase 删除数据库
func (d *MockDuckDBClient) DropDatabase(ctx context.Context, dbName string) error {
	d.logOperation("DropDatabase", dbName)

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.tables[dbName] == nil {
		return fmt.Errorf("database %s does not exist", dbName)
	}

	delete(d.tables, dbName)
	delete(d.schemas, dbName)
	return nil
}

// ListTables 列出表
func (d *MockDuckDBClient) ListTables(ctx context.Context, dbName string) ([]string, error) {
	d.logOperation("ListTables", dbName)

	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.tables[dbName] == nil {
		return nil, fmt.Errorf("database %s does not exist", dbName)
	}

	tables := make([]string, 0, len(d.tables[dbName]))
	for table := range d.tables[dbName] {
		tables = append(tables, table)
	}
	return tables, nil
}

// TableExists 检查表是否存在
func (d *MockDuckDBClient) TableExists(ctx context.Context, dbName, tableName string) (bool, error) {
	d.logOperation("TableExists", fmt.Sprintf("%s/%s", dbName, tableName))

	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.tables[dbName] == nil {
		return false, fmt.Errorf("database %s does not exist", dbName)
	}

	_, exists := d.tables[dbName][tableName]
	return exists, nil
}

// GetTableSchema 获取表结构
func (d *MockDuckDBClient) GetTableSchema(ctx context.Context, dbName, tableName string) (map[string]interface{}, error) {
	d.logOperation("GetTableSchema", fmt.Sprintf("%s/%s", dbName, tableName))

	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.tables[dbName] == nil {
		return nil, fmt.Errorf("database %s does not exist", dbName)
	}

	schema, exists := d.schemas[dbName][tableName]
	if !exists {
		return nil, fmt.Errorf("table %s does not exist in database %s", tableName, dbName)
	}

	result := make(map[string]interface{})
	for k, v := range schema {
		result[k] = v
	}
	return result, nil
}

// GetOperationLog 获取操作日志
func (d *MockDuckDBClient) GetOperationLog() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	log := make([]string, len(d.operationLog))
	copy(log, d.operationLog)
	return log
}

// GetLastOperation 获取最后一次操作
func (d *MockDuckDBClient) GetLastOperation() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastOperation
}

// GetQueryLog 获取查询日志
func (d *MockDuckDBClient) GetQueryLog() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	log := make([]string, len(d.queryLog))
	copy(log, d.queryLog)
	return log
}

// GetConnections 获取连接数
func (d *MockDuckDBClient) GetConnections() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.connections
}

// GetTableData 获取表数据（测试用）
func (d *MockDuckDBClient) GetTableData(dbName, tableName string) []map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.tables[dbName] == nil {
		return nil
	}

	data := d.tables[dbName][tableName]
	result := make([]map[string]interface{}, len(data))
	for i, row := range data {
		result[i] = make(map[string]interface{})
		for k, v := range row {
			result[i][k] = v
		}
	}
	return result
}

// execCreateTable 执行创建表
func (d *MockDuckDBClient) execCreateTable(query string) (sql.Result, error) {
	// Simple CREATE TABLE parsing - extract table name and columns
	query = strings.TrimSpace(query)
	parts := strings.Fields(query)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid CREATE TABLE statement")
	}

	tableName := parts[2]
	// Find the default database (first one)
	var dbName string
	for db := range d.tables {
		dbName = db
		break
	}

	if dbName == "" {
		dbName = "memory"
		if d.tables[dbName] == nil {
			d.tables[dbName] = make(map[string][]map[string]interface{})
			d.schemas[dbName] = make(map[string]map[string]interface{})
		}
	}

	// Create empty table with basic schema
	d.tables[dbName][tableName] = []map[string]interface{}{}
	d.schemas[dbName][tableName] = map[string]interface{}{
		"columns": []string{"id", "data"},
		"types":   map[string]string{"id": "INTEGER", "data": "TEXT"},
	}

	return &MockResult{}, nil
}

// execInsert 执行插入
func (d *MockDuckDBClient) execInsert(query string, args ...interface{}) (sql.Result, error) {
	// Simple INSERT parsing - extract table name and values
	query = strings.TrimSpace(query)
	parts := strings.Fields(query)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid INSERT statement")
	}

	tableName := parts[2]
	// Find the default database
	var dbName string
	for db := range d.tables {
		dbName = db
		break
	}

	if dbName == "" {
		return nil, fmt.Errorf("no database found")
	}

	// Create a simple row with provided args
	row := make(map[string]interface{})
	if len(args) > 0 {
		row["id"] = args[0]
	}
	if len(args) > 1 {
		row["data"] = args[1]
	}

	d.tables[dbName][tableName] = append(d.tables[dbName][tableName], row)
	return &MockResult{RowsAffectedCount: 1}, nil
}

// execUpdate 执行更新
func (d *MockDuckDBClient) execUpdate(query string, args ...interface{}) (sql.Result, error) {
	// Simple UPDATE parsing - very basic implementation
	return &MockResult{RowsAffectedCount: 1}, nil
}

// execDelete 执行删除
func (d *MockDuckDBClient) execDelete(query string, args ...interface{}) (sql.Result, error) {
	// Simple DELETE parsing - very basic implementation
	return &MockResult{RowsAffectedCount: 1}, nil
}

// execSelect 执行查询
func (d *MockDuckDBClient) execSelect(query string, args ...interface{}) (*sql.Rows, error) {
	// Simple SELECT parsing - return mock data
	return nil, nil
}

// logOperation 记录操作
func (d *MockDuckDBClient) logOperation(operation, target string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.lastOperation = fmt.Sprintf("%s: %s", operation, target)
	d.operationLog = append(d.operationLog, d.lastOperation)
}

// logQuery 记录查询
func (d *MockDuckDBClient) logQuery(query string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.queryLog = append(d.queryLog, query)
}

// MockResult 是sql.Result的Mock实现
type MockResult struct {
	LastInsertID int64
	RowsAffectedCount int64
}

func (r *MockResult) LastInsertId() (int64, error) {
	return r.LastInsertID, nil
}

func (r *MockResult) RowsAffected() (int64, error) {
	return r.RowsAffectedCount, nil
}

// MockRows 是sql.Rows的Mock实现
type MockRows struct {
	columns []string
	data    []map[string]interface{}
	current int
}

func (r *MockRows) Close() error {
	return nil
}

func (r *MockRows) Next() bool {
	if r.current < len(r.data) {
		r.current++
		return true
	}
	return false
}

func (r *MockRows) Scan(dest ...interface{}) error {
	if r.current > len(r.data) || r.current <= 0 {
		return fmt.Errorf("no row to scan")
	}

	row := r.data[r.current-1]
	for i := 0; i < len(dest) && i < len(r.columns); i++ {
		if val, exists := row[r.columns[i]]; exists {
			switch v := val.(type) {
			case int:
				dest[i] = interface{}(v)
			case int64:
				dest[i] = interface{}(v)
			case string:
				dest[i] = interface{}(v)
			case float64:
				dest[i] = interface{}(v)
			case bool:
				dest[i] = interface{}(v)
			default:
				dest[i] = interface{}(nil)
			}
		}
	}
	return nil
}

func (r *MockRows) Columns() ([]string, error) {
	return r.columns, nil
}

func (r *MockRows) ColumnTypes() ([]*sql.ColumnType, error) {
	// Mock column types - return empty for simplicity
	return []*sql.ColumnType{}, nil
}

func (r *MockRows) NextResultSet() bool {
	// Mock implementation - only one result set
	return false
}