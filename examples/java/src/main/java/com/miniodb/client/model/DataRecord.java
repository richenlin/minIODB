package com.miniodb.client.model;

import java.time.Instant;
import java.util.Map;
import java.util.Objects;

/**
 * 数据记录模型
 * 
 * 表示 MinIODB 中的一条数据记录。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class DataRecord {
    
    private final String id;
    private final Instant timestamp;
    private final Map<String, Object> payload;
    
    private DataRecord(Builder builder) {
        this.id = builder.id;
        this.timestamp = builder.timestamp;
        this.payload = builder.payload;
    }
    
    /**
     * 获取记录 ID
     * @return 记录 ID
     */
    public String getId() {
        return id;
    }
    
    /**
     * 获取时间戳
     * @return 时间戳
     */
    public Instant getTimestamp() {
        return timestamp;
    }
    
    /**
     * 获取数据负载
     * @return 数据负载映射
     */
    public Map<String, Object> getPayload() {
        return payload;
    }
    
    /**
     * 创建数据记录建造者
     * @return 数据记录建造者实例
     */
    public static Builder builder() {
        return new Builder();
    }
    
    /**
     * 数据记录建造者类
     */
    public static class Builder {
        private String id;
        private Instant timestamp;
        private Map<String, Object> payload;
        
        /**
         * 设置记录 ID
         * @param id 记录 ID
         * @return 建造者实例
         */
        public Builder id(String id) {
            this.id = id;
            return this;
        }
        
        /**
         * 设置时间戳
         * @param timestamp 时间戳
         * @return 建造者实例
         */
        public Builder timestamp(Instant timestamp) {
            this.timestamp = timestamp;
            return this;
        }
        
        /**
         * 设置数据负载
         * @param payload 数据负载映射
         * @return 建造者实例
         */
        public Builder payload(Map<String, Object> payload) {
            this.payload = payload;
            return this;
        }
        
        /**
         * 构建数据记录实例
         * @return 数据记录实例
         */
        public DataRecord build() {
            Objects.requireNonNull(id, "记录 ID 不能为空");
            Objects.requireNonNull(timestamp, "时间戳不能为空");
            Objects.requireNonNull(payload, "数据负载不能为空");
            return new DataRecord(this);
        }
    }
    
    @Override
    public boolean equals(Object o) {
        if (this == o) return true;
        if (o == null || getClass() != o.getClass()) return false;
        DataRecord that = (DataRecord) o;
        return Objects.equals(id, that.id) &&
                Objects.equals(timestamp, that.timestamp) &&
                Objects.equals(payload, that.payload);
    }
    
    @Override
    public int hashCode() {
        return Objects.hash(id, timestamp, payload);
    }
    
    @Override
    public String toString() {
        return "DataRecord{" +
                "id='" + id + '\'' +
                ", timestamp=" + timestamp +
                ", payload=" + payload +
                '}';
    }
}
