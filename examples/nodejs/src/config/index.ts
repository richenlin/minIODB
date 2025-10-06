/**
 * MinIODB Node.js TypeScript SDK 配置模块
 * 
 * 包含客户端配置相关的类型定义和接口。
 */

/**
 * 日志级别枚举
 */
export enum LogLevel {
    TRACE = 'trace',
    DEBUG = 'debug',
    INFO = 'info',
    WARN = 'warn',
    ERROR = 'error',
    OFF = 'off'
}

/**
 * 日志格式枚举
 */
export enum LogFormat {
    TEXT = 'text',
    JSON = 'json'
}

/**
 * 认证配置接口
 */
export interface AuthConfig {
    /** API 密钥 */
    apiKey: string;
    /** API 密码 */
    secret: string;
    /** 令牌类型，默认为 'Bearer' */
    tokenType?: string;
}

/**
 * 连接配置接口
 */
export interface ConnectionConfig {
    /** 最大连接数，默认 10 */
    maxConnections?: number;
    /** 连接超时时间（毫秒），默认 30000 */
    timeout?: number;
    /** 重试次数，默认 3 */
    retryAttempts?: number;
    /** 保持连接时间（毫秒），默认 300000 */
    keepAliveTime?: number;
    /** 保持连接超时时间（毫秒），默认 10000 */
    keepAliveTimeout?: number;
    /** 是否在没有调用时保持连接，默认 false */
    keepAliveWithoutCalls?: boolean;
    /** 最大接收消息长度（字节），默认 4MB */
    maxReceiveMessageLength?: number;
    /** 最大发送消息长度（字节），默认 4MB */
    maxSendMessageLength?: number;
}

/**
 * 日志配置接口
 */
export interface LoggingConfig {
    /** 日志级别，默认 'info' */
    level?: LogLevel | string;
    /** 日志格式，默认 'text' */
    format?: LogFormat | string;
    /** 是否启用请求日志，默认 false */
    enableRequestLogging?: boolean;
    /** 是否启用响应日志，默认 false */
    enableResponseLogging?: boolean;
    /** 是否启用性能日志，默认 true */
    enablePerformanceLogging?: boolean;
}

/**
 * MinIODB 客户端配置接口
 */
export interface MinIODBConfig {
    /** 服务器主机地址，默认 'localhost' */
    host?: string;
    /** gRPC 端口，默认 8080 */
    grpcPort?: number;
    /** REST API 端口，默认 8081 */
    restPort?: number;
    /** 认证配置，可选 */
    auth?: AuthConfig;
    /** 连接配置，可选 */
    connection?: ConnectionConfig;
    /** 日志配置，可选 */
    logging?: LoggingConfig;
}

/**
 * 默认配置常量
 */
export const DEFAULT_CONFIG: Required<Omit<MinIODBConfig, 'auth'>> & { auth?: AuthConfig } = {
    host: 'localhost',
    grpcPort: 8080,
    restPort: 8081,
    connection: {
        maxConnections: 10,
        timeout: 30000,
        retryAttempts: 3,
        keepAliveTime: 300000,
        keepAliveTimeout: 10000,
        keepAliveWithoutCalls: false,
        maxReceiveMessageLength: 4 * 1024 * 1024, // 4MB
        maxSendMessageLength: 4 * 1024 * 1024     // 4MB
    },
    logging: {
        level: LogLevel.INFO,
        format: LogFormat.TEXT,
        enableRequestLogging: false,
        enableResponseLogging: false,
        enablePerformanceLogging: true
    }
};

/**
 * 配置验证器类
 */
export class ConfigValidator {
    /**
     * 验证 MinIODB 配置
     * @param config 配置对象
     * @throws {Error} 配置验证失败时抛出错误
     */
    static validate(config: MinIODBConfig): void {
        if (config.host !== undefined && !config.host.trim()) {
            throw new Error('主机地址不能为空');
        }

        if (config.grpcPort !== undefined && (config.grpcPort <= 0 || config.grpcPort > 65535)) {
            throw new Error('gRPC 端口必须在 1-65535 范围内');
        }

        if (config.restPort !== undefined && (config.restPort <= 0 || config.restPort > 65535)) {
            throw new Error('REST 端口必须在 1-65535 范围内');
        }

        if (config.connection) {
            const conn = config.connection;
            
            if (conn.maxConnections !== undefined && conn.maxConnections <= 0) {
                throw new Error('最大连接数必须大于 0');
            }

            if (conn.timeout !== undefined && conn.timeout <= 0) {
                throw new Error('超时时间必须大于 0');
            }

            if (conn.retryAttempts !== undefined && conn.retryAttempts < 0) {
                throw new Error('重试次数不能为负数');
            }
        }

        if (config.auth) {
            if (!config.auth.apiKey?.trim()) {
                throw new Error('API 密钥不能为空');
            }
            if (!config.auth.secret?.trim()) {
                throw new Error('API 密码不能为空');
            }
        }
    }

    /**
     * 合并配置，使用默认值填充未提供的配置项
     * @param config 用户配置
     * @returns 合并后的完整配置
     */
    static mergeWithDefaults(config: MinIODBConfig): Required<Omit<MinIODBConfig, 'auth'>> & { auth?: AuthConfig } {
        const merged = {
            ...DEFAULT_CONFIG,
            ...config,
            connection: {
                ...DEFAULT_CONFIG.connection,
                ...config.connection
            },
            logging: {
                ...DEFAULT_CONFIG.logging,
                ...config.logging
            }
        };

        this.validate(merged);
        return merged;
    }
}

/**
 * 配置工具类
 */
export class ConfigUtils {
    /**
     * 获取 gRPC 服务器地址
     * @param config 配置对象
     * @returns gRPC 地址字符串
     */
    static getGrpcAddress(config: MinIODBConfig): string {
        const host = config.host || DEFAULT_CONFIG.host;
        const port = config.grpcPort || DEFAULT_CONFIG.grpcPort;
        return `${host}:${port}`;
    }

    /**
     * 获取 REST API 基础 URL
     * @param config 配置对象
     * @returns REST API 基础 URL
     */
    static getRestBaseUrl(config: MinIODBConfig): string {
        const host = config.host || DEFAULT_CONFIG.host;
        const port = config.restPort || DEFAULT_CONFIG.restPort;
        return `http://${host}:${port}`;
    }

    /**
     * 检查是否启用了认证
     * @param config 配置对象
     * @returns 是否启用认证
     */
    static isAuthEnabled(config: MinIODBConfig): boolean {
        return !!(config.auth?.apiKey && config.auth?.secret);
    }

    /**
     * 获取安全的配置对象（隐藏敏感信息）
     * @param config 配置对象
     * @returns 安全的配置对象
     */
    static getSafeConfig(config: MinIODBConfig): Record<string, any> {
        return {
            host: config.host,
            grpcPort: config.grpcPort,
            restPort: config.restPort,
            auth: config.auth ? {
                apiKey: config.auth.apiKey ? '***' : undefined,
                secret: config.auth.secret ? '***' : undefined,
                tokenType: config.auth.tokenType
            } : undefined,
            connection: config.connection,
            logging: config.logging
        };
    }
}
