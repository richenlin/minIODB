package com.miniodb.client.config;

/**
 * 日志配置类
 * 
 * 包含客户端日志相关的配置选项。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class LoggingConfig {
    
    /**
     * 日志级别枚举
     */
    public enum Level {
        TRACE, DEBUG, INFO, WARN, ERROR, OFF
    }
    
    /**
     * 日志格式枚举
     */
    public enum Format {
        TEXT, JSON
    }
    
    private final Level level;
    private final Format format;
    private final boolean enableRequestLogging;
    private final boolean enableResponseLogging;
    private final boolean enablePerformanceLogging;
    
    private LoggingConfig(Builder builder) {
        this.level = builder.level;
        this.format = builder.format;
        this.enableRequestLogging = builder.enableRequestLogging;
        this.enableResponseLogging = builder.enableResponseLogging;
        this.enablePerformanceLogging = builder.enablePerformanceLogging;
    }
    
    /**
     * 获取日志级别
     * @return 日志级别
     */
    public Level getLevel() {
        return level;
    }
    
    /**
     * 获取日志格式
     * @return 日志格式
     */
    public Format getFormat() {
        return format;
    }
    
    /**
     * 是否启用请求日志
     * @return 布尔值
     */
    public boolean isEnableRequestLogging() {
        return enableRequestLogging;
    }
    
    /**
     * 是否启用响应日志
     * @return 布尔值
     */
    public boolean isEnableResponseLogging() {
        return enableResponseLogging;
    }
    
    /**
     * 是否启用性能日志
     * @return 布尔值
     */
    public boolean isEnablePerformanceLogging() {
        return enablePerformanceLogging;
    }
    
    /**
     * 创建默认日志配置
     * @return 默认日志配置实例
     */
    public static LoggingConfig defaultConfig() {
        return builder().build();
    }
    
    /**
     * 创建日志配置建造者
     * @return 日志配置建造者实例
     */
    public static Builder builder() {
        return new Builder();
    }
    
    /**
     * 日志配置建造者类
     */
    public static class Builder {
        private Level level = Level.INFO;
        private Format format = Format.TEXT;
        private boolean enableRequestLogging = false;
        private boolean enableResponseLogging = false;
        private boolean enablePerformanceLogging = true;
        
        /**
         * 设置日志级别
         * @param level 日志级别
         * @return 建造者实例
         */
        public Builder level(Level level) {
            this.level = level;
            return this;
        }
        
        /**
         * 设置日志级别（字符串）
         * @param level 日志级别字符串
         * @return 建造者实例
         */
        public Builder level(String level) {
            this.level = Level.valueOf(level.toUpperCase());
            return this;
        }
        
        /**
         * 设置日志格式
         * @param format 日志格式
         * @return 建造者实例
         */
        public Builder format(Format format) {
            this.format = format;
            return this;
        }
        
        /**
         * 设置日志格式（字符串）
         * @param format 日志格式字符串
         * @return 建造者实例
         */
        public Builder format(String format) {
            this.format = Format.valueOf(format.toUpperCase());
            return this;
        }
        
        /**
         * 设置是否启用请求日志
         * @param enableRequestLogging 布尔值
         * @return 建造者实例
         */
        public Builder enableRequestLogging(boolean enableRequestLogging) {
            this.enableRequestLogging = enableRequestLogging;
            return this;
        }
        
        /**
         * 设置是否启用响应日志
         * @param enableResponseLogging 布尔值
         * @return 建造者实例
         */
        public Builder enableResponseLogging(boolean enableResponseLogging) {
            this.enableResponseLogging = enableResponseLogging;
            return this;
        }
        
        /**
         * 设置是否启用性能日志
         * @param enablePerformanceLogging 布尔值
         * @return 建造者实例
         */
        public Builder enablePerformanceLogging(boolean enablePerformanceLogging) {
            this.enablePerformanceLogging = enablePerformanceLogging;
            return this;
        }
        
        /**
         * 构建日志配置实例
         * @return 日志配置实例
         */
        public LoggingConfig build() {
            return new LoggingConfig(this);
        }
    }
    
    @Override
    public String toString() {
        return "LoggingConfig{" +
                "level=" + level +
                ", format=" + format +
                ", enableRequestLogging=" + enableRequestLogging +
                ", enableResponseLogging=" + enableResponseLogging +
                ", enablePerformanceLogging=" + enablePerformanceLogging +
                '}';
    }
}
