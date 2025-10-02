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
 * 包含表管理功能支持
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
     * 创建表示例
     */
    public void createTable() {
        try {
            logger.info("=== 创建表 ===");
            
            // 创建表配置
            TableConfig config = TableConfig.newBuilder()
                    .setBufferSize(1000)
                    .setFlushIntervalSeconds(30)
                    .setRetentionDays(365)
                    .setBackupEnabled(true)
                    .putProperties("description", "用户数据表")
                    .putProperties("owner", "user-service")
                    .build();
            
            // 创建表请求
            CreateTableRequest request = CreateTableRequest.newBuilder()
                    .setTableName("users")
                    .setConfig(config)
                    .setIfNotExists(true)
                    .build();
            
            CreateTableResponse response = blockingStub.createTable(request);
            
            logger.info("创建表结果:");
            logger.info("  成功: {}", response.getSuccess());
            logger.info("  消息: {}", response.getMessage());
            
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
            
            ListTablesRequest request = ListTablesRequest.newBuilder().build();
            ListTablesResponse response = blockingStub.listTables(request);
            
            logger.info("表列表:");
            logger.info("  总数: {}", response.getTotal());
            
            for (TableInfo table : response.getTablesList()) {
                logger.info("  - 表名: {}, 状态: {}, 创建时间: {}", 
                    table.getName(), table.getStatus(), table.getCreatedAt());
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
            
            DescribeTableRequest request = DescribeTableRequest.newBuilder()
                    .setTableName("users")
                    .build();
            
            DescribeTableResponse response = blockingStub.describeTable(request);
            TableInfo tableInfo = response.getTableInfo();
            TableStats stats = response.getStats();
            TableConfig config = tableInfo.getConfig();
            
            logger.info("表详情:");
            logger.info("  表名: {}", tableInfo.getName());
            logger.info("  状态: {}", tableInfo.getStatus());
            logger.info("  创建时间: {}", tableInfo.getCreatedAt());
            logger.info("  最后写入: {}", tableInfo.getLastWrite());
            logger.info("  配置: 缓冲区大小={}, 刷新间隔={}s, 保留天数={}", 
                config.getBufferSize(), config.getFlushIntervalSeconds(), config.getRetentionDays());
            logger.info("  统计: 记录数={}, 文件数={}, 大小={}字节", 
                stats.getRecordCount(), stats.getFileCount(), stats.getSizeBytes());
            
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
            
            // 准备负载数据
            Struct.Builder payloadBuilder = Struct.newBuilder();
            payloadBuilder.putFields("user_id", Value.newBuilder().setStringValue("user123").build());
            payloadBuilder.putFields("action", Value.newBuilder().setStringValue("login").build());
            payloadBuilder.putFields("score", Value.newBuilder().setNumberValue(95.5).build());
            payloadBuilder.putFields("success", Value.newBuilder().setBoolValue(true).build());
            
            // 添加元数据
            Struct.Builder metadataBuilder = Struct.newBuilder();
            metadataBuilder.putFields("browser", Value.newBuilder().setStringValue("Chrome").build());
            metadataBuilder.putFields("ip", Value.newBuilder().setStringValue("192.168.1.100").build());
            payloadBuilder.putFields("metadata", Value.newBuilder().setStructValue(metadataBuilder.build()).build());
            
            // 创建写入请求（包含表名）
            WriteRequest request = WriteRequest.newBuilder()
                    .setTable("users")  // 指定表名
                    .setId("user123")
                    .setTimestamp(Timestamp.newBuilder()
                            .setSeconds(Instant.now().getEpochSecond())
                            .build())
                    .setPayload(payloadBuilder.build())
                    .build();
            
            // 发送请求
            WriteResponse response = blockingStub.write(request);
            
            logger.info("数据写入结果:");
            logger.info("  表名: users");
            logger.info("  成功: {}", response.getSuccess());
            logger.info("  消息: {}", response.getMessage());
            
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
            
            // 创建查询请求
            String sql = "SELECT COUNT(*) as total, AVG(score) as avg_score " +
                        "FROM users WHERE user_id = 'user123' AND timestamp >= '2024-01-01'";
            
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
     * 跨表查询示例
     */
    public void crossTableQuery() {
        try {
            logger.info("=== 跨表查询 ===");
            
            // 首先创建订单表
            TableConfig orderConfig = TableConfig.newBuilder()
                    .setBufferSize(2000)
                    .setFlushIntervalSeconds(15)
                    .setRetentionDays(2555)
                    .setBackupEnabled(true)
                    .putProperties("description", "订单数据表")
                    .putProperties("owner", "order-service")
                    .build();
            
            CreateTableRequest orderTableRequest = CreateTableRequest.newBuilder()
                    .setTableName("orders")
                    .setConfig(orderConfig)
                    .setIfNotExists(true)
                    .build();
            
            try {
                blockingStub.createTable(orderTableRequest);
            } catch (Exception e) {
                logger.warn("创建订单表失败（可能已存在）: {}", e.getMessage());
            }
            
            // 写入订单数据
            Struct.Builder orderPayloadBuilder = Struct.newBuilder();
            orderPayloadBuilder.putFields("order_id", Value.newBuilder().setStringValue("order456").build());
            orderPayloadBuilder.putFields("user_id", Value.newBuilder().setStringValue("user123").build());
            orderPayloadBuilder.putFields("amount", Value.newBuilder().setNumberValue(299.99).build());
            orderPayloadBuilder.putFields("status", Value.newBuilder().setStringValue("completed").build());
            
            WriteRequest orderRequest = WriteRequest.newBuilder()
                    .setTable("orders")
                    .setId("order456")
                    .setTimestamp(Timestamp.newBuilder()
                            .setSeconds(Instant.now().getEpochSecond())
                            .build())
                    .setPayload(orderPayloadBuilder.build())
                    .build();
            
            try {
                blockingStub.write(orderRequest);
            } catch (Exception e) {
                logger.warn("写入订单数据失败: {}", e.getMessage());
            }
            
            // 执行跨表查询
            String crossSql = "SELECT u.user_id, u.action, o.order_id, o.amount " +
                            "FROM users u JOIN orders o ON u.user_id = o.user_id " +
                            "WHERE u.user_id = 'user123'";
            
            QueryRequest crossQueryRequest = QueryRequest.newBuilder()
                    .setSql(crossSql)
                    .build();
            
            QueryResponse response = blockingStub.query(crossQueryRequest);
            
            logger.info("跨表查询结果:");
            logger.info("  SQL: {}", crossSql);
            logger.info("  结果: {}", response.getResultJson());
            
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
     * 删除表示例（可选，用于清理）
     */
    public void dropTable() {
        try {
            logger.info("=== 删除表 ===");
            
            DropTableRequest request = DropTableRequest.newBuilder()
                    .setTableName("orders")
                    .setIfExists(true)
                    .setCascade(true)
                    .build();
            
            DropTableResponse response = blockingStub.dropTable(request);
            
            logger.info("删除表结果:");
            logger.info("  成功: {}", response.getSuccess());
            logger.info("  消息: {}", response.getMessage());
            logger.info("  删除文件数: {}", response.getFilesDeleted());
            
        } catch (Exception e) {
            logger.error("删除表失败", e);
        }
    }

    /**
     * 运行所有示例
     */
    public void runAllExamples() {
        logger.info("开始运行MinIODB Java gRPC客户端示例（包含表管理功能）...");
        
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

    public static void main(String[] args) {
        GrpcClientExample client = new GrpcClientExample();
        
        try {
            client.runAllExamples();
        } finally {
            try {
                client.shutdown();
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                logger.error("关闭gRPC连接时被中断", e);
            }
        }
    }
} 