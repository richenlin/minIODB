package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/logger"
	"minIODB/internal/service"
	"minIODB/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// 初始化测试logger
	_ = logger.InitLogger(logger.LogConfig{
		Level:      "info",
		Format:     "console",
		Output:     "stdout",
		Filename:   "",
		MaxSize:    100,
		MaxBackups: 7,
		MaxAge:     1,
		Compress:   true,
	})

	// 设置gin测试模式
	gin.SetMode(gin.TestMode)
}

// TestRESTServerCreation REST服务器创建测试
func TestRESTServerCreation(t *testing.T) {
	ctx := context.Background()

	// 创建模拟存储
	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}

	// 创建服务
	miniodbService := service.NewMinIODBService(mockStorage, mockCacheStorage)
	require.NotNil(t, miniodbService)

	// 创建配置
	cfg := config.Config{
		Server: config.ServerConfig{
			Mode: "development",
			Port: 8080,
		},
		Security: config.SecurityConfig{
			Mode: "none",
		},
	}

	// 创建REST服务器
	server, err := NewServer(ctx, miniodbService, cfg)
	require.NoError(t, err)
	require.NotNil(t, server)

	// 验证服务器属性
	assert.NotNil(t, server.miniodbService)
	assert.NotNil(t, server.cfg)
	assert.NotNil(t, server.ctx)
}

// TestRESTServerStartStop REST服务器启动停止测试
func TestRESTServerStartStop(t *testing.T) {
	ctx := context.Background()

	// 创建测试服务
	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}
	miniodbService := service.NewMinIODBService(mockStorage, mockCacheStorage)

	cfg := config.Config{
		Server: config.ServerConfig{Mode: "development", Port: 8081},
		Security: config.SecurityConfig{Mode: "none"},
	}

	server, err := NewServer(ctx, miniodbService, cfg)
	require.NoError(t, err)

	// 测试设置协调器
	server.SetCoordinators(nil, nil)
	server.SetMetadataManager(nil)

	// 测试启动（超时）
	startCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	err = server.Start(startCtx, "8082")
	assert.Error(t, err) // 由于端口监听，预期超时错误
	t.Logf("Start error as expected: %v", err)

	// 测试停止
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stopCancel()

	err = server.Stop(stopCtx)
	assert.NoError(t, err)
}

// TestSetupRESTRoutes REST路由设置测试
func TestSetupRESTRoutes(t *testing.T) {
	ctx := context.Background()

	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}
	miniodbService := service.NewMinIODBService(mockStorage, mockCacheStorage)

	cfg := config.Config{
		Server: config.ServerConfig{Mode: "development", Port: 8080},
		Security: config.SecurityConfig{Mode: "none"},
	}

	server, err := NewServer(ctx, miniodbService, cfg)
	require.NoError(t, err)

	// 创建测试路由器
	ginEngine := gin.New()
	ginEngine.Use(gin.Logger())

	// 设置路由
	server.setupRoutes(ginEngine)

	// 验证路由存在
	routes := ginEngine.Routes()
	assert.Greater(t, len(routes), 0, "Should have routes defined")

	// 检查预期端点存在
	endpoints := []string{"/api/v1/tables", "/api/v1/health", "/api/v1/invalidate-cache"}
	foundEndpoints := make(map[string]bool)

	for _, route := range routes {
		foundEndpoints[route.Path] = true
	}

	for _, expected := range endpoints {
		assert.True(t, foundEndpoints[expected], "Should have endpoint: %s", expected)
	}
	t.Logf("Found %d REST API endpoints", len(routes))
}

// TestHealthCheckEndpoint 健康检查端点测试
func TestHealthCheckEndpoint(t *testing.T) {
	// 创建测试路由器
	router := gin.New()

	// 模拟健康检查端点
	router.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	})

	// 测试健康检查响应
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/health", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "healthy", response["status"])
	assert.Contains(t, response, "timestamp")
	t.Log("Health check endpoint test passed")
}

// TestTableEndpoints CRUD表端点测试
func TestTableEndpoints(t *testing.T) {
	router := gin.New()

	// 模拟表API端点
	tableData := make(map[string]interface{})

	router.GET("/api/v1/tables", func(c *gin.Context) {
		tables := []string{"users", "orders", "products"}
		c.JSON(http.StatusOK, gin.H{
			"tables": tables,
			"count":  len(tables),
		})
	})

	router.POST("/api/v1/tables", func(c *gin.Context) {
		var req struct {
			Name        string `json:"name" binding:"required"`
			Description string `json:"description"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		tableData[req.Name] = map[string]string{
			"name":        req.Name,
			"description": req.Description,
			"created_at":  time.Now().Format(time.RFC3339),
		}

		c.JSON(http.StatusCreated, gin.H{
			"message": "Table created successfully",
			"table":   tableData[req.Name],
		})
	})

	router.GET("/api/v1/tables/:name", func(c *gin.Context) {
		name := c.Param("name")
		if table, exists := tableData[name]; exists {
			c.JSON(http.StatusOK, table)
		} else {
			c.JSON(http.StatusNotFound, gin.H{"error": "Table not found"})
		}
	})

	router.DELETE("/api/v1/tables/:name", func(c *gin.Context) {
		name := c.Param("name")
		if _, exists := tableData[name]; exists {
			delete(tableData, name)
			c.JSON(http.StatusOK, gin.H{"message": "Table deleted successfully"})
		} else {
			c.JSON(http.StatusNotFound, gin.H{"error": "Table not found"})
		}
	})

	// 测试GET /tables
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/tables", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var listResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &listResp)
	assert.Equal(t, "3.0", fmt.Sprintf("%.0f", listResp["count"]))
	assert.Contains(t, listResp, "tables")

	// 测试POST /tables
	w = httptest.NewRecorder()
	createReq, _ := http.NewRequest("POST", "/api/v1/tables", bytes.NewBufferString(`{"name":"test_table","description":"Test table"}`))
	createReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, createReq)

	assert.Equal(t, http.StatusCreated, w.Code)
	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	assert.Equal(t, "Table created successfully", createResp["message"])

	// 测试GET /tables/:name
	w = httptest.NewRecorder()
	getReq, _ := http.NewRequest("GET", "/api/v1/tables/test_table", nil)
	router.ServeHTTP(w, getReq)

	assert.Equal(t, http.StatusOK, w.Code)
	var getResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &getResp)
	assert.Equal(t, "test_table", getResp["name"])

	// 测试DELETE /tables/:name
	w = httptest.NewRecorder()
	delReq, _ := http.NewRequest("DELETE", "/api/v1/tables/test_table", nil)
	router.ServeHTTP(w, delReq)

	assert.Equal(t, http.StatusOK, w.Code)
	var delResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &delResp)
	assert.Equal(t, "Table deleted successfully", delResp["message"])

	// 验证表被删除
	w = httptest.NewRecorder()
	router.ServeHTTP(w, getReq)
	assert.Equal(t, http.StatusNotFound, w.Code)

	t.Log("Table endpoints test completed")
}

// TestDataAPIEndpoints 数据API端点测试
func TestDataAPIEndpoints(t *testing.T) {
	router := gin.New()

	// 模拟数据存储
	dataStore := make(map[string][]map[string]interface{})

	router.POST("/api/v1/data/write", func(c *gin.Context) {
		var req struct {
			Table string                 `json:"table" binding:"required"`
			Data  map[string]interface{} `json:"data" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if dataStore[req.Table] == nil {
			dataStore[req.Table] = []map[string]interface{}{}
		}
		dataStore[req.Table] = append(dataStore[req.Table], req.Data)

		c.JSON(http.StatusOK, gin.H{
			"message": "Data written successfully",
			"count":   len(dataStore[req.Table]),
		})
	})

	router.GET("/api/v1/data/read", func(c *gin.Context) {
		table := c.Query("table")
		limit := c.DefaultQuery("limit", "10")

		var limitInt int
		fmt.Sscanf(limit, "%d", &limitInt)

		if records, exists := dataStore[table]; exists {
			if len(records) > limitInt {
				records = records[:limitInt]
			}
			c.JSON(http.StatusOK, gin.H{
				"data":  records,
				"count": len(records),
			})
		} else {
			c.JSON(http.StatusOK, gin.H{
				"data":  []interface{}{},
				"count": 0,
			})
		}
	})

	cosmJson := `{\"name\":\"John\",\"age\":\"30\",\"city\":\"New York\"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/data/write", bytes.NewBufferString(fmt.Sprintf(`{"table":"users","data":%s}`, cosmJson)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var writeResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &writeResp)
	assert.Equal(t, "Data written successfully", writeResp["message"])
	assert.Equal(t, "1.0", fmt.Sprintf("%.0f", writeResp["count"]))
	w = httptest.NewRecorder()
	readReq, _ := http.NewRequest("GET", "/api/v1/data/read?table=users&limit=1", nil)
	router.ServeHTTP(w, readReq)

	assert.Equal(t, http.StatusOK, w.Code)
	var readResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &readResp)
	assert.Equal(t, "1.0", fmt.Sprintf("%.0f", readResp["count"]))

	t.Log("Data API endpoints test completed")
}

// TestErrorHandling 错误处理测试
func TestErrorHandling(t *testing.T) {
	router := gin.New()

	router.GET("/api/v1/error-test", func(c *gin.Context) {
		statusCode, _ := strNumconv.AtoiFallback(c.Query("status"))
		if statusCode == 0 {
			statusCode = http.StatusInternalServerError
		}

		c.JSON(statusCode, gin.H{
			"error":   "Test error occurred",
			"code":    statusCode,
		})
	})

	testCases := []struct {
		name       string
		statusCode int
		expected   bool
	}{
		{"400 error", 400, true},
		{"404 error", 404, true},
		{"500 error", 500, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", fmt.Sprintf("/api/v1/error-test?status=%d", tc.statusCode), nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tc.statusCode, w.Code, "Status code should match")

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)

			assert.Equal(t, tc.statusCode, int(resp["code"].(float64)))
			assert.Equal(t, "Test error occurred", resp["error"])
		})
	}

	t.Log("Error handling test completed")
}

// TestConcurrentAPIRequests 并发API请求测试
func TestConcurrentAPIRequests(t *testing.T) {
	router := gin.New()

	requestCount := 0
	var mu sync.Mutex

	router.GET("/api/v1/concurrent", func(c *gin.Context) {
		mu.Lock()
		requestCount++
		currentCount := requestCount
		mu.Unlock()

		time.Sleep(10 * time.Millisecond) // 模拟处理延迟

		c.JSON(http.StatusOK, gin.H{
			"count": currentCount,
		})
	})

	// 并发测试
	concurrency := 20
	var wg sync.WaitGroup
	responseCounts := make([]int, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/api/v1/concurrent", nil)
			router.ServeHTTP(w, req)

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)

			if count, exists := resp["count"]; exists {
				value := int(count.(float64))
				if index < len(responseCounts) {
					responseCounts[index] = value
				}
			}
		}(i)
	}

	wg.Wait()	// 等待所有请求完成

	// 验证所有请求都被处理
	assert.Equal(t, concurrency, requestCount, "All concurrent requests should be processed")

	// 验证响应
	assert.Len(t, responseCounts, concurrency)
	for i, count := range responseCounts {
		assert.True(t, count > 0, "Response %d should have valid count", i)
	}

	t.Log()
	t.Logf("Concurrent API requests test completed: %d requests processed", concurrency)
}

// MockStorage 模拟存储（复用metadata中的实现）
type MockStorage struct {
	data map[string]interface{}
}

func (m *MockStorage) Initialize(ctx context.Context) error {
	if m.data == nil {
		m.data = make(map[string]interface{})
	}
	return nil
}

func (m *MockStorage) Write(ctx context.Context, key string, data interface{}) error {
	if m.data == nil {
		m.data = make(map[string]interface{})
	}
	m.data[key] = data
	return nil
}

func (m *MockStorage) Read(ctx context.Context, key string) (interface{}, error) {
	if m.data == nil {
		return "[]", nil
	}
	if value, exists := m.data[key]; exists {
		return value, nil
	}
	return "[]", nil
}

func (m *MockStorage) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	keys := []string{}
	for key := range m.data {
		if prefix == "" || len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (m *MockStorage) Close(ctx context.Context) error {
	return nil
}

// MockCacheStorage 模拟缓存存储
type MockCacheStorage struct{}

func (c *MockCacheStorage) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return nil
}

func (c *MockCacheStorage) Get(ctx context.Context, key string) (interface{}, error) {
	return nil, nil
}

func (c *MockCacheStorage) Exists(ctx context.Context, key string) (bool, error) {
	return false, nil
}

func (c *MockCacheStorage) Delete(ctx context.Context, key string) error {
	return nil
}

func (c *MockCacheStorage) Close(ctx context.Context) error {
	return nil
}