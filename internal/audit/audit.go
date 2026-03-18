package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Action 审计操作类型
type Action string

const (
	ActionWrite  Action = "write"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

// AuditEntry 审计日志条目
type AuditEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Action    Action                 `json:"action"`    // write, update, delete
	Table     string                 `json:"table"`     // 表名
	RecordID  string                 `json:"record_id"` // 记录ID
	UserID    string                 `json:"user_id,omitempty"`
	ClientIP  string                 `json:"client_ip,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Success   bool                   `json:"success"`
	Error     string                 `json:"error,omitempty"`
	NodeID    string                 `json:"node_id"`            // 节点ID
	Duration  time.Duration          `json:"duration,omitempty"` // 操作耗时
}

// AuditLogger 审计日志记录器
type AuditLogger struct {
	logger   *zap.Logger
	file     *os.File
	filePath string
	mu       sync.Mutex
	enabled  bool
	nodeID   string
}

// Config 审计日志配置
type Config struct {
	Enabled  bool   // 是否启用审计日志
	FilePath string // 审计日志文件路径
	NodeID   string // 节点ID
}

// NewAuditLogger 创建新的审计日志记录器
func NewAuditLogger(cfg *Config, logger *zap.Logger) (*AuditLogger, error) {
	if !cfg.Enabled {
		return &AuditLogger{
			logger:  logger,
			enabled: false,
			nodeID:  cfg.NodeID,
		}, nil
	}

	// 确保目录存在
	dir := filepath.Dir(cfg.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	// 打开或创建日志文件
	f, err := os.OpenFile(cfg.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &AuditLogger{
		logger:   logger,
		file:     f,
		filePath: cfg.FilePath,
		enabled:  true,
		nodeID:   cfg.NodeID,
	}, nil
}

// Log 记录审计日志
func (a *AuditLogger) Log(entry *AuditEntry) {
	if !a.enabled {
		return
	}

	// 设置时间戳和节点ID
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if entry.NodeID == "" {
		entry.NodeID = a.nodeID
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// 序列化并写入文件
	data, err := json.Marshal(entry)
	if err != nil {
		a.logger.Error("Failed to marshal audit entry", zap.Error(err))
		return
	}

	if _, err := a.file.Write(append(data, '\n')); err != nil {
		a.logger.Error("Failed to write audit log", zap.Error(err))
	}
}

// LogWithContext 记录带有上下文的审计日志
func (a *AuditLogger) LogWithContext(ctx context.Context, action Action, table, recordID string, success bool, err error, details map[string]interface{}) {
	entry := &AuditEntry{
		Timestamp: time.Now(),
		Action:    action,
		Table:     table,
		RecordID:  recordID,
		Success:   success,
		Details:   details,
		NodeID:    a.nodeID,
	}

	if err != nil {
		entry.Error = err.Error()
	}

	// 从上下文中提取用户ID和客户端IP（如果存在）
	if userID, ok := ctx.Value("user_id").(string); ok {
		entry.UserID = userID
	}
	if clientIP, ok := ctx.Value("client_ip").(string); ok {
		entry.ClientIP = clientIP
	}

	a.Log(entry)
}

// LogWrite 记录写入操作
func (a *AuditLogger) LogWrite(ctx context.Context, table, recordID string, success bool, err error, details map[string]interface{}) {
	a.LogWithContext(ctx, ActionWrite, table, recordID, success, err, details)
}

// LogUpdate 记录更新操作
func (a *AuditLogger) LogUpdate(ctx context.Context, table, recordID string, success bool, err error, details map[string]interface{}) {
	a.LogWithContext(ctx, ActionUpdate, table, recordID, success, err, details)
}

// LogDelete 记录删除操作
func (a *AuditLogger) LogDelete(ctx context.Context, table, recordID string, success bool, err error, details map[string]interface{}) {
	a.LogWithContext(ctx, ActionDelete, table, recordID, success, err, details)
}

// Close 关闭审计日志记录器
func (a *AuditLogger) Close() error {
	if a.file != nil {
		return a.file.Close()
	}
	return nil
}

// IsEnabled 返回审计日志是否启用
func (a *AuditLogger) IsEnabled() bool {
	return a.enabled
}

// SetEnabled 设置审计日志启用状态
func (a *AuditLogger) SetEnabled(enabled bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.enabled = enabled
}

// GetFilePath 返回审计日志文件路径
func (a *AuditLogger) GetFilePath() string {
	return a.filePath
}
