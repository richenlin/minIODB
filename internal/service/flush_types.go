package service

// FlushTableRequest 手动刷新表请求
type FlushTableRequest struct {
	TableName string `json:"table_name"`
}

// FlushTableResponse 手动刷新表响应
type FlushTableResponse struct {
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	RecordsFlushed int64  `json:"records_flushed"`
}
