package com.miniodb.client.model;

import java.util.Map;
import java.util.Objects;

/**
 * 表配置模型
 * 
 * 表示 MinIODB 中表的配置信息。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class TableConfig {
    
    private final int bufferSize;
    private final int flushIntervalSeconds;
    private final int retentionDays;
    private final boolean backupEnabled;
    private final Map<String, String> properties;
    
    private TableConfig(Builder builder) {
        this.bufferSize = builder.bufferSize;
        this.flushIntervalSeconds = builder.flushIntervalSeconds;
        this.retentionDays = builder.retentionDays;
        this.backupEnabled = builder.backupEnabled;
        this.properties = builder.properties;
    }
    
    /**
     * 获取缓冲区大小
     * @return 缓冲区大小
     */
    public int getBufferSize() {
        return bufferSize;
    }
    
    /**
     * 获取刷新间隔（秒）
     * @return 刷新间隔秒数
     */
    public int getFlushIntervalSeconds() {
        return flushIntervalSeconds;
    }
    
    /**
     * 获取数据保留天数
     * @return 保留天数
     */
    public int getRetentionDays() {
        return retentionDays;
    }
    
    /**
     * 是否启用备份
     * @return 布尔值
     */
    public boolean isBackupEnabled() {
        return backupEnabled;
    }
    
    /**
     * 获取扩展属性
     * @return 属性映射
     */
    public Map<String, String> getProperties() {
        return properties;
    }
    
    /**
     * 创建表配置建造者
     * @return 表配置建造者实例
     */
    public static Builder builder() {
        return new Builder();
    }
    
    /**
     * 表配置建造者类
     */
    public static class Builder {
        private int bufferSize = 1000;
        private int flushIntervalSeconds = 30;
        private int retentionDays = 365;
        private boolean backupEnabled = false;
        private Map<String, String> properties;
        
        /**
         * 设置缓冲区大小
         * @param bufferSize 缓冲区大小
         * @return 建造者实例
         */
        public Builder bufferSize(int bufferSize) {
            this.bufferSize = bufferSize;
            return this;
        }
        
        /**
         * 设置刷新间隔（秒）
         * @param flushIntervalSeconds 刷新间隔秒数
         * @return 建造者实例
         */
        public Builder flushIntervalSeconds(int flushIntervalSeconds) {
            this.flushIntervalSeconds = flushIntervalSeconds;
            return this;
        }
        
        /**
         * 设置数据保留天数
         * @param retentionDays 保留天数
         * @return 建造者实例
         */
        public Builder retentionDays(int retentionDays) {
            this.retentionDays = retentionDays;
            return this;
        }
        
        /**
         * 设置是否启用备份
         * @param backupEnabled 布尔值
         * @return 建造者实例
         */
        public Builder backupEnabled(boolean backupEnabled) {
            this.backupEnabled = backupEnabled;
            return this;
        }
        
        /**
         * 设置扩展属性
         * @param properties 属性映射
         * @return 建造者实例
         */
        public Builder properties(Map<String, String> properties) {
            this.properties = properties;
            return this;
        }
        
        /**
         * 构建表配置实例
         * @return 表配置实例
         */
        public TableConfig build() {
            return new TableConfig(this);
        }
    }
    
    @Override
    public boolean equals(Object o) {
        if (this == o) return true;
        if (o == null || getClass() != o.getClass()) return false;
        TableConfig that = (TableConfig) o;
        return bufferSize == that.bufferSize &&
                flushIntervalSeconds == that.flushIntervalSeconds &&
                retentionDays == that.retentionDays &&
                backupEnabled == that.backupEnabled &&
                Objects.equals(properties, that.properties);
    }
    
    @Override
    public int hashCode() {
        return Objects.hash(bufferSize, flushIntervalSeconds, retentionDays, backupEnabled, properties);
    }
    
    @Override
    public String toString() {
        return "TableConfig{" +
                "bufferSize=" + bufferSize +
                ", flushIntervalSeconds=" + flushIntervalSeconds +
                ", retentionDays=" + retentionDays +
                ", backupEnabled=" + backupEnabled +
                ", properties=" + properties +
                '}';
    }
}
