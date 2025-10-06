package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/logger"

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

// TestTransportStructures REST传输层结构测试
func TestTransportStructures(t *testing.T) {
	// 测试golint建议的边缘修复：结构定义确保正确性
	systemConfig := &config.SystemConfig{
		Version:       "1.0.0",
		Architecture:  "linux-amd64",
		StartupTime:   time.Now(),
		ShutdownTime:  time.Now().Add(time.Hour),
		License:       "GPL-v3.0",
	}

	assert.Equal(t, "1.0.0", systemConfig.Version)
	assert.Equal(t, "linux-amd64", systemConfig.Architecture)
	assert.NotNil(t, systemConfig.StartupTime)
	assert.NotNil(t, systemConfig.ShutdownTime)
}

// TestWriteRequest REST写入请求测试
func TestWriteRequest(t *testing.T) {
	request := &WriteRequest{
		Table: "users",
		ID:    "user-123",
		Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		Payload: map[string]interface{}{
			"name": "John Doe",
			"email": "john@example.com",
			"age":  30,
		},
	}

	assert.Equal(t, "users", request.Table)
	assert.Equal(t, "user-123", request.ID)
	assert.Equal(t, time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC), request.Timestamp)
	assert.NotNil(t, request.Payload)
	assert.Equal(t, "John Doe", request.Payload["name"])
}

// TestWriteResponse REST写入响应测试
func TestWriteResponse(t *testing.T) {
	resp := &WriteResponse{
		ID:        "resp-001",
		Table:     "orders",
		Status:    "success",
		Message:   "Order created successfully",
		Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		MessageID: "msg-123",
	}

	assert.Equal(t, "resp-001", resp.ID)
	assert.Equal(t, "orders", resp.Table)
	assert.Equal(t, "success", resp.Status)
	assert.Equal(t, "Order created successfully", resp.Message)
	assert.NotNil(t, resp.Timestamp)
	assert.Equal(t, "msg-123", resp.MessageID)
}

// TestHTTPHeaders HTTP头部测试
func TestHTTPHeaders(t *testing.T) {
	router := gin.New()

	router.GET("/api/v1/headers", func(c *gin.Context) {
		c.Header("X-Custom-Header", "custom-value")
		c.Header("Cache-Control", "no-cache")
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/headers", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "custom-value", w.Body.String())
	// 注意：这里content-type header在 JSON 响应时由 gin 自动设置
	assert.Contains(t, w.HeaderMap.Get("Content-Type"), "application/json")
	assert.Equal(t, "no-cache", w.Headers().Get("Cache-Control"))
}

// TestRESTEndpoints REST端点路径测试
func TestRESTEndpoints(t *testing.T) {
	router := gin.New()

	// 模拟REST端点进行测试
	endpoints := []string{
		"/api/v1/health",
		"/api/v1/write",
		"/api/v1/read",
		"/api/v1/query",
		"/api/v1/tables",
		"/api/v1/invalidate-cache",
	}

	// 验证所有URL路径的格式
	for _, endpoint := range endpoints {
		assert.NotEmpty(t, endpoint)
		assert.True(t, strings.HasPrefix(endpoint, "/api/v1/"), "Endpoint should start with /api/v1/")
		assert.True(t, len(endpoint) > len("/api/v1/"), "Endpoint should have path component")
	}
}

// TestContentTypes 内容类型测试
func TestContentTypes(t *testing.T) {
	router := gin.New()

	router.GET("/api/v1/types", func(c *gin.Context) {
		contentType := c.GetHeader("Accept")

		switch {
		case strings.Contains(contentType, "application/xml"):
			c.XML(http.StatusOK, gin.H{"status": "xml", "timestamp": time.Now().Format(time.RFC3339)})
		case strings.Contains(contentType, "text/plain"):
			c.String(http.StatusOK, "OK: %s\n", time.Now().Format(time.RFC3339))
		default: // application/json or any other
			c.JSON(http.StatusOK, gin.H{"status": "json", "timestamp": time.Now().Format(time.RFC3339)})
		}
	})

	// 测试 JSON 响应
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/api/v1/types", nil)
	req1.Header.Set("Accept", "application/json")
	router.ServeHTTP(w1, req1)

	assert.Equal(t, http.StatusOK, w1.Code)
	var jsonResp map[string]interface{}
	json.Unmarshal(w1.Body.Bytes(), &jsonResp)
	assert.Equal(t, "json", jsonResp["status"])

	// 测试默认 JSON 响应（没有 Accept 头）
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/v1/types", nil)
	router.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)
	var defaultResp map[string]interface{}
	json.Unmarshal(w2.Body.Bytes(), &defaultResp)
	assert.Equal(t, "json", defaultResp["status"])
}

// TestErrorResponses 错误响应测试
func TestErrorResponses(t *testing.T) {
	router := gin.New()

	router.GET("/api/v1/error/:code", func(c *gin.Context) {
		codeStr := c.Param("code")

		switch codeStr {
		case "400":
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Bad Request",
				"code":  400,
				"message": "The request is malformed",
			})
		case "404":
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Not Found",
				"code":  404,
				"message": "The requested resource was not found",
			})
		case "500":
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Internal Server Error",
				"code":  500,
				"message": "An internal error occurred",
			})
		default:
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		}
	})

	// 检查不同的错误状态码被正确处理
	testCases := []struct {
		path     string
		code     int
		errorMsg string
	}{
		{"/api/v1/error/400", 400, "Bad Request"},
		{"/api/v1/error/404", 404, "Not Found"},
		{"/api/v1/error/500", 500, "Internal Server Error"},
		{"/api/v1/error/200", 200, ""},
	}

	for _, tc := range testCases {
		t.Run("Error+"+tc.errorMsg, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", tc.path, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tc.code, w.Code)

			if tc.code != http.StatusOK {
				var errResp map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &errResp)
				assert.Equal(t, tc.errorMsg, errResp["error"])
				assert.Contains(t, errResp["message"], "The")
			}
		})
	}
}

// TestResponseTimes 响应时间测试
func TestResponseTimes(t *testing.T) {
	router := gin.New()

	router.Use(func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)
		c.Header("X-Response-Time", fmt.Sprintf("%.dms", duration.Round(time.Millisecond)))
	})

	router.GET("/api/v1/timed", func(c *gin.Context) {
		time.Sleep(50 * time.Millisecond) // 模拟处理时间
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 测试响应时间获取（简化检查）
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/timed", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Headers().Get("X-Response-Time"))

	// 验证JSON响应成功
	var jsonResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &jsonResp)
	assert.Equal(t, "ok", jsonResp["status"])
}

// TestRequestValidation 请求验证测试
func TestRequestValidation(t *testing.T) {
	router := gin.New()

	router.POST("/api/v1/validate", func(c *gin.Context) {
		var body struct {
			Table string                 `json:"table" binding:"required"`
			Data  map[string]interface{} `json:"data" binding:"required"`
		}

		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Validation failed",
				"code":  400,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Data validated successfully",
			"table":   body.Table,
		})
	})

	// 测试有效请求
	w1 := httptest.NewRecorder()
	validBody := `{"table":"users","data":{"name":"John","age":30}}`
	req1, _ := http.NewRequest("POST", "/api/v1/validate", bytes.NewBufferString(validBody))
	req1.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	// 测试无效请求 - 缺少必填字段
	w2 := httptest.NewRecorder()
	invalidBody := `{"table":""}` // 空table
	req2, _ := http.NewRequest("POST", "/api/v1/validate", bytes.NewBufferString(invalidBody))
	req2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusBadRequest, w2.Code)

	var errorResp map[string]interface{}
	json.Unmarshal(w2.Body.Bytes(), &errorResp)
	assert.Equal(t, "Validation failed", errorResp["error"])
}

// TestPagination 分页测试
func TestPagination(t *testing.T) {
	router := gin.New()

	// 模拟数据
	allItems := make([]int, 100)
	for i := 0; i < 100; i++ {
		allItems[i] = i + 1
	}

	router.GET("/api/v1/items", func(c *gin.Context) {
		page, _ := strNumconv.AtoiFallback(c.DefaultQuery("page", "1"))
		limit, _ := strNumconv.AtoiFallback(c.DefaultQuery("limit", "10"))

		if page < 1 {
			page = 1
		}
		if limit < 1 || limit > 50 {
			limit = 10
		}

		offset := (page - 1) * limit
		total := len(allItems)

		end := offset + limit
		if end > total {
			end = total
		}

		items := allItems[offset:end]
		totalPages := (total + limit - 1) / limit

		c.JSON(http.StatusOK, gin.H{
			"items": items,
			"page": page,
			"limit": limit,
			"total": total,
			"total_pages": totalPages,
		})
	})

	// 测试第一页
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/items?page=1&limit=5", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var pageResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &pageResp)

	assert.Equal(t, "1.0", fmt.Sprintf("%.0f", pageResp["page"]))
	assert.Equal(t, "5.0", fmt.Sprintf("%.0f", pageResp["limit"]))
	assert.Equal(t, "100.0", fmt.Sprintf("%.0f", pageResp["total"]))
	assert.Equal(t, "20.0", fmt.Sprintf("%.0f", pageResp["total_pages"]))
}

// sync struct for advanced URL validation testing
type ValidationTestCase struct {
	Name     string
	URLParts []string
	Expected bool
}

// TestAdvancedURLValidation 高级URL验证测试
func TestAdvancedURLValidation(t *testing.T) {
	validationCases := []ValidationTestCase{
		{"Valid nested structure", []string{"api", "v1", "users", "123", "orders"}, true},
		{"Valid with limit parameter", []string{"api", "v1", "products", "limit"}, true},
		{"Invalid double-slash", []string{"api//v1", "products"}, false},
		{"Valid with specific operation", []string{"api", "v1", "tables", "products", "validate"}, true},
		{"Invalid empty segment", []string{"api", "v1", "", "tables"}, false},
		{"Valid complex path", []string{"api", "v1", "analytics", "tables", "daily", "revenue", "2023-10-06"}, true},
	}

	for _, tc := range validationCases {
		t.Run(tc.Name, func(t *testing.T) {
			// 使用验证函数（简化版）
			parts := tc.URLParts
			isValid := len(parts) > 2 && parts[0] == "api" && parts[1] == "v1" && "api//v1" == parts[2] == false && "" != parts[len(parts)-1]
			// log.Printf("Actual validation result for %s: %v", tc.Name, isValid)
			assert.Equal(t, tc.Expected, isValid, tc.Name)
		})
	}
}