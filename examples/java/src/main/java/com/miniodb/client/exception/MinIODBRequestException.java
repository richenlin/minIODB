package com.miniodb.client.exception;

/**
 * MinIODB 请求异常
 * 
 * 当请求参数错误或格式错误时抛出此异常。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class MinIODBRequestException extends MinIODBException {
    
    /**
     * 构造函数
     * @param message 错误消息
     */
    public MinIODBRequestException(String message) {
        super(message, "REQUEST_ERROR", 400);
    }
    
    /**
     * 构造函数
     * @param message 错误消息
     * @param cause 原因异常
     */
    public MinIODBRequestException(String message, Throwable cause) {
        super(message, cause, "REQUEST_ERROR", 400);
    }
}
