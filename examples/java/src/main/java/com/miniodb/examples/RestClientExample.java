package com.miniodb.examples;

import okhttp3.*;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.time.Instant;
import java.util.concurrent.TimeUnit;

/**
 * MinIODB Java REST客户端示例
 * 演示如何使用Java HTTP客户端连接MinIODB REST API
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
     * 数据写入示例
     */
    public void writeData() {
        try {
            logger.info("=== 数据写入 ===");
            
            // 构建请求数据
            ObjectNode requestData = objectMapper.createObjectNode();
            requestData.put("id", "user123");
            requestData.put("timestamp", Instant.now().toString());
            
            ObjectNode payload = objectMapper.createObjectNode();
            payload.put("user_id", "user123");
            payload.put("action", "login");
            payload.put("score", 95.5);
            payload.put("success", true);
            requestData.set("payload", payload);
            
            RequestBody body = RequestBody.create(
                    objectMapper.writeValueAsString(requestData), JSON);
            
            Request request = createAuthenticatedRequest()
                    .url(BASE_URL + "/v1/data")
                    .post(body)
                    .build();
            
            JsonNode response = executeRequest(request);
            
            logger.info("数据写入结果:");
            logger.info("  成功: {}", response.get("success").asBoolean());
            logger.info("  消息: {}", response.get("message").asText());
            logger.info("  节点ID: {}", response.get("node_id").asText());
            
        } catch (Exception e) {
            logger.error("数据写入失败", e);
        }
    }

    /**
     * 数据查询示例
     */
    public void queryData() {
        try {
            logger.info("=== 数据查询 ===");
            
            ObjectNode queryData = objectMapper.createObjectNode();
            String sql = "SELECT COUNT(*) as total, AVG(score) as avg_score " +
                        "FROM table WHERE user_id = 'user123' AND timestamp >= '2024-01-01'";
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
            
            ObjectNode recoverData = objectMapper.createObjectNode();
            
            // 按时间范围恢复
            ObjectNode timeRange = objectMapper.createObjectNode();
            timeRange.put("start_date", "2024-01-01");
            timeRange.put("end_date", "2024-01-15");
            timeRange.set("ids", objectMapper.createArrayNode().add("user123"));
            
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
            
            JsonNode nodes = response.get("nodes");
            if (nodes.isArray()) {
                for (JsonNode node : nodes) {
                    logger.info("    - ID: {}, 状态: {}, 类型: {}, 地址: {}, 最后活跃: {}", 
                               node.get("id").asText(),
                               node.get("status").asText(),
                               node.get("type").asText(),
                               node.get("address").asText(),
                               node.get("last_seen").asLong());
                }
            }
            
        } catch (Exception e) {
            logger.error("获取节点信息失败", e);
        }
    }

    /**
     * 运行所有示例
     */
    public void runAllExamples() {
        logger.info("开始运行MinIODB Java REST客户端示例...");
        
        healthCheck();
        writeData();
        queryData();
        triggerBackup();
        recoverData();
        getStats();
        getNodes();
        
        logger.info("所有示例运行完成!");
    }

    /**
     * 清理资源
     */
    public void cleanup() {
        client.dispatcher().executorService().shutdown();
        client.connectionPool().evictAll();
        logger.info("REST客户端资源已清理");
    }

    public static void main(String[] args) {
        RestClientExample client = new RestClientExample();
        
        try {
            client.runAllExamples();
        } finally {
            client.cleanup();
        }
    }
} 