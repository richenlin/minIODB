package com.miniodb.examples;

import okhttp3.*;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.time.Instant;
import java.util.concurrent.TimeUnit;

/**
 * MinIODB Java REST客户端示例
 * 演示如何使用Java HTTP客户端连接MinIODB REST API
 * 包含表管理功能支持
 */
public class RestClientExample {
    private static final Logger logger = LoggerFactory.getLogger(RestClientExample.class);
    
    private static final String BASE_URL = "http://localhost:8081";
    private static final String JWT_TOKEN = "your-jwt-token-here";
    private static final MediaType JSON = MediaType.get("application/json; charset=utf-8");
    
    private final OkHttpClient client;
    private final ObjectMapper objectMapper;

    public RestClientExample() {
        this.client = new OkHttpClient.Builder()
                .connectTimeout(10, TimeUnit.SECONDS)
                .writeTimeout(10, TimeUnit.SECONDS)
                .readTimeout(30, TimeUnit.SECONDS)
                .build();
        
        this.objectMapper = new ObjectMapper();
        
        logger.info("REST客户端已配置，服务器地址: {}", BASE_URL);
    }

    /**
     * 创建带认证头的请求构建器
     */
    private Request.Builder createAuthenticatedRequest() {
        return new Request.Builder()
                .addHeader("Authorization", "Bearer " + JWT_TOKEN)
                .addHeader("Content-Type", "application/json");
    }

    /**
     * 发送HTTP请求并处理响应
     */
    private JsonNode executeRequest(Request request) throws IOException {
        try (Response response = client.newCall(request).execute()) {
            String responseBody = response.body().string();
            
            if (!response.isSuccessful()) {
                logger.error("请求失败: {} {}", response.code(), responseBody);
                throw new IOException("HTTP " + response.code() + ": " + responseBody);
            }
            
            return objectMapper.readTree(responseBody);
        }
    }

    /**
     * 健康检查示例
     */
    public void healthCheck() {
        try {
            logger.info("=== 健康检查 ===");
            
            Request request = new Request.Builder()
                    .url(BASE_URL + "/v1/health")
                    .get()
                    .build();
            
            JsonNode response = executeRequest(request);
            
            logger.info("健康检查结果:");
            logger.info("  状态: {}", response.get("status").asText());
            logger.info("  时间: {}", response.get("timestamp").asText());
            logger.info("  版本: {}", response.get("version").asText());
            logger.info("  详情: {}", response.get("details"));
            
        } catch (Exception e) {
            logger.error("健康检查失败", e);
        }
    }

    /**
     * 创建表示例
     */
    public void createTable() {
        try {
            logger.info("=== 创建表 ===");
            
            // 构建表配置
            ObjectNode config = objectMapper.createObjectNode();
            config.put("buffer_size", 1000);
            config.put("flush_interval_seconds", 30);
            config.put("retention_days", 365);
            config.put("backup_enabled", true);
            
            ObjectNode properties = objectMapper.createObjectNode();
            properties.put("description", "用户数据表");
            properties.put("owner", "user-service");
            config.set("properties", properties);
            
            // 构建请求数据
            ObjectNode requestData = objectMapper.createObjectNode();
            requestData.put("table_name", "users");
            requestData.set("config", config);
            requestData.put("if_not_exists", true);
            
            RequestBody body = RequestBody.create(
                    objectMapper.writeValueAsString(requestData), JSON);
            
            Request request = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/tables")
                    .post(body)
                    .build();
            
            JsonNode response = executeRequest(request);
            
            logger.info("创建表结果:");
            logger.info("  成功: {}", response.get("success").asBoolean());
            logger.info("  消息: {}", response.get("message").asText());
            
        } catch (Exception e) {
            logger.error("创建表失败", e);
        }
    }

    /**
     * 列出表示例
     */
    public void listTables() {
        try {
            logger.info("=== 列出表 ===");
            
            Request request = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/tables")
                    .get()
                    .build();
            
            JsonNode response = executeRequest(request);
            
            logger.info("表列表:");
            logger.info("  总数: {}", response.get("total").asInt());
            
            ArrayNode tables = (ArrayNode) response.get("tables");
            for (JsonNode table : tables) {
                logger.info("  - 表名: {}, 状态: {}, 创建时间: {}", 
                    table.get("name").asText(),
                    table.get("status").asText(),
                    table.get("created_at").asText());
            }
            
        } catch (Exception e) {
            logger.error("列出表失败", e);
        }
    }

    /**
     * 描述表示例
     */
    public void describeTable() {
        try {
            logger.info("=== 描述表 ===");
            
            Request request = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/tables/users")
                    .get()
                    .build();
            
            JsonNode response = executeRequest(request);
            JsonNode tableInfo = response.get("table_info");
            JsonNode stats = response.get("stats");
            JsonNode config = tableInfo.get("config");
            
            logger.info("表详情:");
            logger.info("  表名: {}", tableInfo.get("name").asText());
            logger.info("  状态: {}", tableInfo.get("status").asText());
            logger.info("  创建时间: {}", tableInfo.get("created_at").asText());
            logger.info("  最后写入: {}", tableInfo.get("last_write").asText());
            logger.info("  配置: 缓冲区大小={}, 刷新间隔={}s, 保留天数={}", 
                config.get("buffer_size").asInt(),
                config.get("flush_interval_seconds").asInt(),
                config.get("retention_days").asInt());
            logger.info("  统计: 记录数={}, 文件数={}, 大小={}字节", 
                stats.get("record_count").asLong(),
                stats.get("file_count").asLong(),
                stats.get("size_bytes").asLong());
            
        } catch (Exception e) {
            logger.error("描述表失败", e);
        }
    }

    /**
     * 数据写入示例（支持表）
     */
    public void writeData() {
        try {
            logger.info("=== 数据写入 ===");
            
            // 构建请求数据
            ObjectNode requestData = objectMapper.createObjectNode();
            requestData.put("table", "users");  // 指定表名
            requestData.put("id", "user123");
            requestData.put("timestamp", Instant.now().toString());
            
            ObjectNode payload = objectMapper.createObjectNode();
            payload.put("user_id", "user123");
            payload.put("action", "login");
            payload.put("score", 95.5);
            payload.put("success", true);
            
            ObjectNode metadata = objectMapper.createObjectNode();
            metadata.put("browser", "Chrome");
            metadata.put("ip", "192.168.1.100");
            payload.set("metadata", metadata);
            
            requestData.set("payload", payload);
            
            RequestBody body = RequestBody.create(
                    objectMapper.writeValueAsString(requestData), JSON);
            
            Request request = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/data")
                    .post(body)
                    .build();
            
            JsonNode response = executeRequest(request);
            
            logger.info("数据写入结果:");
            logger.info("  表名: users");
            logger.info("  成功: {}", response.get("success").asBoolean());
            logger.info("  消息: {}", response.get("message").asText());
            logger.info("  节点ID: {}", response.get("node_id").asText());
            
        } catch (Exception e) {
            logger.error("数据写入失败", e);
        }
    }

    /**
     * 数据查询示例（使用表名）
     */
    public void queryData() {
        try {
            logger.info("=== 数据查询 ===");
            
            ObjectNode queryData = objectMapper.createObjectNode();
            String sql = "SELECT COUNT(*) as total, AVG(score) as avg_score " +
                        "FROM users WHERE user_id = 'user123' AND timestamp >= '2024-01-01'";
            queryData.put("sql", sql);
            
            RequestBody body = RequestBody.create(
                    objectMapper.writeValueAsString(queryData), JSON);
            
            Request request = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/query")
                    .post(body)
                    .build();
            
            JsonNode response = executeRequest(request);
            
            logger.info("查询结果:");
            logger.info("  SQL: {}", sql);
            logger.info("  结果JSON: {}", response.get("result_json").asText());
            
            // 解析结果JSON
            try {
                JsonNode resultData = objectMapper.readTree(response.get("result_json").asText());
                logger.info("  解析后结果: {}", resultData.toPrettyString());
            } catch (Exception e) {
                logger.warn("JSON解析失败: {}", e.getMessage());
            }
            
        } catch (Exception e) {
            logger.error("数据查询失败", e);
        }
    }

    /**
     * 跨表查询示例
     */
    public void crossTableQuery() {
        try {
            logger.info("=== 跨表查询 ===");
            
            // 首先创建订单表
            ObjectNode orderConfig = objectMapper.createObjectNode();
            orderConfig.put("buffer_size", 2000);
            orderConfig.put("flush_interval_seconds", 15);
            orderConfig.put("retention_days", 2555);
            orderConfig.put("backup_enabled", true);
            
            ObjectNode orderProperties = objectMapper.createObjectNode();
            orderProperties.put("description", "订单数据表");
            orderProperties.put("owner", "order-service");
            orderConfig.set("properties", orderProperties);
            
            ObjectNode orderTableRequest = objectMapper.createObjectNode();
            orderTableRequest.put("table_name", "orders");
            orderTableRequest.set("config", orderConfig);
            orderTableRequest.put("if_not_exists", true);
            
            RequestBody createOrderTableBody = RequestBody.create(
                    objectMapper.writeValueAsString(orderTableRequest), JSON);
            
            Request createOrderTableReq = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/tables")
                    .post(createOrderTableBody)
                    .build();
            
            try {
                executeRequest(createOrderTableReq);
            } catch (Exception e) {
                logger.warn("创建订单表失败（可能已存在）: {}", e.getMessage());
            }
            
            // 写入订单数据
            ObjectNode orderData = objectMapper.createObjectNode();
            orderData.put("table", "orders");
            orderData.put("id", "order456");
            orderData.put("timestamp", Instant.now().toString());
            
            ObjectNode orderPayload = objectMapper.createObjectNode();
            orderPayload.put("order_id", "order456");
            orderPayload.put("user_id", "user123");
            orderPayload.put("amount", 299.99);
            orderPayload.put("status", "completed");
            orderData.set("payload", orderPayload);
            
            RequestBody orderBody = RequestBody.create(
                    objectMapper.writeValueAsString(orderData), JSON);
            
            Request orderRequest = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/data")
                    .post(orderBody)
                    .build();
            
            try {
                executeRequest(orderRequest);
            } catch (Exception e) {
                logger.warn("写入订单数据失败: {}", e.getMessage());
            }
            
            // 执行跨表查询
            ObjectNode crossQueryData = objectMapper.createObjectNode();
            String crossSql = "SELECT u.user_id, u.action, o.order_id, o.amount " +
                            "FROM users u JOIN orders o ON u.user_id = o.user_id " +
                            "WHERE u.user_id = 'user123'";
            crossQueryData.put("sql", crossSql);
            
            RequestBody crossQueryBody = RequestBody.create(
                    objectMapper.writeValueAsString(crossQueryData), JSON);
            
            Request crossQueryRequest = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/query")
                    .post(crossQueryBody)
                    .build();
            
            JsonNode response = executeRequest(crossQueryRequest);
            
            logger.info("跨表查询结果:");
            logger.info("  SQL: {}", crossSql);
            logger.info("  结果JSON: {}", response.get("result_json").asText());
            
        } catch (Exception e) {
            logger.error("跨表查询失败", e);
        }
    }

    /**
     * 触发备份示例
     */
    public void triggerBackup() {
        try {
            logger.info("=== 触发备份 ===");
            
            ObjectNode backupData = objectMapper.createObjectNode();
            backupData.put("id", "user123");
            backupData.put("day", "2024-01-15");
            
            RequestBody body = RequestBody.create(
                    objectMapper.writeValueAsString(backupData), JSON);
            
            Request request = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/backup/trigger")
                    .post(body)
                    .build();
            
            JsonNode response = executeRequest(request);
            
            logger.info("备份结果:");
            logger.info("  成功: {}", response.get("success").asBoolean());
            logger.info("  消息: {}", response.get("message").asText());
            logger.info("  备份文件数: {}", response.get("files_backed_up").asInt());
            
        } catch (Exception e) {
            logger.error("备份操作失败", e);
        }
    }

    /**
     * 数据恢复示例
     */
    public void recoverData() {
        try {
            logger.info("=== 数据恢复 ===");
            
            ObjectNode timeRange = objectMapper.createObjectNode();
            timeRange.put("start_date", "2024-01-01");
            timeRange.put("end_date", "2024-01-15");
            ArrayNode ids = objectMapper.createArrayNode();
            ids.add("user123");
            timeRange.set("ids", ids);
            
            ObjectNode recoverData = objectMapper.createObjectNode();
            recoverData.set("time_range", timeRange);
            recoverData.put("force_overwrite", false);
            
            RequestBody body = RequestBody.create(
                    objectMapper.writeValueAsString(recoverData), JSON);
            
            Request request = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/backup/recover")
                    .post(body)
                    .build();
            
            JsonNode response = executeRequest(request);
            
            logger.info("恢复结果:");
            logger.info("  成功: {}", response.get("success").asBoolean());
            logger.info("  消息: {}", response.get("message").asText());
            logger.info("  恢复文件数: {}", response.get("files_recovered").asInt());
            logger.info("  恢复的键: {}", response.get("recovered_keys"));
            
        } catch (Exception e) {
            logger.error("数据恢复失败", e);
        }
    }

    /**
     * 获取系统统计信息
     */
    public void getStats() {
        try {
            logger.info("=== 系统统计 ===");
            
            Request request = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/stats")
                    .get()
                    .build();
            
            JsonNode response = executeRequest(request);
            
            logger.info("系统统计:");
            logger.info("  时间戳: {}", response.get("timestamp").asText());
            logger.info("  缓冲区统计: {}", response.get("buffer_stats"));
            logger.info("  Redis统计: {}", response.get("redis_stats"));
            logger.info("  MinIO统计: {}", response.get("minio_stats"));
            
        } catch (Exception e) {
            logger.error("获取统计信息失败", e);
        }
    }

    /**
     * 获取节点信息
     */
    public void getNodes() {
        try {
            logger.info("=== 节点信息 ===");
            
            Request request = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/nodes")
                    .get()
                    .build();
            
            JsonNode response = executeRequest(request);
            
            logger.info("节点信息:");
            logger.info("  总数: {}", response.get("total").asInt());
            logger.info("  节点列表:");
            
            ArrayNode nodes = (ArrayNode) response.get("nodes");
            for (JsonNode node : nodes) {
                logger.info("    - ID: {}, 状态: {}, 类型: {}, 地址: {}, 最后活跃: {}",
                    node.get("id").asText(),
                    node.get("status").asText(),
                    node.get("type").asText(),
                    node.get("address").asText(),
                    node.get("last_seen").asLong());
            }
            
        } catch (Exception e) {
            logger.error("获取节点信息失败", e);
        }
    }

    /**
     * 删除表示例（可选，用于清理）
     */
    public void dropTable() {
        try {
            logger.info("=== 删除表 ===");
            
            ObjectNode dropData = objectMapper.createObjectNode();
            dropData.put("table_name", "orders");
            dropData.put("if_exists", true);
            dropData.put("cascade", true);
            
            RequestBody body = RequestBody.create(
                    objectMapper.writeValueAsString(dropData), JSON);
            
            Request request = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/tables/orders")
                    .delete(body)
                    .build();
            
            JsonNode response = executeRequest(request);
            
            logger.info("删除表结果:");
            logger.info("  成功: {}", response.get("success").asBoolean());
            logger.info("  消息: {}", response.get("message").asText());
            logger.info("  删除文件数: {}", response.get("files_deleted").asInt());
            
        } catch (Exception e) {
            logger.error("删除表失败", e);
        }
    }

    /**
     * 运行所有示例
     */
    public void runAllExamples() {
        logger.info("开始运行MinIODB Java REST客户端示例（包含表管理功能）...");
        
        // 按顺序执行示例
        healthCheck();
        sleep(500);
        
        createTable();
        sleep(500);
        
        listTables();
        sleep(500);
        
        describeTable();
        sleep(500);
        
        writeData();
        sleep(500);
        
        queryData();
        sleep(500);
        
        crossTableQuery();
        sleep(500);
        
        triggerBackup();
        sleep(500);
        
        recoverData();
        sleep(500);
        
        getStats();
        sleep(500);
        
        getNodes();
        sleep(500);
        
        // dropTable(); // 可选，用于清理
        
        logger.info("所有示例运行完成!");
    }

    /**
     * 短暂休眠
     */
    private void sleep(int milliseconds) {
        try {
            Thread.sleep(milliseconds);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }

    /**
     * 清理资源
     */
    public void cleanup() {
        if (client != null) {
            client.dispatcher().executorService().shutdown();
            client.connectionPool().evictAll();
        }
    }

    public static void main(String[] args) {
        RestClientExample example = new RestClientExample();
        try {
            example.runAllExamples();
        } finally {
            example.cleanup();
        }
    }
} 