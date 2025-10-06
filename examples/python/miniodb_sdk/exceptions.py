"""
MinIODB Python SDK 异常定义

定义了所有 MinIODB 客户端可能抛出的异常类型。
"""

from typing import Optional


class MinIODBException(Exception):
    """
    MinIODB 客户端异常基类
    
    所有 MinIODB 客户端异常的基类。
    """
    
    def __init__(
        self, 
        message: str, 
        error_code: str = "UNKNOWN", 
        status_code: int = -1,
        cause: Optional[Exception] = None
    ):
        super().__init__(message)
        self.message = message
        self.error_code = error_code
        self.status_code = status_code
        self.cause = cause
    
    def __str__(self) -> str:
        return f"MinIODBException(error_code='{self.error_code}', status_code={self.status_code}, message='{self.message}')"


class MinIODBConnectionException(MinIODBException):
    """
    MinIODB 连接异常
    
    当无法连接到 MinIODB 服务时抛出此异常。
    """
    
    def __init__(self, message: str, cause: Optional[Exception] = None):
        super().__init__(message, "CONNECTION_ERROR", 503, cause)


class MinIODBAuthenticationException(MinIODBException):
    """
    MinIODB 认证异常
    
    当认证失败或令牌过期时抛出此异常。
    """
    
    def __init__(self, message: str, cause: Optional[Exception] = None):
        super().__init__(message, "AUTHENTICATION_ERROR", 401, cause)


class MinIODBRequestException(MinIODBException):
    """
    MinIODB 请求异常
    
    当请求参数错误或格式错误时抛出此异常。
    """
    
    def __init__(self, message: str, cause: Optional[Exception] = None):
        super().__init__(message, "REQUEST_ERROR", 400, cause)


class MinIODBServerException(MinIODBException):
    """
    MinIODB 服务器异常
    
    当服务器内部发生错误时抛出此异常。
    """
    
    def __init__(self, message: str, cause: Optional[Exception] = None):
        super().__init__(message, "SERVER_ERROR", 500, cause)


class MinIODBTimeoutException(MinIODBException):
    """
    MinIODB 超时异常
    
    当请求超时时抛出此异常。
    """
    
    def __init__(self, message: str, cause: Optional[Exception] = None):
        super().__init__(message, "TIMEOUT_ERROR", 408, cause)
