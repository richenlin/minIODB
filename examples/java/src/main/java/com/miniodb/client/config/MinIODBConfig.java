package com.miniodb.client.config;

import java.time.Duration;

/**
 * MinIODB 客户端配置类
 * 
 * 此类包含连接到 MinIODB 服务所需的所有配置选项。
 * 使用建造者模式来创建配置实例。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class MinIODBConfig {
    
    private final String host;
    private final int grpcPort;
    private final int restPort;
    private final AuthConfig auth;
    private final ConnectionConfig connection;
    private final LoggingConfig logging;
    
    private MinIODBConfig(Builder builder) {
        this.host = builder.host;
        this.grpcPort = builder.grpcPort;
        this.restPort = builder.restPort;
        this.auth = builder.auth;
        this.connection = builder.connection;
        this.logging = builder.logging;
    }
    
    /**
     * 获取服务器主机地址
     * @return 主机地址
     */
    public String getHost() {
        return host;
    }
    
    /**
     * 获取 gRPC 端口
     * @return gRPC 端口号
     */
    public int getGrpcPort() {
        return grpcPort;
    }
    
    /**
     * 获取 REST API 端口
     * @return REST 端口号
     */
    public int getRestPort() {
        return restPort;
    }
    
    /**
     * 获取认证配置
     * @return 认证配置对象
     */
    public AuthConfig getAuth() {
        return auth;
    }
    
    /**
     * 获取连接配置
     * @return 连接配置对象
     */
    public ConnectionConfig getConnection() {
        return connection;
    }
    
    /**
     * 获取日志配置
     * @return 日志配置对象
     */
    public LoggingConfig getLogging() {
        return logging;
    }
    
    /**
     * 获取 gRPC 服务器地址
     * @return 完整的 gRPC 地址
     */
    public String getGrpcAddress() {
        return host + ":" + grpcPort;
    }
    
    /**
     * 获取 REST API 基础 URL
     * @return REST API 基础 URL
     */
    public String getRestBaseUrl() {
        return "http://" + host + ":" + restPort;
    }
    
    /**
     * 创建配置建造者
     * @return 配置建造者实例
     */
    public static Builder builder() {
        return new Builder();
    }
    
    /**
     * 配置建造者类
     */
    public static class Builder {
        private String host = "localhost";
        private int grpcPort = 8080;
        private int restPort = 8081;
        private AuthConfig auth;
        private ConnectionConfig connection = ConnectionConfig.defaultConfig();
        private LoggingConfig logging = LoggingConfig.defaultConfig();
        
        /**
         * 设置服务器主机地址
         * @param host 主机地址
         * @return 建造者实例
         */
        public Builder host(String host) {
            this.host = host;
            return this;
        }
        
        /**
         * 设置 gRPC 端口
         * @param port gRPC 端口号
         * @return 建造者实例
         */
        public Builder grpcPort(int port) {
            this.grpcPort = port;
            return this;
        }
        
        /**
         * 设置 REST API 端口
         * @param port REST 端口号
         * @return 建造者实例
         */
        public Builder restPort(int port) {
            this.restPort = port;
            return this;
        }
        
        /**
         * 设置认证配置
         * @param auth 认证配置
         * @return 建造者实例
         */
        public Builder auth(AuthConfig auth) {
            this.auth = auth;
            return this;
        }
        
        /**
         * 设置连接配置
         * @param connection 连接配置
         * @return 建造者实例
         */
        public Builder connection(ConnectionConfig connection) {
            this.connection = connection;
            return this;
        }
        
        /**
         * 设置日志配置
         * @param logging 日志配置
         * @return 建造者实例
         */
        public Builder logging(LoggingConfig logging) {
            this.logging = logging;
            return this;
        }
        
        /**
         * 构建配置实例
         * @return MinIODB 配置实例
         */
        public MinIODBConfig build() {
            return new MinIODBConfig(this);
        }
    }
    
    @Override
    public String toString() {
        return "MinIODBConfig{" +
                "host='" + host + '\'' +
                ", grpcPort=" + grpcPort +
                ", restPort=" + restPort +
                ", auth=" + auth +
                ", connection=" + connection +
                ", logging=" + logging +
                '}';
    }
}
