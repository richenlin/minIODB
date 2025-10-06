package com.miniodb.client.config;

/**
 * 认证配置类
 * 
 * 包含连接到 MinIODB 服务所需的认证信息。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class AuthConfig {
    
    private final String apiKey;
    private final String secret;
    private final String tokenType;
    
    private AuthConfig(Builder builder) {
        this.apiKey = builder.apiKey;
        this.secret = builder.secret;
        this.tokenType = builder.tokenType;
    }
    
    /**
     * 获取 API 密钥
     * @return API 密钥
     */
    public String getApiKey() {
        return apiKey;
    }
    
    /**
     * 获取 API 密码
     * @return API 密码
     */
    public String getSecret() {
        return secret;
    }
    
    /**
     * 获取令牌类型
     * @return 令牌类型
     */
    public String getTokenType() {
        return tokenType;
    }
    
    /**
     * 检查是否启用了认证
     * @return 如果配置了认证信息则返回 true
     */
    public boolean isEnabled() {
        return apiKey != null && !apiKey.isEmpty() && 
               secret != null && !secret.isEmpty();
    }
    
    /**
     * 创建认证配置建造者
     * @return 认证配置建造者实例
     */
    public static Builder builder() {
        return new Builder();
    }
    
    /**
     * 认证配置建造者类
     */
    public static class Builder {
        private String apiKey;
        private String secret;
        private String tokenType = "Bearer";
        
        /**
         * 设置 API 密钥
         * @param apiKey API 密钥
         * @return 建造者实例
         */
        public Builder apiKey(String apiKey) {
            this.apiKey = apiKey;
            return this;
        }
        
        /**
         * 设置 API 密码
         * @param secret API 密码
         * @return 建造者实例
         */
        public Builder secret(String secret) {
            this.secret = secret;
            return this;
        }
        
        /**
         * 设置令牌类型
         * @param tokenType 令牌类型
         * @return 建造者实例
         */
        public Builder tokenType(String tokenType) {
            this.tokenType = tokenType;
            return this;
        }
        
        /**
         * 构建认证配置实例
         * @return 认证配置实例
         */
        public AuthConfig build() {
            return new AuthConfig(this);
        }
    }
    
    @Override
    public String toString() {
        return "AuthConfig{" +
                "apiKey='" + (apiKey != null ? "***" : null) + '\'' +
                ", secret='" + (secret != null ? "***" : null) + '\'' +
                ", tokenType='" + tokenType + '\'' +
                '}';
    }
}
