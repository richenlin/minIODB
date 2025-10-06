package com.miniodb.client.exception;

/**
 * MinIODB 客户端异常基类
 * 
 * 所有 MinIODB 客户端异常的基类。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class MinIODBException extends Exception {
    
    private final String errorCode;
    private final int statusCode;
    
    /**
     * 构造函数
     * @param message 错误消息
     */
    public MinIODBException(String message) {
        super(message);
        this.errorCode = "UNKNOWN";
        this.statusCode = -1;
    }
    
    /**
     * 构造函数
     * @param message 错误消息
     * @param cause 原因异常
     */
    public MinIODBException(String message, Throwable cause) {
        super(message, cause);
        this.errorCode = "UNKNOWN";
        this.statusCode = -1;
    }
    
    /**
     * 构造函数
     * @param message 错误消息
     * @param errorCode 错误码
     * @param statusCode 状态码
     */
    public MinIODBException(String message, String errorCode, int statusCode) {
        super(message);
        this.errorCode = errorCode;
        this.statusCode = statusCode;
    }
    
    /**
     * 构造函数
     * @param message 错误消息
     * @param cause 原因异常
     * @param errorCode 错误码
     * @param statusCode 状态码
     */
    public MinIODBException(String message, Throwable cause, String errorCode, int statusCode) {
        super(message, cause);
        this.errorCode = errorCode;
        this.statusCode = statusCode;
    }
    
    /**
     * 获取错误码
     * @return 错误码
     */
    public String getErrorCode() {
        return errorCode;
    }
    
    /**
     * 获取状态码
     * @return 状态码
     */
    public int getStatusCode() {
        return statusCode;
    }
    
    @Override
    public String toString() {
        return String.format("MinIODBException{errorCode='%s', statusCode=%d, message='%s'}", 
                errorCode, statusCode, getMessage());
    }
}
