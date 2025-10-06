package com.miniodb.client.exception;

/**
 * MinIODB 超时异常
 * 
 * 当请求超时时抛出此异常。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class MinIODBTimeoutException extends MinIODBException {
    
    /**
     * 构造函数
     * @param message 错误消息
     */
    public MinIODBTimeoutException(String message) {
        super(message, "TIMEOUT_ERROR", 408);
    }
    
    /**
     * 构造函数
     * @param message 错误消息
     * @param cause 原因异常
     */
    public MinIODBTimeoutException(String message, Throwable cause) {
        super(message, cause, "TIMEOUT_ERROR", 408);
    }
}
