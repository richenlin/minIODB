/**
 * MinIODB Node.js TypeScript SDK 错误处理模块
 * 
 * 定义了所有 MinIODB 客户端可能抛出的错误类型。
 */

/**
 * 错误码枚举
 */
export enum ErrorCode {
    UNKNOWN = 'UNKNOWN',
    CONNECTION_ERROR = 'CONNECTION_ERROR',
    AUTHENTICATION_ERROR = 'AUTHENTICATION_ERROR',
    REQUEST_ERROR = 'REQUEST_ERROR',
    SERVER_ERROR = 'SERVER_ERROR',
    TIMEOUT_ERROR = 'TIMEOUT_ERROR'
}

/**
 * MinIODB 客户端错误基类
 */
export class MinIODBError extends Error {
    public readonly code: ErrorCode;
    public readonly statusCode: number;
    public readonly cause?: Error;

    constructor(
        message: string,
        code: ErrorCode = ErrorCode.UNKNOWN,
        statusCode: number = -1,
        cause?: Error
    ) {
        super(message);
        this.name = this.constructor.name;
        this.code = code;
        this.statusCode = statusCode;
        this.cause = cause;

        // 确保错误堆栈正确显示
        if (Error.captureStackTrace) {
            Error.captureStackTrace(this, this.constructor);
        }
    }

    /**
     * 转换为 JSON 对象
     */
    toJSON(): Record<string, any> {
        return {
            name: this.name,
            message: this.message,
            code: this.code,
            statusCode: this.statusCode,
            stack: this.stack,
            cause: this.cause?.message
        };
    }

    /**
     * 转换为字符串
     */
    toString(): string {
        return `${this.name}(code=${this.code}, status=${this.statusCode}, message='${this.message}')`;
    }
}

/**
 * MinIODB 连接错误
 * 
 * 当无法连接到 MinIODB 服务时抛出此错误。
 */
export class MinIODBConnectionError extends MinIODBError {
    constructor(message: string, cause?: Error) {
        super(message, ErrorCode.CONNECTION_ERROR, 503, cause);
    }
}

/**
 * MinIODB 认证错误
 * 
 * 当认证失败或令牌过期时抛出此错误。
 */
export class MinIODBAuthenticationError extends MinIODBError {
    constructor(message: string, cause?: Error) {
        super(message, ErrorCode.AUTHENTICATION_ERROR, 401, cause);
    }
}

/**
 * MinIODB 请求错误
 * 
 * 当请求参数错误或格式错误时抛出此错误。
 */
export class MinIODBRequestError extends MinIODBError {
    constructor(message: string, cause?: Error) {
        super(message, ErrorCode.REQUEST_ERROR, 400, cause);
    }
}

/**
 * MinIODB 服务器错误
 * 
 * 当服务器内部发生错误时抛出此错误。
 */
export class MinIODBServerError extends MinIODBError {
    constructor(message: string, cause?: Error) {
        super(message, ErrorCode.SERVER_ERROR, 500, cause);
    }
}

/**
 * MinIODB 超时错误
 * 
 * 当请求超时时抛出此错误。
 */
export class MinIODBTimeoutError extends MinIODBError {
    constructor(message: string, cause?: Error) {
        super(message, ErrorCode.TIMEOUT_ERROR, 408, cause);
    }
}

/**
 * 错误类型检查工具
 */
export class ErrorUtils {
    /**
     * 检查是否为连接错误
     */
    static isConnectionError(error: Error): error is MinIODBConnectionError {
        return error instanceof MinIODBConnectionError;
    }

    /**
     * 检查是否为认证错误
     */
    static isAuthenticationError(error: Error): error is MinIODBAuthenticationError {
        return error instanceof MinIODBAuthenticationError;
    }

    /**
     * 检查是否为请求错误
     */
    static isRequestError(error: Error): error is MinIODBRequestError {
        return error instanceof MinIODBRequestError;
    }

    /**
     * 检查是否为服务器错误
     */
    static isServerError(error: Error): error is MinIODBServerError {
        return error instanceof MinIODBServerError;
    }

    /**
     * 检查是否为超时错误
     */
    static isTimeoutError(error: Error): error is MinIODBTimeoutError {
        return error instanceof MinIODBTimeoutError;
    }

    /**
     * 检查是否为 MinIODB 错误
     */
    static isMinIODBError(error: Error): error is MinIODBError {
        return error instanceof MinIODBError;
    }

    /**
     * 从 gRPC 错误转换为 MinIODB 错误
     */
    static fromGrpcError(error: any): MinIODBError {
        if (!error) {
            return new MinIODBServerError('未知的 gRPC 错误');
        }

        const message = error.message || error.details || '未知错误';
        
        // gRPC 状态码映射
        switch (error.code) {
            case 16: // UNAUTHENTICATED
                return new MinIODBAuthenticationError(message, error);
            case 3: // INVALID_ARGUMENT
                return new MinIODBRequestError(message, error);
            case 4: // DEADLINE_EXCEEDED
                return new MinIODBTimeoutError(message, error);
            case 14: // UNAVAILABLE
                return new MinIODBConnectionError(message, error);
            case 13: // INTERNAL
                return new MinIODBServerError(message, error);
            default:
                return new MinIODBServerError(message, error);
        }
    }

    /**
     * 从 HTTP 错误转换为 MinIODB 错误
     */
    static fromHttpError(error: any, statusCode?: number): MinIODBError {
        if (!error) {
            return new MinIODBServerError('未知的 HTTP 错误');
        }

        const message = error.message || error.response?.data?.message || '未知错误';
        const status = statusCode || error.response?.status || error.status || 500;

        switch (status) {
            case 401:
            case 403:
                return new MinIODBAuthenticationError(message, error);
            case 400:
            case 422:
                return new MinIODBRequestError(message, error);
            case 408:
            case 504:
                return new MinIODBTimeoutError(message, error);
            case 503:
            case 502:
                return new MinIODBConnectionError(message, error);
            default:
                if (status >= 500) {
                    return new MinIODBServerError(message, error);
                } else {
                    return new MinIODBRequestError(message, error);
                }
        }
    }

    /**
     * 创建重试策略错误
     */
    static createRetryableError(
        error: MinIODBError,
        attempt: number,
        maxAttempts: number
    ): MinIODBError {
        const isRetryable = this.isConnectionError(error) || 
                           this.isTimeoutError(error) || 
                           this.isServerError(error);

        if (!isRetryable || attempt >= maxAttempts) {
            return error;
        }

        const newMessage = `${error.message} (尝试 ${attempt}/${maxAttempts})`;
        
        switch (true) {
            case this.isConnectionError(error):
                return new MinIODBConnectionError(newMessage, error);
            case this.isTimeoutError(error):
                return new MinIODBTimeoutError(newMessage, error);
            case this.isServerError(error):
                return new MinIODBServerError(newMessage, error);
            default:
                return error;
        }
    }
}
