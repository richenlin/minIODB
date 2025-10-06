"""
MinIODB Python SDK

官方 Python 客户端库，用于与 MinIODB 服务交互。
"""

from .client import MinIODBClient, MinIODBSyncClient
from .config import MinIODBConfig, AuthConfig, ConnectionConfig, LoggingConfig
from .models import DataRecord, TableConfig
from .exceptions import (
    MinIODBException,
    MinIODBConnectionException,
    MinIODBAuthenticationException,
    MinIODBRequestException,
    MinIODBServerException,
    MinIODBTimeoutException
)

__version__ = "1.0.0"
__author__ = "MinIODB Team"
__email__ = "team@miniodb.com"

__all__ = [
    # 客户端
    "MinIODBClient",
    "MinIODBSyncClient",
    
    # 配置
    "MinIODBConfig",
    "AuthConfig", 
    "ConnectionConfig",
    "LoggingConfig",
    
    # 模型
    "DataRecord",
    "TableConfig",
    
    # 异常
    "MinIODBException",
    "MinIODBConnectionException",
    "MinIODBAuthenticationException", 
    "MinIODBRequestException",
    "MinIODBServerException",
    "MinIODBTimeoutException",
]
