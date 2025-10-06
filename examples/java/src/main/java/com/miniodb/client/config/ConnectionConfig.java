package com.miniodb.client.config;

import java.time.Duration;

/**
 * 连接配置类
 * 
 * 包含 gRPC 连接相关的配置选项。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class ConnectionConfig {
    
    private final int maxConnections;
    private final Duration timeout;
    private final int retryAttempts;
    private final Duration keepAliveTime;
    private final Duration keepAliveTimeout;
    private final boolean keepAliveWithoutCalls;
    private final int maxInboundMessageSize;
    private final int maxOutboundMessageSize;
    
    private ConnectionConfig(Builder builder) {
        this.maxConnections = builder.maxConnections;
        this.timeout = builder.timeout;
        this.retryAttempts = builder.retryAttempts;
        this.keepAliveTime = builder.keepAliveTime;
        this.keepAliveTimeout = builder.keepAliveTimeout;
        this.keepAliveWithoutCalls = builder.keepAliveWithoutCalls;
        this.maxInboundMessageSize = builder.maxInboundMessageSize;
        this.maxOutboundMessageSize = builder.maxOutboundMessageSize;
    }
    
    /**
     * 获取最大连接数
     * @return 最大连接数
     */
    public int getMaxConnections() {
        return maxConnections;
    }
    
    /**
     * 获取连接超时时间
     * @return 超时时间
     */
    public Duration getTimeout() {
        return timeout;
    }
    
    /**
     * 获取重试次数
     * @return 重试次数
     */
    public int getRetryAttempts() {
        return retryAttempts;
    }
    
    /**
     * 获取保持连接时间
     * @return 保持连接时间
     */
    public Duration getKeepAliveTime() {
        return keepAliveTime;
    }
    
    /**
     * 获取保持连接超时时间
     * @return 保持连接超时时间
     */
    public Duration getKeepAliveTimeout() {
        return keepAliveTimeout;
    }
    
    /**
     * 是否在没有调用时保持连接
     * @return 布尔值
     */
    public boolean isKeepAliveWithoutCalls() {
        return keepAliveWithoutCalls;
    }
    
    /**
     * 获取最大入站消息大小
     * @return 最大入站消息大小（字节）
     */
    public int getMaxInboundMessageSize() {
        return maxInboundMessageSize;
    }
    
    /**
     * 获取最大出站消息大小
     * @return 最大出站消息大小（字节）
     */
    public int getMaxOutboundMessageSize() {
        return maxOutboundMessageSize;
    }
    
    /**
     * 创建默认连接配置
     * @return 默认连接配置实例
     */
    public static ConnectionConfig defaultConfig() {
        return builder().build();
    }
    
    /**
     * 创建连接配置建造者
     * @return 连接配置建造者实例
     */
    public static Builder builder() {
        return new Builder();
    }
    
    /**
     * 连接配置建造者类
     */
    public static class Builder {
        private int maxConnections = 10;
        private Duration timeout = Duration.ofSeconds(30);
        private int retryAttempts = 3;
        private Duration keepAliveTime = Duration.ofMinutes(5);
        private Duration keepAliveTimeout = Duration.ofSeconds(10);
        private boolean keepAliveWithoutCalls = false;
        private int maxInboundMessageSize = 4 * 1024 * 1024; // 4MB
        private int maxOutboundMessageSize = 4 * 1024 * 1024; // 4MB
        
        /**
         * 设置最大连接数
         * @param maxConnections 最大连接数
         * @return 建造者实例
         */
        public Builder maxConnections(int maxConnections) {
            this.maxConnections = maxConnections;
            return this;
        }
        
        /**
         * 设置连接超时时间
         * @param timeout 超时时间
         * @return 建造者实例
         */
        public Builder timeout(Duration timeout) {
            this.timeout = timeout;
            return this;
        }
        
        /**
         * 设置重试次数
         * @param retryAttempts 重试次数
         * @return 建造者实例
         */
        public Builder retryAttempts(int retryAttempts) {
            this.retryAttempts = retryAttempts;
            return this;
        }
        
        /**
         * 设置保持连接时间
         * @param keepAliveTime 保持连接时间
         * @return 建造者实例
         */
        public Builder keepAliveTime(Duration keepAliveTime) {
            this.keepAliveTime = keepAliveTime;
            return this;
        }
        
        /**
         * 设置保持连接超时时间
         * @param keepAliveTimeout 保持连接超时时间
         * @return 建造者实例
         */
        public Builder keepAliveTimeout(Duration keepAliveTimeout) {
            this.keepAliveTimeout = keepAliveTimeout;
            return this;
        }
        
        /**
         * 设置是否在没有调用时保持连接
         * @param keepAliveWithoutCalls 布尔值
         * @return 建造者实例
         */
        public Builder keepAliveWithoutCalls(boolean keepAliveWithoutCalls) {
            this.keepAliveWithoutCalls = keepAliveWithoutCalls;
            return this;
        }
        
        /**
         * 设置最大入站消息大小
         * @param maxInboundMessageSize 最大入站消息大小（字节）
         * @return 建造者实例
         */
        public Builder maxInboundMessageSize(int maxInboundMessageSize) {
            this.maxInboundMessageSize = maxInboundMessageSize;
            return this;
        }
        
        /**
         * 设置最大出站消息大小
         * @param maxOutboundMessageSize 最大出站消息大小（字节）
         * @return 建造者实例
         */
        public Builder maxOutboundMessageSize(int maxOutboundMessageSize) {
            this.maxOutboundMessageSize = maxOutboundMessageSize;
            return this;
        }
        
        /**
         * 构建连接配置实例
         * @return 连接配置实例
         */
        public ConnectionConfig build() {
            return new ConnectionConfig(this);
        }
    }
    
    @Override
    public String toString() {
        return "ConnectionConfig{" +
                "maxConnections=" + maxConnections +
                ", timeout=" + timeout +
                ", retryAttempts=" + retryAttempts +
                ", keepAliveTime=" + keepAliveTime +
                ", keepAliveTimeout=" + keepAliveTimeout +
                ", keepAliveWithoutCalls=" + keepAliveWithoutCalls +
                ", maxInboundMessageSize=" + maxInboundMessageSize +
                ", maxOutboundMessageSize=" + maxOutboundMessageSize +
                '}';
    }
}
