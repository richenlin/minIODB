package com.miniodb.client.exception;

/**
 * MinIODB 认证异常
 * 
 * 当认证失败或令牌过期时抛出此异常。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class MinIODBAuthenticationException extends MinIODBException {
    
    /**
     * 构造函数
     * @param message 错误消息
     */
    public MinIODBAuthenticationException(String message) {
        super(message, "AUTHENTICATION_ERROR", 401);
    }
    
    /**
     * 构造函数
     * @param message 错误消息
     * @param cause 原因异常
     */
    public MinIODBAuthenticationException(String message, Throwable cause) {
        super(message, cause, "AUTHENTICATION_ERROR", 401);
    }
}
