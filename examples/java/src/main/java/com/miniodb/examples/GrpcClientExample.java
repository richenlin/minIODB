package com.miniodb.examples;

import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.Metadata;
import io.grpc.stub.MetadataUtils;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import com.google.protobuf.Struct;
import com.google.protobuf.Value;
import com.google.protobuf.Timestamp;

import java.time.Instant;
import java.util.concurrent.TimeUnit;

/**
 * MinIODB Java gRPC客户端示例
 * 演示如何使用Java客户端连接MinIODB系统
 */
public class GrpcClientExample {
    private static final Logger logger = LoggerFactory.getLogger(GrpcClientExample.class);
    
    private static final String SERVER_HOST = "localhost";
    private static final int SERVER_PORT = 8080;
    private static final String JWT_TOKEN = "your-jwt-token-here";
    
    private final ManagedChannel channel;
    private final OlapServiceGrpc.OlapServiceBlockingStub blockingStub;
    private final ObjectMapper objectMapper;

    public GrpcClientExample() {
        // 创建gRPC通道
        this.channel = ManagedChannelBuilder.forAddress(SERVER_HOST, SERVER_PORT)
                .usePlaintext() // 生产环境中应使用TLS
                .build();
        
        // 创建带认证的stub
        Metadata metadata = new Metadata();
        Metadata.Key<String> authKey = Metadata.Key.of("authorization", Metadata.ASCII_STRING_MARSHALLER);
        metadata.put(authKey, "Bearer " + JWT_TOKEN);
        
        this.blockingStub = MetadataUtils.attachHeaders(
                OlapServiceGrpc.newBlockingStub(channel), metadata);
        
        this.objectMapper = new ObjectMapper();
        
        logger.info("gRPC客户端已连接到 {}:{}", SERVER_HOST, SERVER_PORT);
    }

    /**
     * 关闭连接
     */
    public void shutdown() throws InterruptedException {
        channel.shutdown().awaitTermination(5, TimeUnit.SECONDS);
        logger.info("gRPC连接已关闭");
    }

    /**
     * 健康检查示例
     */
    public void healthCheck() {
        try {
            logger.info("=== 健康检查 ===");
            
            HealthCheckRequest request = HealthCheckRequest.newBuilder().build();
            HealthCheckResponse response = blockingStub.healthCheck(request);
            
            logger.info("健康检查结果:");
            logger.info("  状态: {}", response.getStatus());
            logger.info("  时间: {}", response.getTimestamp());
            logger.info("  版本: {}", response.getVersion());
            logger.info("  详情: {}", response.getDetailsMap());
            
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
            
            // 准备负载数据
            Struct.Builder payloadBuilder = Struct.newBuilder();
            payloadBuilder.putFields("user_id", Value.newBuilder().setStringValue("user123").build());
            payloadBuilder.putFields("action", Value.newBuilder().setStringValue("login").build());
            payloadBuilder.putFields("score", Value.newBuilder().setNumberValue(95.5).build());
            payloadBuilder.putFields("success", Value.newBuilder().setBoolValue(true).build());
            
            // 创建写入请求
            WriteRequest request = WriteRequest.newBuilder()
                    .setId("user123")
                    .setTimestamp(Timestamp.newBuilder()
                            .setSeconds(Instant.now().getEpochSecond())
                            .build())
                    .setPayload(payloadBuilder.build())
                    .build();
            
            // 发送请求
            WriteResponse response = blockingStub.write(request);
            
            logger.info("数据写入结果:");
            logger.info("  成功: {}", response.getSuccess());
            logger.info("  消息: {}", response.getMessage());
            
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
            
            // 创建查询请求
            String sql = "SELECT COUNT(*) as total, AVG(score) as avg_score " +
                        "FROM table WHERE user_id = 'user123' AND timestamp >= '2024-01-01'";
            
            QueryRequest request = QueryRequest.newBuilder()
                    .setSql(sql)
                    .build();
            
            // 发送查询
            QueryResponse response = blockingStub.query(request);
            
            logger.info("查询结果:");
            logger.info("  SQL: {}", sql);
            logger.info("  结果: {}", response.getResultJson());
            
            // 解析JSON结果
            try {
                ObjectNode result = (ObjectNode) objectMapper.readTree(response.getResultJson());
                logger.info("  解析后结果: {}", result.toPrettyString());
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
            
            TriggerBackupRequest request = TriggerBackupRequest.newBuilder()
                    .setId("user123")
                    .setDay("2024-01-15")
                    .build();
            
            TriggerBackupResponse response = blockingStub.triggerBackup(request);
            
            logger.info("备份结果:");
            logger.info("  成功: {}", response.getSuccess());
            logger.info("  消息: {}", response.getMessage());
            logger.info("  备份文件数: {}", response.getFilesBackedUp());
            
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
            
            // 按时间范围恢复
            TimeRangeFilter timeRange = TimeRangeFilter.newBuilder()
                    .setStartDate("2024-01-01")
                    .setEndDate("2024-01-15")
                    .addIds("user123")
                    .build();
            
            RecoverDataRequest request = RecoverDataRequest.newBuilder()
                    .setTimeRange(timeRange)
                    .setForceOverwrite(false)
                    .build();
            
            RecoverDataResponse response = blockingStub.recoverData(request);
            
            logger.info("恢复结果:");
            logger.info("  成功: {}", response.getSuccess());
            logger.info("  消息: {}", response.getMessage());
            logger.info("  恢复文件数: {}", response.getFilesRecovered());
            logger.info("  恢复的键: {}", response.getRecoveredKeysList());
            
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
            
            GetStatsRequest request = GetStatsRequest.newBuilder().build();
            GetStatsResponse response = blockingStub.getStats(request);
            
            logger.info("系统统计:");
            logger.info("  时间戳: {}", response.getTimestamp());
            logger.info("  缓冲区统计: {}", response.getBufferStatsMap());
            logger.info("  Redis统计: {}", response.getRedisStatsMap());
            logger.info("  MinIO统计: {}", response.getMinioStatsMap());
            
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
            
            GetNodesRequest request = GetNodesRequest.newBuilder().build();
            GetNodesResponse response = blockingStub.getNodes(request);
            
            logger.info("节点信息:");
            logger.info("  总数: {}", response.getTotal());
            logger.info("  节点列表:");
            
            for (NodeInfo node : response.getNodesList()) {
                logger.info("    - ID: {}, 状态: {}, 类型: {}, 地址: {}, 最后活跃: {}", 
                           node.getId(), node.getStatus(), node.getType(), 
                           node.getAddress(), node.getLastSeen());
            }
            
        } catch (Exception e) {
            logger.error("获取节点信息失败", e);
        }
    }

    /**
     * 运行所有示例
     */
    public void runAllExamples() {
        logger.info("开始运行MinIODB Java gRPC客户端示例...");
        
        healthCheck();
        writeData();
        queryData();
        triggerBackup();
        recoverData();
        getStats();
        getNodes();
        
        logger.info("所有示例运行完成!");
    }

    public static void main(String[] args) {
        GrpcClientExample client = new GrpcClientExample();
        
        try {
            client.runAllExamples();
        } finally {
            try {
                client.shutdown();
            } catch (InterruptedException e) {
                logger.error("关闭连接时发生异常", e);
            }
        }
    }
} 