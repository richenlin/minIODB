package com.miniodb.client.exception;

/**
 * MinIODB 服务器异常
 * 
 * 当服务器内部发生错误时抛出此异常。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class MinIODBServerException extends MinIODBException {
    
    /**
     * 构造函数
     * @param message 错误消息
     */
    public MinIODBServerException(String message) {
        super(message, "SERVER_ERROR", 500);
    }
    
    /**
     * 构造函数
     * @param message 错误消息
     * @param cause 原因异常
     */
    public MinIODBServerException(String message, Throwable cause) {
        super(message, cause, "SERVER_ERROR", 500);
    }
}
