package com.miniodb.sdk;

import com.google.protobuf.Struct;
import com.google.protobuf.Timestamp;
import com.google.protobuf.util.JsonFormat;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.time.Instant;
import java.util.*;
import java.util.concurrent.TimeUnit;
import java.util.stream.Collectors;

/**
 * MinIODB Java gRPC 客户端示例
 *
 * 本示例展示如何使用 gRPC 调用 MinIODB 的各种 API
 * 包括数据操作、表管理、流式操作等
 *
 * 使用方法:
 *   mvn compile exec:java -Dexec.args="-addr localhost:8080"
 *
 * 注意: 需要先编译 proto 文件生成 Java 代码
 */
public class MinioDBGrpcClient {

    private static final Logger logger = LoggerFactory.getLogger(MinioDBGrpcClient.class);

    private final ManagedChannel channel;
    // 注意: 需要从生成的代码导入
    // private final MinIODBServiceGrpc.MinIODBServiceBlockingStub blockingStub;
    // private final MinIODBServiceGrpc.MinIODBServiceStub asyncStub;
    private final String addr;

    /**
     * 创建 MinIODB gRPC 客户端
     */
    public MinioDBGrpcClient(String addr) {
        this.addr = addr;
        this.channel = ManagedChannelBuilder.forTarget(addr)
                .usePlaintext()
                .build();

        // 注意: 需要从生成的代码导入
        // this.blockingStub = MinIODBServiceGrpc.newBlockingStub(channel);
        // this.asyncStub = MinIODBServiceGrpc.newStub(channel);

        logger.info("Connected to MinIODB at {}", addr);
    }

    /**
     * 关闭连接
     */
    public void shutdown() throws InterruptedException {
        if (channel != null && !channel.isShutdown()) {
            channel.shutdown().awaitTermination(5, TimeUnit.SECONDS);
            logger.info("Connection closed");
        }
    }

    // ===============================================
    // 数据操作
    // ===============================================

    /**
     * 写入数据
     */
    public boolean writeData(String table, String recordId, Map<String, Object> payload) {
        logger.info("Writing data to table '{}', ID: {}", table, recordId);

        // 注意: 实际使用时使用生成的 protobuf 类
        // DataRecord record = DataRecord.newBuilder()
        //         .setId(recordId)
        //         .setTimestamp(toProtobufTimestamp(Instant.now()))
        //         .setPayload(toProtobufStruct(payload))
        //         .build();
        //
        // WriteDataRequest request = WriteDataRequest.newBuilder()
        //         .setTable(table)
        //         .setData(record)
        //         .build();
        //
        // WriteDataResponse response = blockingStub.writeData(request);

        // 模拟响应
        boolean success = true;
        String message = String.format("Data successfully ingested for table: %s, ID: %s", table, recordId);
        String nodeId = "node-1";

        if (success) {
            logger.info("✓ Data written successfully to table '{}', ID: {}, Node: {}", table, recordId, nodeId);
        } else {
            logger.error("✗ Write failed: {}", message);
        }

        return success;
    }

    /**
     * 查询数据
     */
    public String queryData(String sql, int limit) throws Exception {
        logger.info("Executing query: {}", sql);

        // 注意: 实际使用时使用生成的 protobuf 类
        // QueryDataRequest request = QueryDataRequest.newBuilder()
        //         .setSql(sql)
        //         .setLimit(limit)
        //         .build();
        //
        // QueryDataResponse response = blockingStub.queryData(request);

        // 模拟响应
        String resultJson = "[{\"id\":\"user_0\",\"name\":\"User 0\",\"email\":\"user0@example.com\",\"age\":25}," +
                "{\"id\":\"user_1\",\"name\":\"User 1\",\"email\":\"user1@example.com\",\"age\":26}]";
        boolean hasMore = false;
        String nextCursor = "";

        logger.info("✓ Query executed successfully, has_more: {}", hasMore);
        return resultJson;
    }

    /**
     * 更新数据
     */
    public boolean updateData(String table, String recordId, Map<String, Object> payload) {
        logger.info("Updating data in table '{}', ID: {}", table, recordId);

        // 注意: 实际使用时使用生成的 protobuf 类
        // DataRecord record = DataRecord.newBuilder()
        //         .setId(recordId)
        //         .setTimestamp(toProtobufTimestamp(Instant.now()))
        //         .setPayload(toProtobufStruct(payload))
        //         .build();
        //
        // UpdateDataRequest request = UpdateDataRequest.newBuilder()
        //         .setTable(table)
        //         .setId(recordId)
        //         .setTimestamp(toProtobufTimestamp(Instant.now()))
        //         .setPayload(toProtobufStruct(payload))
        //         .build();
        //
        // UpdateDataResponse response = blockingStub.updateData(request);

        // 模拟响应
        boolean success = true;
        String message = String.format("Record %s updated successfully in table %s", recordId, table);
        String nodeId = "node-1";

        if (success) {
            logger.info("✓ Data updated successfully, ID: {}", recordId);
        } else {
            logger.error("✗ Update failed: {}", message);
        }

        return success;
    }

    /**
     * 删除数据
     */
    public int deleteData(String table, String recordId) {
        logger.info("Deleting data from table '{}', ID: {}", table, recordId);

        // 注意: 实际使用时使用生成的 protobuf 类
        // DeleteDataRequest request = DeleteDataRequest.newBuilder()
        //         .setTable(table)
        //         .setId(recordId)
        //         .setSoftDelete(false)
        //         .build();
        //
        // DeleteDataResponse response = blockingStub.deleteData(request);

        // 模拟响应
        boolean success = true;
        int deletedCount = 1;

        if (success) {
            logger.info("✓ Data deleted successfully, deleted_count: {}", deletedCount);
        } else {
            logger.error("✗ Delete failed");
        }

        return deletedCount;
    }

    // ===============================================
    // 流式操作
    // ===============================================

    /**
     * 批量流式写入
     */
    public long streamWrite(String table, List<Map<String, Object>> recordsData) {
        logger.info("Stream writing {} records to table '{}'", recordsData.size(), table);

        // 注意: 实际使用时使用生成的 protobuf 类
        // StreamObserver<StreamWriteRequest> requestObserver = asyncStub.streamWrite(
        //         new StreamObserver<StreamWriteResponse>() {
        //             @Override
        //             public void onNext(StreamWriteResponse response) {
        //                 logger.info("Received stream response: {}", response);
        //             }
        //
        //             @Override
        //             public void onError(Throwable t) {
        //                 logger.error("Stream error: {}", t.getMessage());
        //             }
        //
        //             @Override
        //             public void onCompleted() {
        //                 logger.info("Stream write completed");
        //             }
        //         });
        //
        // // 分批发送
        // int batchSize = 100;
        // for (int i = 0; i < recordsData.size(); i += batchSize) {
        //     int end = Math.min(i + batchSize, recordsData.size());
        //     List<DataRecord> batch = recordsData.subList(i, end).stream()
        //             .map(this::toDataRecord)
        //             .collect(Collectors.toList());
        //
        //     StreamWriteRequest request = StreamWriteRequest.newBuilder()
        //             .setTable(table)
        //             .addAllRecords(batch)
        //             .build();
        //     requestObserver.onNext(request);
        //
        //     logger.info("  Sent batch {}-{}", i, end - 1);
        // }
        //
        // requestObserver.onCompleted();

        // 模拟响应
        long recordsCount = recordsData.size();
        List<String> errors = Collections.emptyList();

        logger.info("✓ Stream write completed: {} records, {} errors", recordsCount, errors.size());
        return recordsCount;
    }

    // ===============================================
    // 表管理
    // ===============================================

    /**
     * 创建表
     */
    public boolean createTable(String tableName, TableConfig config) {
        logger.info("Creating table: {}", tableName);

        // 注意: 实际使用时使用生成的 protobuf 类
        // CreateTableRequest request = CreateTableRequest.newBuilder()
        //         .setTableName(tableName)
        //         .setConfig(toProtobufTableConfig(config))
        //         .setIfNotExists(true)
        //         .build();
        //
        // CreateTableResponse response = blockingStub.createTable(request);

        // 模拟响应
        boolean success = true;
        String message = String.format("Table %s created successfully", tableName);

        if (success) {
            logger.info("✓ Table '{}' created successfully", tableName);
        } else {
            logger.error("✗ Create table failed: {}", message);
        }

        return success;
    }

    /**
     * 列出表
     */
    public List<TableInfo> listTables(String pattern) {
        logger.info("Listing tables with pattern: {}", pattern);

        // 注意: 实际使用时使用生成的 protobuf 类
        // ListTablesRequest request = ListTablesRequest.newBuilder()
        //         .setPattern(pattern)
        //         .build();
        //
        // ListTablesResponse response = blockingStub.listTables(request);

        // 模拟响应
        List<TableInfo> tables = new ArrayList<>();
        tables.add(new TableInfo("users", "active", 20, 2, 1024000L));
        tables.add(new TableInfo("orders", "active", 100, 5, 5120000L));

        logger.info("✓ Listed {} tables", tables.size());
        return tables;
    }

    /**
     * 获取表信息
     */
    public TableInfo getTable(String tableName) {
        logger.info("Getting table info: {}", tableName);

        // 注意: 实际使用时使用生成的 protobuf 类
        // GetTableRequest request = GetTableRequest.newBuilder()
        //         .setTableName(tableName)
        //         .build();
        //
        // GetTableResponse response = blockingStub.getTable(request);

        // 模拟响应
        TableInfo tableInfo = new TableInfo(tableName, "active", 20, 2, 1024000L);

        logger.info("✓ Got table info for '{}'", tableName);
        return tableInfo;
    }

    /**
     * 删除表
     */
    public int deleteTable(String tableName) {
        logger.info("Deleting table: {}", tableName);

        // 注意: 实际使用时使用生成的 protobuf 类
        // DeleteTableRequest request = DeleteTableRequest.newBuilder()
        //         .setTableName(tableName)
        //         .setIfExists(false)
        //         .setCascade(false)
        //         .build();
        //
        // DeleteTableResponse response = blockingStub.deleteTable(request);

        // 模拟响应
        boolean success = true;
        int filesDeleted = 2;

        if (success) {
            logger.info("✓ Table '{}' deleted successfully, files deleted: {}", tableName, filesDeleted);
        } else {
            logger.error("✗ Delete table failed");
        }

        return filesDeleted;
    }

    // ===============================================
    // 元数据管理
    // ===============================================

    /**
     * 备份元数据
     */
    public String backupMetadata() {
        logger.info("Backing up metadata");

        // 注意: 实际使用时使用生成的 protobuf 类
        // BackupMetadataRequest request = BackupMetadataRequest.newBuilder()
        //         .setForce(false)
        //         .build();
        //
        // BackupMetadataResponse response = blockingStub.backupMetadata(request);

        // 模拟响应
        String backupId = "backup_" + System.currentTimeMillis();

        logger.info("✓ Metadata backed up successfully, ID: {}", backupId);
        return backupId;
    }

    /**
     * 恢复元数据
     */
    public RestoreResult restoreMetadata(boolean fromLatest) {
        logger.info("Restoring metadata from latest: {}", fromLatest);

        // 注意: 实际使用时使用生成的 protobuf 类
        // RestoreMetadataRequest request = RestoreMetadataRequest.newBuilder()
        //         .setFromLatest(fromLatest)
        //         .setDryRun(false)
        //         .setOverwrite(false)
        //         .setValidate(true)
        //         .setParallel(true)
        //         .build();
        //
        // RestoreMetadataResponse response = blockingStub.restoreMetadata(request);

        // 模拟响应
        RestoreResult result = new RestoreResult(
                true,
                "Metadata restored successfully",
                "backup_1234567890",
                100,
                95,
                3,
                2,
                "2.5s"
        );

        logger.info("✓ Metadata restored successfully: total={}, ok={}, skipped={}, errors={}",
                result.entriesTotal, result.entriesOk, result.entriesSkipped, result.entriesError);
        return result;
    }

    /**
     * 列出备份
     */
    public List<BackupInfo> listBackups(int days) {
        logger.info("Listing backups in the last {} days", days);

        // 注意: 实际使用时使用生成的 protobuf 类
        // ListBackupsRequest request = ListBackupsRequest.newBuilder()
        //         .setDays(days)
        //         .build();
        //
        // ListBackupsResponse response = blockingStub.listBackups(request);

        // 模拟响应
        List<BackupInfo> backups = new ArrayList<>();
        backups.add(new BackupInfo(
                "backup_1234567890",
                "node-1",
                Instant.now(),
                1024000L
        ));

        logger.info("✓ Listed {} backups", backups.size());
        return backups;
    }

    // ===============================================
    // 健康检查和监控
    // ===============================================

    /**
     * 健康检查
     */
    public HealthStatus healthCheck() {
        logger.info("Performing health check");

        // 注意: 实际使用时使用生成的 protobuf 类
        // HealthCheckRequest request = HealthCheckRequest.newBuilder().build();
        // HealthCheckResponse response = blockingStub.healthCheck(request);

        // 模拟响应
        HealthStatus status = new HealthStatus("healthy", "1.0.0");

        logger.info("✓ Health check: status={}, version={}", status.status, status.version);
        return status;
    }

    /**
     * 获取状态
     */
    public SystemStatus getStatus() {
        logger.info("Getting system status");

        // 注意: 实际使用时使用生成的 protobuf 类
        // GetStatusRequest request = GetStatusRequest.newBuilder().build();
        // GetStatusResponse response = blockingStub.getStatus(request);

        // 模拟响应
        Map<String, Long> bufferStats = new HashMap<>();
        bufferStats.put("total_tasks", 100L);
        bufferStats.put("completed_tasks", 95L);

        Map<String, Long> redisStats = new HashMap<>();
        redisStats.put("hits", 1000L);
        redisStats.put("misses", 100L);

        SystemStatus status = new SystemStatus(1, bufferStats, redisStats);

        logger.info("✓ Got status: total_nodes={}", status.totalNodes);
        return status;
    }

    // ===============================================
    // 辅助方法
    // ===============================================

    private Timestamp toProtobufTimestamp(Instant instant) {
        return Timestamp.newBuilder()
                .setSeconds(instant.getEpochSecond())
                .setNanos(instant.getNano())
                .build();
    }

    private Struct toProtobufStruct(Map<String, Object> map) {
        Struct.Builder builder = Struct.newBuilder();
        map.forEach((key, value) -> builder.putFields(key, toProtobufValue(value)));
        return builder.build();
    }

    private com.google.protobuf.Value toProtobufValue(Object value) {
        com.google.protobuf.Value.Builder builder = com.google.protobuf.Value.newBuilder();

        if (value == null) {
            builder.setNullValue(com.google.protobuf.NullValue.NULL_VALUE);
        } else if (value instanceof String) {
            builder.setStringValue((String) value);
        } else if (value instanceof Number) {
            builder.setNumberValue(((Number) value).doubleValue());
        } else if (value instanceof Boolean) {
            builder.setBoolValue((Boolean) value);
        } else if (value instanceof Map) {
            builder.setStructValue(toProtobufStruct((Map<String, Object>) value));
        } else if (value instanceof List) {
            com.google.protobuf.ListValue.Builder listBuilder = com.google.protobuf.ListValue.newBuilder();
            for (Object item : (List<?>) value) {
                listBuilder.addValues(toProtobufValue(item));
            }
            builder.setListValue(listBuilder.build());
        }

        return builder.build();
    }

    // ===============================================
    // 内部类
    // ===============================================

    public static class TableConfig {
        public int bufferSize;
        public int flushIntervalSeconds;
        public int retentionDays;
        public boolean backupEnabled;
        public String idStrategy;
        public boolean autoGenerateId;

        public TableConfig(int bufferSize, int flushIntervalSeconds, int retentionDays,
                          boolean backupEnabled, String idStrategy, boolean autoGenerateId) {
            this.bufferSize = bufferSize;
            this.flushIntervalSeconds = flushIntervalSeconds;
            this.retentionDays = retentionDays;
            this.backupEnabled = backupEnabled;
            this.idStrategy = idStrategy;
            this.autoGenerateId = autoGenerateId;
        }
    }

    public static class TableInfo {
        public String name;
        public String status;
        public long recordCount;
        public int fileCount;
        public long sizeBytes;

        public TableInfo(String name, String status, long recordCount, int fileCount, long sizeBytes) {
            this.name = name;
            this.status = status;
            this.recordCount = recordCount;
            this.fileCount = fileCount;
            this.sizeBytes = sizeBytes;
        }
    }

    public static class BackupInfo {
        public String objectName;
        public String nodeId;
        public Instant timestamp;
        public long size;

        public BackupInfo(String objectName, String nodeId, Instant timestamp, long size) {
            this.objectName = objectName;
            this.nodeId = nodeId;
            this.timestamp = timestamp;
            this.size = size;
        }
    }

    public static class HealthStatus {
        public String status;
        public String version;

        public HealthStatus(String status, String version) {
            this.status = status;
            this.version = version;
        }
    }

    public static class RestoreResult {
        public boolean success;
        public String message;
        public String backupFile;
        public int entriesTotal;
        public int entriesOk;
        public int entriesSkipped;
        public int entriesError;
        public String duration;

        public RestoreResult(boolean success, String message, String backupFile,
                           int entriesTotal, int entriesOk, int entriesSkipped,
                           int entriesError, String duration) {
            this.success = success;
            this.message = message;
            this.backupFile = backupFile;
            this.entriesTotal = entriesTotal;
            this.entriesOk = entriesOk;
            this.entriesSkipped = entriesSkipped;
            this.entriesError = entriesError;
            this.duration = duration;
        }
    }

    public static class SystemStatus {
        public int totalNodes;
        public Map<String, Long> bufferStats;
        public Map<String, Long> redisStats;

        public SystemStatus(int totalNodes, Map<String, Long> bufferStats, Map<String, Long> redisStats) {
            this.totalNodes = totalNodes;
            this.bufferStats = bufferStats;
            this.redisStats = redisStats;
        }
    }

    // ===============================================
    // 主程序
    // ===============================================

    public static void main(String[] args) throws Exception {
        String addr = "localhost:8080";

        // 解析命令行参数
        for (int i = 0; i < args.length; i++) {
            if ("-addr".equals(args[i]) && i + 1 < args.length) {
                addr = args[++i];
            }
        }

        // 创建客户端
        MinioDBGrpcClient client = new MinioDBGrpcClient(addr);

        try {
            System.out.println("========================================");
            System.out.println("MinIODB Java gRPC Client Example");
            System.out.println("Server: " + addr);
            System.out.println("========================================");

            // 1. 健康检查
            System.out.println("\n--- Health Check ---");
            client.healthCheck();

            // 2. 创建表
            System.out.println("\n--- Create Table ---");
            String tableName = "users";
            TableConfig config = new TableConfig(
                    1000, 60, 30,
                    true, "uuid", true
            );
            client.createTable(tableName, config);

            // 3. 写入数据
            System.out.println("\n--- Write Data ---");
            for (int i = 0; i < 5; i++) {
                Map<String, Object> payload = new HashMap<>();
                payload.put("name", "User " + i);
                payload.put("email", "user" + i + "@example.com");
                payload.put("age", 25 + i);
                payload.put("active", true);
                payload.put("created_at", Instant.now().toString());

                String recordId = "user_" + i;
                client.writeData(tableName, recordId, payload);

                Thread.sleep(100);
            }

            // 4. 查询数据
            System.out.println("\n--- Query Data ---");
            String result = client.queryData("SELECT * FROM " + tableName + " LIMIT 3", 3);
            System.out.println("Query result:\n" + result);

            // 5. 更新数据
            System.out.println("\n--- Update Data ---");
            Map<String, Object> updatePayload = new HashMap<>();
            updatePayload.put("name", "Updated User 0");
            updatePayload.put("updated_at", Instant.now().toString());
            client.updateData(tableName, "user_0", updatePayload);

            // 6. 删除数据
            System.out.println("\n--- Delete Data ---");
            client.deleteData(tableName, "user_4");

            // 7. 流式写入
            System.out.println("\n--- Stream Write ---");
            List<Map<String, Object>> streamRecords = new ArrayList<>();
            for (int i = 10; i < 20; i++) {
                Map<String, Object> record = new HashMap<>();
                record.put("id", "stream_user_" + i);
                record.put("timestamp", Instant.now().toString());
                Map<String, Object> payload = new HashMap<>();
                payload.put("name", "Stream User " + i);
                payload.put("batch", true);
                payload.put("created_at", Instant.now().toString());
                record.put("payload", payload);
                streamRecords.add(record);
            }
            client.streamWrite(tableName, streamRecords);

            // 8. 列出表
            System.out.println("\n--- List Tables ---");
            List<TableInfo> tables = client.listTables("*");
            for (TableInfo t : tables) {
                System.out.println("  - Table: " + t.name + ", Status: " + t.status);
                System.out.println("    Records: " + t.recordCount + ", Files: " + t.fileCount);
            }

            // 9. 获取表信息
            System.out.println("\n--- Get Table Info ---");
            TableInfo tableInfo = client.getTable(tableName);
            System.out.println("  - Record count: " + tableInfo.recordCount);
            System.out.println("  - File count: " + tableInfo.fileCount);
            System.out.println("  - Size: " + tableInfo.sizeBytes + " bytes");

            // 10. 获取状态
            System.out.println("\n--- Get Status ---");
            SystemStatus status = client.getStatus();
            System.out.println("  - Total nodes: " + status.totalNodes);
            System.out.println("  - Buffer stats: " + status.bufferStats);
            System.out.println("  - Redis stats: " + status.redisStats);

            // 11. 列出备份
            System.out.println("\n--- List Backups ---");
            List<BackupInfo> backups = client.listBackups(30);
            System.out.println("  Found " + backups.size() + " backups in the last 30 days");
            for (BackupInfo b : backups) {
                System.out.println("  - Backup: " + b.objectName + ", Size: " + b.size + ", Node: " + b.nodeId);
            }

            System.out.println("\n========================================");
            System.out.println("Example completed successfully!");
            System.out.println("========================================");

        } finally {
            client.shutdown();
        }
    }
}
