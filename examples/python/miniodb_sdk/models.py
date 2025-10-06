"""
MinIODB Python SDK 数据模型

定义了与 MinIODB 交互所需的数据模型。
"""

from dataclasses import dataclass, field
from datetime import datetime
from typing import Dict, Any, Optional, List
from uuid import uuid4


@dataclass
class DataRecord:
    """
    数据记录模型
    
    表示 MinIODB 中的一条数据记录。
    """
    id: str
    timestamp: datetime
    payload: Dict[str, Any]
    
    def __post_init__(self):
        """验证数据"""
        if not self.id:
            raise ValueError("记录 ID 不能为空")
        if not isinstance(self.payload, dict):
            raise ValueError("数据负载必须是字典类型")
    
    @classmethod
    def create(
        cls, 
        payload: Dict[str, Any], 
        record_id: Optional[str] = None,
        timestamp: Optional[datetime] = None
    ) -> 'DataRecord':
        """
        创建数据记录
        
        Args:
            payload: 数据负载
            record_id: 记录ID，如果不提供则自动生成
            timestamp: 时间戳，如果不提供则使用当前时间
            
        Returns:
            DataRecord 实例
        """
        return cls(
            id=record_id or str(uuid4()),
            timestamp=timestamp or datetime.now(),
            payload=payload
        )


@dataclass
class TableConfig:
    """
    表配置模型
    
    表示 MinIODB 中表的配置信息。
    """
    buffer_size: int = 1000
    flush_interval_seconds: int = 30
    retention_days: int = 365
    backup_enabled: bool = False
    properties: Optional[Dict[str, str]] = None
    
    def __post_init__(self):
        """验证配置"""
        if self.buffer_size <= 0:
            raise ValueError("缓冲区大小必须大于 0")
        if self.flush_interval_seconds <= 0:
            raise ValueError("刷新间隔必须大于 0")
        if self.retention_days <= 0:
            raise ValueError("保留天数必须大于 0")


@dataclass
class TableStats:
    """
    表统计信息模型
    """
    record_count: int = 0
    file_count: int = 0
    size_bytes: int = 0
    oldest_record: Optional[datetime] = None
    newest_record: Optional[datetime] = None


@dataclass
class TableInfo:
    """
    表信息模型
    """
    name: str
    config: TableConfig
    created_at: datetime
    last_write: Optional[datetime] = None
    status: str = "active"
    stats: Optional[TableStats] = None


@dataclass
class BackupInfo:
    """
    备份信息模型
    """
    object_name: str
    node_id: str
    timestamp: datetime
    size: int
    last_modified: datetime


@dataclass
class NodeInfo:
    """
    节点信息模型
    """
    id: str
    status: str
    type: str
    address: str
    last_seen: int


# 响应模型
@dataclass
class WriteDataResponse:
    """写入数据响应"""
    success: bool
    message: str
    node_id: Optional[str] = None


@dataclass
class QueryDataResponse:
    """查询数据响应"""
    result_json: str
    has_more: bool = False
    next_cursor: Optional[str] = None


@dataclass 
class UpdateDataResponse:
    """更新数据响应"""
    success: bool
    message: str
    node_id: Optional[str] = None


@dataclass
class DeleteDataResponse:
    """删除数据响应"""
    success: bool
    message: str
    deleted_count: int = 0


@dataclass
class StreamWriteResponse:
    """流式写入响应"""
    success: bool
    records_count: int
    errors: List[str] = field(default_factory=list)


@dataclass
class StreamQueryResponse:
    """流式查询响应"""
    records: List[DataRecord]
    has_more: bool = False
    cursor: Optional[str] = None


@dataclass
class CreateTableResponse:
    """创建表响应"""
    success: bool
    message: str


@dataclass
class ListTablesResponse:
    """列出表响应"""
    tables: List[TableInfo]
    total: int


@dataclass
class GetTableResponse:
    """获取表响应"""
    table_info: TableInfo


@dataclass
class DeleteTableResponse:
    """删除表响应"""
    success: bool
    message: str
    files_deleted: int = 0


@dataclass
class BackupMetadataResponse:
    """备份元数据响应"""
    success: bool
    message: str
    backup_id: str
    timestamp: datetime


@dataclass
class RestoreMetadataResponse:
    """恢复元数据响应"""
    success: bool
    message: str
    backup_file: str
    entries_total: int = 0
    entries_ok: int = 0
    entries_skipped: int = 0
    entries_error: int = 0
    duration: str = ""
    errors: List[str] = field(default_factory=list)
    details: Optional[Dict[str, str]] = None


@dataclass
class ListBackupsResponse:
    """列出备份响应"""
    backups: List[BackupInfo]
    total: int


@dataclass
class GetMetadataStatusResponse:
    """获取元数据状态响应"""
    node_id: str
    backup_status: Dict[str, str]
    last_backup: Optional[datetime] = None
    next_backup: Optional[datetime] = None
    health_status: str = "unknown"


@dataclass
class HealthCheckResponse:
    """健康检查响应"""
    status: str
    timestamp: datetime
    version: str
    details: Optional[Dict[str, str]] = None


@dataclass
class GetStatusResponse:
    """获取状态响应"""
    timestamp: datetime
    buffer_stats: Dict[str, int]
    redis_stats: Dict[str, int]
    minio_stats: Dict[str, int]
    nodes: List[NodeInfo]
    total_nodes: int


@dataclass
class GetMetricsResponse:
    """获取指标响应"""
    timestamp: datetime
    performance_metrics: Dict[str, float]
    resource_usage: Dict[str, int]
    system_info: Dict[str, str]


@dataclass
class GetTokenResponse:
    """获取令牌响应"""
    access_token: str
    refresh_token: str
    expires_in: int
    token_type: str = "Bearer"


@dataclass
class RefreshTokenResponse:
    """刷新令牌响应"""
    access_token: str
    refresh_token: str
    expires_in: int
    token_type: str = "Bearer"


@dataclass
class RevokeTokenResponse:
    """撤销令牌响应"""
    success: bool
    message: str
