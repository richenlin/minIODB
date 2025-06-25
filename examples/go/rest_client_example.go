package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

// 配置常量
const (
	defaultRESTHost = "http://localhost:8081"
	defaultJWTToken = "your-jwt-token-here"
)

// RESTClient MinIODB REST客户端
type RESTClient struct {
	baseURL    string
	jwtToken   string
	httpClient *http.Client
	logger     *logrus.Logger
}

// NewRESTClient 创建新的REST客户端
func NewRESTClient() *RESTClient {
	baseURL := os.Getenv("MINIODB_REST_HOST")
	if baseURL == "" {
		baseURL = defaultRESTHost
	}

	jwtToken := os.Getenv("MINIODB_JWT_TOKEN")
	if jwtToken == "" {
		jwtToken = defaultJWTToken
	}

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	return &RESTClient{
		baseURL:  baseURL,
		jwtToken: jwtToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// doRequest 执行HTTP请求
func (c *RESTClient) doRequest(method, endpoint string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %v", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(method, c.baseURL+endpoint, reqBody)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.jwtToken)

	c.logger.Infof("发送 %s 请求到 %s", method, endpoint)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP错误 %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// WriteRequest 数据写入请求结构
type WriteRequest struct {
	ID        string                 `json:"id"`
	Timestamp string                 `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

// WriteResponse 数据写入响应结构
type WriteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	NodeID  string `json:"node_id"`
}

// QueryRequest 查询请求结构
type QueryRequest struct {
	SQL string `json:"sql"`
}

// QueryResponse 查询响应结构
type QueryResponse struct {
	ResultJSON string `json:"result_json"`
}

// TriggerBackupRequest 备份请求结构
type TriggerBackupRequest struct {
	ID  string `json:"id"`
	Day string `json:"day"`
}

// TriggerBackupResponse 备份响应结构
type TriggerBackupResponse struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	FilesBackedUp int32  `json:"files_backed_up"`
}

// RecoverDataRequest 恢复请求结构
type RecoverDataRequest struct {
	TimeRange      *TimeRangeFilter `json:"time_range,omitempty"`
	IDRange        *IDRangeFilter   `json:"id_range,omitempty"`
	ForceOverwrite bool             `json:"force_overwrite"`
}

// TimeRangeFilter 时间范围过滤器
type TimeRangeFilter struct {
	StartDate string   `json:"start_date"`
	EndDate   string   `json:"end_date"`
	IDs       []string `json:"ids,omitempty"`
}

// IDRangeFilter ID范围过滤器
type IDRangeFilter struct {
	IDs       []string `json:"ids,omitempty"`
	IDPattern string   `json:"id_pattern,omitempty"`
}

// RecoverDataResponse 恢复响应结构
type RecoverDataResponse struct {
	Success        bool     `json:"success"`
	Message        string   `json:"message"`
	FilesRecovered int32    `json:"files_recovered"`
	RecoveredKeys  []string `json:"recovered_keys"`
}

// HealthResponse 健康检查响应
type HealthResponse struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Version   string            `json:"version"`
	Details   map[string]string `json:"details"`
}

// StatsResponse 统计响应
type StatsResponse struct {
	Timestamp   string            `json:"timestamp"`
	BufferStats map[string]int64  `json:"buffer_stats"`
	RedisStats  map[string]int64  `json:"redis_stats"`
	MinioStats  map[string]int64  `json:"minio_stats"`
}

// NodeInfo 节点信息
type NodeInfo struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	Address  string `json:"address"`
	LastSeen int64  `json:"last_seen"`
}

// NodesResponse 节点响应
type NodesResponse struct {
	Nodes []NodeInfo `json:"nodes"`
	Total int32      `json:"total"`
}

// HealthCheck 健康检查
func (c *RESTClient) HealthCheck() error {
	c.logger.Info("=== 健康检查 ===")

	respBody, err := c.doRequest("GET", "/v1/health", nil)
	if err != nil {
		return fmt.Errorf("健康检查失败: %v", err)
	}

	var response HealthResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("解析健康检查响应失败: %v", err)
	}

	c.logger.Infof("健康检查结果:")
	c.logger.Infof("  状态: %s", response.Status)
	c.logger.Infof("  时间: %s", response.Timestamp)
	c.logger.Infof("  版本: %s", response.Version)
	c.logger.Infof("  详情: %v", response.Details)

	return nil
}

// WriteData 写入数据
func (c *RESTClient) WriteData() error {
	c.logger.Info("=== 数据写入 ===")

	request := WriteRequest{
		ID:        "user123",
		Timestamp: time.Now().Format(time.RFC3339),
		Payload: map[string]interface{}{
			"user_id": "user123",
			"action":  "login",
			"score":   95.5,
			"success": true,
		},
	}

	respBody, err := c.doRequest("POST", "/v1/data", request)
	if err != nil {
		return fmt.Errorf("数据写入失败: %v", err)
	}

	var response WriteResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("解析写入响应失败: %v", err)
	}

	c.logger.Infof("数据写入结果:")
	c.logger.Infof("  成功: %v", response.Success)
	c.logger.Infof("  消息: %s", response.Message)
	c.logger.Infof("  节点ID: %s", response.NodeID)

	return nil
}

// QueryData 查询数据
func (c *RESTClient) QueryData() error {
	c.logger.Info("=== 数据查询 ===")

	sql := "SELECT COUNT(*) as total, AVG(score) as avg_score FROM table WHERE user_id = 'user123' AND timestamp >= '2024-01-01'"
	request := QueryRequest{SQL: sql}

	respBody, err := c.doRequest("POST", "/v1/query", request)
	if err != nil {
		return fmt.Errorf("数据查询失败: %v", err)
	}

	var response QueryResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("解析查询响应失败: %v", err)
	}

	c.logger.Infof("查询结果:")
	c.logger.Infof("  SQL: %s", sql)
	c.logger.Infof("  结果JSON: %s", response.ResultJSON)

	// 尝试解析结果JSON
	var resultData interface{}
	if err := json.Unmarshal([]byte(response.ResultJSON), &resultData); err == nil {
		prettyJSON, _ := json.MarshalIndent(resultData, "  ", "  ")
		c.logger.Infof("  解析后结果: %s", string(prettyJSON))
	} else {
		c.logger.Warnf("  JSON解析失败: %v", err)
	}

	return nil
}

// TriggerBackup 触发备份
func (c *RESTClient) TriggerBackup() error {
	c.logger.Info("=== 触发备份 ===")

	request := TriggerBackupRequest{
		ID:  "user123",
		Day: "2024-01-15",
	}

	respBody, err := c.doRequest("POST", "/v1/backup/trigger", request)
	if err != nil {
		return fmt.Errorf("触发备份失败: %v", err)
	}

	var response TriggerBackupResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("解析备份响应失败: %v", err)
	}

	c.logger.Infof("备份结果:")
	c.logger.Infof("  成功: %v", response.Success)
	c.logger.Infof("  消息: %s", response.Message)
	c.logger.Infof("  备份文件数: %d", response.FilesBackedUp)

	return nil
}

// RecoverData 恢复数据
func (c *RESTClient) RecoverData() error {
	c.logger.Info("=== 数据恢复 ===")

	request := RecoverDataRequest{
		TimeRange: &TimeRangeFilter{
			StartDate: "2024-01-01",
			EndDate:   "2024-01-15",
			IDs:       []string{"user123"},
		},
		ForceOverwrite: false,
	}

	respBody, err := c.doRequest("POST", "/v1/backup/recover", request)
	if err != nil {
		return fmt.Errorf("数据恢复失败: %v", err)
	}

	var response RecoverDataResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("解析恢复响应失败: %v", err)
	}

	c.logger.Infof("恢复结果:")
	c.logger.Infof("  成功: %v", response.Success)
	c.logger.Infof("  消息: %s", response.Message)
	c.logger.Infof("  恢复文件数: %d", response.FilesRecovered)
	c.logger.Infof("  恢复的键: %v", response.RecoveredKeys)

	return nil
}

// GetStats 获取统计信息
func (c *RESTClient) GetStats() error {
	c.logger.Info("=== 系统统计 ===")

	respBody, err := c.doRequest("GET", "/v1/stats", nil)
	if err != nil {
		return fmt.Errorf("获取统计信息失败: %v", err)
	}

	var response StatsResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("解析统计响应失败: %v", err)
	}

	c.logger.Infof("系统统计:")
	c.logger.Infof("  时间戳: %s", response.Timestamp)
	c.logger.Infof("  缓冲区统计: %v", response.BufferStats)
	c.logger.Infof("  Redis统计: %v", response.RedisStats)
	c.logger.Infof("  MinIO统计: %v", response.MinioStats)

	return nil
}

// GetNodes 获取节点信息
func (c *RESTClient) GetNodes() error {
	c.logger.Info("=== 节点信息 ===")

	respBody, err := c.doRequest("GET", "/v1/nodes", nil)
	if err != nil {
		return fmt.Errorf("获取节点信息失败: %v", err)
	}

	var response NodesResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("解析节点响应失败: %v", err)
	}

	c.logger.Infof("节点信息:")
	c.logger.Infof("  总数: %d", response.Total)
	c.logger.Infof("  节点列表:")

	for _, node := range response.Nodes {
		c.logger.Infof("    - ID: %s, 状态: %s, 类型: %s, 地址: %s, 最后活跃: %d",
			node.ID, node.Status, node.Type, node.Address, node.LastSeen)
	}

	return nil
}

// RunAllExamples 运行所有示例
func (c *RESTClient) RunAllExamples() {
	c.logger.Info("开始运行MinIODB Go REST客户端示例...")

	examples := []struct {
		name string
		fn   func() error
	}{
		{"健康检查", c.HealthCheck},
		{"数据写入", c.WriteData},
		{"数据查询", c.QueryData},
		{"触发备份", c.TriggerBackup},
		{"数据恢复", c.RecoverData},
		{"获取统计", c.GetStats},
		{"获取节点", c.GetNodes},
	}

	for _, example := range examples {
		if err := example.fn(); err != nil {
			c.logger.Errorf("%s失败: %v", example.name, err)
		}
		time.Sleep(500 * time.Millisecond) // 短暂延迟
	}

	c.logger.Info("所有示例运行完成!")
}

func main() {
	client := NewRESTClient()
	client.logger.Infof("REST客户端已配置，服务器地址: %s", client.baseURL)

	client.RunAllExamples()
} 