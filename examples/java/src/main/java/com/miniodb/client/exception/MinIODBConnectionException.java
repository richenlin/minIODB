package com.miniodb.client.exception;

/**
 * MinIODB 连接异常
 * 
 * 当无法连接到 MinIODB 服务时抛出此异常。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class MinIODBConnectionException extends MinIODBException {
    
    /**
     * 构造函数
     * @param message 错误消息
     */
    public MinIODBConnectionException(String message) {
        super(message, "CONNECTION_ERROR", 503);
    }
    
    /**
     * 构造函数
     * @param message 错误消息
     * @param cause 原因异常
     */
    public MinIODBConnectionException(String message, Throwable cause) {
        super(message, cause, "CONNECTION_ERROR", 503);
    }
}
