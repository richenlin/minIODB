"""
MinIODB Python SDK 配置模块

包含客户端配置相关的类和函数。
"""

from dataclasses import dataclass, field
from datetime import timedelta
from typing import Optional, Dict, Any
from enum import Enum


class LogLevel(str, Enum):
    """日志级别枚举"""
    TRACE = "TRACE"
    DEBUG = "DEBUG"
    INFO = "INFO"
    WARN = "WARN"
    ERROR = "ERROR"
    OFF = "OFF"


class LogFormat(str, Enum):
    """日志格式枚举"""
    TEXT = "TEXT"
    JSON = "JSON"


@dataclass
class AuthConfig:
    """
    认证配置
    
    包含连接到 MinIODB 服务所需的认证信息。
    """
    api_key: str
    secret: str
    token_type: str = "Bearer"
    
    @property
    def is_enabled(self) -> bool:
        """检查是否启用了认证"""
        return bool(self.api_key and self.secret)


@dataclass
class ConnectionConfig:
    """
    连接配置
    
    包含 gRPC 连接相关的配置选项。
    """
    max_connections: int = 10
    timeout: timedelta = field(default_factory=lambda: timedelta(seconds=30))
    retry_attempts: int = 3
    keepalive_time: timedelta = field(default_factory=lambda: timedelta(minutes=5))
    keepalive_timeout: timedelta = field(default_factory=lambda: timedelta(seconds=10))
    keepalive_without_calls: bool = False
    max_receive_message_length: int = 4 * 1024 * 1024  # 4MB
    max_send_message_length: int = 4 * 1024 * 1024     # 4MB


@dataclass
class LoggingConfig:
    """
    日志配置
    
    包含客户端日志相关的配置选项。
    """
    level: LogLevel = LogLevel.INFO
    format: LogFormat = LogFormat.TEXT
    enable_request_logging: bool = False
    enable_response_logging: bool = False
    enable_performance_logging: bool = True


@dataclass
class MinIODBConfig:
    """
    MinIODB 客户端配置
    
    包含连接到 MinIODB 服务所需的所有配置选项。
    """
    host: str = "localhost"
    grpc_port: int = 8080
    rest_port: int = 8081
    auth: Optional[AuthConfig] = None
    connection: ConnectionConfig = field(default_factory=ConnectionConfig)
    logging: LoggingConfig = field(default_factory=LoggingConfig)
    
    @property
    def grpc_address(self) -> str:
        """获取 gRPC 服务器地址"""
        return f"{self.host}:{self.grpc_port}"
    
    @property
    def rest_base_url(self) -> str:
        """获取 REST API 基础 URL"""
        return f"http://{self.host}:{self.rest_port}"
    
    def to_dict(self) -> Dict[str, Any]:
        """转换为字典"""
        return {
            "host": self.host,
            "grpc_port": self.grpc_port,
            "rest_port": self.rest_port,
            "auth": {
                "api_key": "***" if self.auth and self.auth.api_key else None,
                "secret": "***" if self.auth and self.auth.secret else None,
                "token_type": self.auth.token_type if self.auth else None,
            } if self.auth else None,
            "connection": {
                "max_connections": self.connection.max_connections,
                "timeout": str(self.connection.timeout),
                "retry_attempts": self.connection.retry_attempts,
                "keepalive_time": str(self.connection.keepalive_time),
            },
            "logging": {
                "level": self.logging.level.value,
                "format": self.logging.format.value,
                "enable_request_logging": self.logging.enable_request_logging,
                "enable_response_logging": self.logging.enable_response_logging,
                "enable_performance_logging": self.logging.enable_performance_logging,
            }
        }
