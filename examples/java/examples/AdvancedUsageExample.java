package com.miniodb.examples;

import com.miniodb.client.config.MinIODBConfig;
import com.miniodb.client.config.AuthConfig;
import com.miniodb.client.config.ConnectionConfig;
import com.miniodb.client.config.LoggingConfig;
import com.miniodb.client.model.DataRecord;
import com.miniodb.client.model.TableConfig;
import com.miniodb.client.exception.*;

import java.time.Duration;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.*;
import java.util.stream.Collectors;
import java.util.stream.IntStream;

/**
 * MinIODB Java SDK 高级使用示例
 * 
 * 演示 MinIODB Java SDK 的高级功能，包括：
 * - 批量并发操作
 * - 连接池管理
 * - 错误处理和重试
 * - 性能监控
 * - 流式处理
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class AdvancedUsageExample {
    
    private static final int THREAD_POOL_SIZE = 10;
    private static final int BATCH_SIZE = 100;
    private static final int TOTAL_RECORDS = 1000;
    
    public static void main(String[] args) {
        System.out.println("MinIODB Java SDK 高级使用示例");
        System.out.println("=".repeat(50));
        
        try {
            advancedConfigurationExample();
            concurrentOperationsExample();
            errorHandlingExample();
            performanceMonitoringExample();
            dataAnalysisExample();
            backupMaintenanceExample();
            
            System.out.println("\n注意: 完整的客户端功能需要先生成 gRPC 代码并实现客户端类。");
            System.out.println("请运行: ./scripts/generate_java.sh 来生成必要的 gRPC 代码。");
            
        } catch (Exception e) {
            System.err.println("示例执行失败: " + e.getMessage());
            e.printStackTrace();
        }
    }
    
    /**
     * 高级配置示例
     */
    private static void advancedConfigurationExample() {
        System.out.println("\n1. 高级配置示例");
        System.out.println("-".repeat(30));
        
        // 创建高级配置
        MinIODBConfig config = MinIODBConfig.builder()
            .host("miniodb-cluster")
            .grpcPort(8080)
            .restPort(8081)
            .auth(AuthConfig.builder()
                .apiKey("demo-api-key")
                .secret("demo-secret")
                .tokenType("Bearer")
                .build())
            .connection(ConnectionConfig.builder()
                .maxConnections(20)
                .timeout(Duration.ofSeconds(60))
                .retryAttempts(5)
                .keepAliveTime(Duration.ofMinutes(10))
                .keepAliveTimeout(Duration.ofSeconds(10))
                .maxInboundMessageSize(8 * 1024 * 1024) // 8MB
                .maxOutboundMessageSize(8 * 1024 * 1024) // 8MB
                .build())
            .logging(LoggingConfig.builder()
                .level(LoggingConfig.Level.INFO)
                .format(LoggingConfig.Format.JSON)
                .enableRequestLogging(true)
                .enablePerformanceLogging(true)
                .build())
            .build();
        
        System.out.println("高级配置: " + config);
        System.out.println("gRPC 地址: " + config.getGrpcAddress());
        System.out.println("认证启用: " + (config.getAuth() != null && config.getAuth().isEnabled()));
    }
    
    /**
     * 并发操作示例
     */
    private static void concurrentOperationsExample() {
        System.out.println("\n2. 并发操作示例");
        System.out.println("-".repeat(30));
        
        // 创建配置
        MinIODBConfig config = MinIODBConfig.builder()
            .host("localhost")
            .grpcPort(8080)
            .connection(ConnectionConfig.builder()
                .maxConnections(THREAD_POOL_SIZE)
                .build())
            .build();
        
        // 准备测试数据
        List<DataRecord> records = IntStream.range(0, TOTAL_RECORDS)
            .mapToObj(i -> DataRecord.builder()
                .id("user_" + String.format("%04d", i))
                .timestamp(Instant.now())
                .payload(Map.of(
                    "user_id", "user_" + String.format("%04d", i),
                    "name", "User " + i,
                    "email", "user" + i + "@example.com",
                    "age", 20 + (i % 50),
                    "department", Arrays.asList("Engineering", "Sales", "Marketing", "HR").get(i % 4),
                    "salary", 50000 + (i * 100),
                    "join_date", Instant.now().minusSeconds(i * 86400).toString(),
                    "active", i % 10 != 0 // 90% 活跃用户
                ))
                .build())
            .collect(Collectors.toList());
        
        System.out.printf("准备了 %d 条测试记录\n", records.size());
        
        // 演示并发写入的结构
        System.out.println("并发写入示例结构:");
        System.out.println("""
            
            public class ConcurrentWriteExample {
                private final ExecutorService executorService;
                private final Semaphore semaphore;
                
                public CompletableFuture<List<WriteDataResponse>> concurrentWrite(
                        MinIODBClient client, 
                        String table, 
                        List<DataRecord> records) {
                    
                    List<CompletableFuture<WriteDataResponse>> futures = records.stream()
                        .map(record -> CompletableFuture.supplyAsync(() -> {
                            try {
                                semaphore.acquire(); // 限制并发数
                                return client.writeData(table, record);
                            } catch (Exception e) {
                                throw new RuntimeException(e);
                            } finally {
                                semaphore.release();
                            }
                        }, executorService))
                        .collect(Collectors.toList());
                    
                    return CompletableFuture.allOf(futures.toArray(new CompletableFuture[0]))
                        .thenApply(v -> futures.stream()
                            .map(CompletableFuture::join)
                            .collect(Collectors.toList()));
                }
            }
            """);
        
        // 演示批量操作
        System.out.println("批量操作示例:");
        int totalBatches = (records.size() + BATCH_SIZE - 1) / BATCH_SIZE;
        System.out.printf("将 %d 条记录分为 %d 个批次，每批 %d 条\n", 
                         records.size(), totalBatches, BATCH_SIZE);
        
        for (int i = 0; i < totalBatches; i++) {
            int start = i * BATCH_SIZE;
            int end = Math.min(start + BATCH_SIZE, records.size());
            List<DataRecord> batch = records.subList(start, end);
            
            System.out.printf("批次 %d: %d 条记录 (索引 %d-%d)\n", 
                             i + 1, batch.size(), start, end - 1);
            
            // 实际实现中这里会调用 client.streamWrite
            // StreamWriteResponse response = client.streamWrite("users", batch);
        }
    }
    
    /**
     * 错误处理和重试示例
     */
    private static void errorHandlingExample() {
        System.out.println("\n3. 错误处理和重试示例");
        System.out.println("-".repeat(30));
        
        // 重试机制示例
        System.out.println("重试机制示例:");
        
        // 演示不同类型的操作
        List<ErrorSimulation> errorSimulations = Arrays.asList(
            new ErrorSimulation("模拟连接错误", () -> {
                if (Math.random() < 0.7) {
                    throw new MinIODBConnectionException("模拟连接失败");
                }
                return "连接成功";
            }),
            new ErrorSimulation("模拟认证错误", () -> {
                throw new MinIODBAuthenticationException("模拟认证失败");
            }),
            new ErrorSimulation("模拟服务器错误", () -> {
                if (Math.random() < 0.5) {
                    throw new MinIODBServerException("模拟服务器错误");
                }
                return "服务器响应正常";
            })
        );
        
        for (ErrorSimulation simulation : errorSimulations) {
            System.out.printf("\n执行操作: %s\n", simulation.name);
            try {
                String result = executeWithRetry(simulation.operation, 3);
                System.out.println("操作成功: " + result);
            } catch (MinIODBException e) {
                System.out.println("操作最终失败: " + e.toString());
            }
        }
        
        // 演示实际错误处理流程
        System.out.println("\n实际错误处理示例:");
        System.out.println("""
            
            public class ErrorHandlingService {
                public void handleDataOperation(MinIODBClient client, DataRecord record) {
                    try {
                        WriteDataResponse response = client.writeData("users", record);
                        if (!response.isSuccess()) {
                            logger.warn("写入失败: {}", response.getMessage());
                        }
                    } catch (MinIODBConnectionException e) {
                        logger.error("连接错误: {}", e.getMessage());
                        // 可能需要重试或检查网络
                        scheduleRetry(record);
                    } catch (MinIODBAuthenticationException e) {
                        logger.error("认证失败: {}", e.getMessage());
                        // 可能需要重新获取令牌
                        refreshToken();
                    } catch (MinIODBTimeoutException e) {
                        logger.error("请求超时: {}", e.getMessage());
                        // 可能需要重试
                        scheduleRetry(record);
                    } catch (MinIODBException e) {
                        logger.error("操作失败: {}", e.toString());
                    }
                }
            }
            """);
    }
    
    /**
     * 性能监控示例
     */
    private static void performanceMonitoringExample() {
        System.out.println("\n4. 性能监控示例");
        System.out.println("-".repeat(30));
        
        // 性能指标收集
        PerformanceMetrics metrics = new PerformanceMetrics();
        
        // 模拟操作监控
        List<OperationSimulation> operations = Arrays.asList(
            new OperationSimulation("写入操作", Duration.ofMillis(50), true),
            new OperationSimulation("查询操作", Duration.ofMillis(30), true),
            new OperationSimulation("更新操作", Duration.ofMillis(45), false),
            new OperationSimulation("删除操作", Duration.ofMillis(25), true),
            new OperationSimulation("批量操作", Duration.ofMillis(200), true)
        );
        
        System.out.println("执行模拟操作:");
        for (OperationSimulation operation : operations) {
            simulateOperation(metrics, operation.name, operation.duration, operation.success);
        }
        
        // 计算和显示统计信息
        System.out.printf("\n性能统计:\n");
        System.out.printf("  总操作数: %d\n", metrics.getOperationCount());
        System.out.printf("  成功操作: %d\n", metrics.getSuccessCount());
        System.out.printf("  失败操作: %d\n", metrics.getErrorCount());
        System.out.printf("  成功率: %.2f%%\n", metrics.getSuccessRate() * 100);
        System.out.printf("  总耗时: %s\n", metrics.getTotalDuration());
        System.out.printf("  平均耗时: %s\n", metrics.getAverageDuration());
        System.out.printf("  最小耗时: %s\n", metrics.getMinDuration());
        System.out.printf("  最大耗时: %s\n", metrics.getMaxDuration());
    }
    
    /**
     * 数据分析示例
     */
    private static void dataAnalysisExample() {
        System.out.println("\n5. 数据分析示例");
        System.out.println("-".repeat(30));
        
        // 定义分析查询
        List<AnalysisQuery> analysisQueries = Arrays.asList(
            new AnalysisQuery(
                "部门统计分析",
                """
                SELECT 
                    department,
                    COUNT(*) as employee_count,
                    AVG(age) as avg_age,
                    AVG(salary) as avg_salary,
                    MIN(salary) as min_salary,
                    MAX(salary) as max_salary
                FROM users 
                WHERE active = true
                GROUP BY department
                ORDER BY avg_salary DESC
                """
            ),
            new AnalysisQuery(
                "年龄分布分析",
                """
                SELECT 
                    CASE 
                        WHEN age < 25 THEN '20-24'
                        WHEN age < 30 THEN '25-29'
                        WHEN age < 35 THEN '30-34'
                        WHEN age < 40 THEN '35-39'
                        ELSE '40+'
                    END as age_group,
                    COUNT(*) as count,
                    AVG(salary) as avg_salary
                FROM users
                WHERE active = true
                GROUP BY age_group
                ORDER BY age_group
                """
            ),
            new AnalysisQuery(
                "薪资趋势分析",
                """
                SELECT 
                    DATE_FORMAT(join_date, '%Y-%m') as join_month,
                    COUNT(*) as new_hires,
                    AVG(salary) as avg_starting_salary
                FROM users
                WHERE join_date >= DATE_SUB(NOW(), INTERVAL 12 MONTH)
                GROUP BY join_month
                ORDER BY join_month
                """
            )
        );
        
        for (AnalysisQuery analysis : analysisQueries) {
            System.out.printf("\n执行分析: %s\n", analysis.name);
            System.out.printf("查询语句:\n%s\n", analysis.query);
            
            // 模拟查询执行
            long startTime = System.currentTimeMillis();
            
            // 实际实现中这里会调用:
            // QueryDataResponse response = client.queryData(analysis.query, 100, null);
            
            // 模拟查询耗时
            try {
                Thread.sleep(50 + analysis.query.length() / 10);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
            
            long elapsed = System.currentTimeMillis() - startTime;
            System.out.printf("查询耗时: %d ms\n", elapsed);
            
            // 模拟结果处理
            System.out.printf("结果处理: 假设返回了多行数据\n");
        }
    }
    
    /**
     * 备份和维护示例
     */
    private static void backupMaintenanceExample() {
        System.out.println("\n6. 备份和维护示例");
        System.out.println("-".repeat(30));
        
        // 备份操作示例
        System.out.println("备份操作示例:");
        
        // 模拟备份操作
        List<BackupOperation> backupOperations = Arrays.asList(
            new BackupOperation("触发元数据备份", () -> {
                System.out.println("  正在备份元数据...");
                Thread.sleep(100);
                System.out.println("  备份ID: backup_20240115_103000");
                System.out.println("  备份时间: " + Instant.now());
            }),
            new BackupOperation("列出最近备份", () -> {
                System.out.println("  查询最近7天的备份...");
                Thread.sleep(50);
                
                List<String> backups = Arrays.asList(
                    "backup_20240115_103000.json (2024-01-15 10:30:00, 2.5MB)",
                    "backup_20240114_103000.json (2024-01-14 10:30:00, 2.4MB)",
                    "backup_20240113_103000.json (2024-01-13 10:30:00, 2.3MB)"
                );
                
                System.out.printf("  找到 %d 个备份:\n", backups.size());
                for (String backup : backups) {
                    System.out.println("    - " + backup);
                }
            }),
            new BackupOperation("获取元数据状态", () -> {
                System.out.println("  获取元数据状态...");
                Thread.sleep(30);
                
                System.out.println("  元数据状态:");
                System.out.println("    节点ID: miniodb-node-1");
                System.out.println("    健康状态: healthy");
                System.out.println("    上次备份: 2024-01-15 10:30:00");
                System.out.println("    下次备份: 2024-01-16 10:30:00");
            })
        );
        
        for (BackupOperation operation : backupOperations) {
            System.out.printf("\n执行: %s\n", operation.name);
            try {
                operation.operation.run();
                System.out.printf("操作成功\n");
            } catch (Exception e) {
                System.out.printf("操作失败: %s\n", e.getMessage());
            }
        }
        
        // 维护任务示例
        System.out.println("\n维护任务示例:");
        List<MaintenanceTask> maintenanceTasks = Arrays.asList(
            new MaintenanceTask("清理过期数据", "删除超过保留期的数据", Duration.ofMillis(200)),
            new MaintenanceTask("优化索引", "重建和优化数据库索引", Duration.ofMillis(500)),
            new MaintenanceTask("压缩数据文件", "压缩存储文件以节省空间", Duration.ofMillis(300)),
            new MaintenanceTask("健康检查", "检查系统组件健康状态", Duration.ofMillis(100))
        );
        
        for (MaintenanceTask task : maintenanceTasks) {
            System.out.printf("\n执行维护任务: %s\n", task.name);
            System.out.printf("描述: %s\n", task.description);
            
            long startTime = System.currentTimeMillis();
            try {
                Thread.sleep(task.duration.toMillis());
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
            long elapsed = System.currentTimeMillis() - startTime;
            
            System.out.printf("任务完成，耗时: %d ms\n", elapsed);
        }
    }
    
    // 辅助方法和类
    
    private static String executeWithRetry(Operation operation, int maxRetries) throws MinIODBException {
        MinIODBException lastException = null;
        
        for (int attempt = 0; attempt <= maxRetries; attempt++) {
            try {
                return operation.execute();
            } catch (MinIODBException e) {
                lastException = e;
                
                // 检查错误类型决定是否重试
                if (e instanceof MinIODBAuthenticationException || e instanceof MinIODBRequestException) {
                    System.out.printf("不可重试的错误: %s\n", e.getMessage());
                    throw e;
                }
                
                if (attempt < maxRetries) {
                    long backoffTime = (long) Math.pow(2, attempt) * 1000; // 指数退避
                    System.out.printf("尝试 %d 失败: %s，%d ms 后重试\n", 
                                     attempt + 1, e.getMessage(), backoffTime);
                    try {
                        Thread.sleep(backoffTime);
                    } catch (InterruptedException ie) {
                        Thread.currentThread().interrupt();
                        throw new MinIODBException("重试被中断", ie);
                    }
                }
            }
        }
        
        System.out.printf("所有重试都失败，最后错误: %s\n", lastException.getMessage());
        throw lastException;
    }
    
    private static void simulateOperation(PerformanceMetrics metrics, String name, Duration duration, boolean success) {
        long startTime = System.currentTimeMillis();
        
        // 模拟操作
        try {
            Thread.sleep(duration.toMillis());
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
        
        long elapsed = System.currentTimeMillis() - startTime;
        Duration actualDuration = Duration.ofMillis(elapsed);
        
        // 更新指标
        metrics.recordOperation(actualDuration, success);
        
        System.out.printf("%s: 耗时 %d ms, 成功: %s\n", name, elapsed, success);
    }
    
    // 内部类和接口
    
    @FunctionalInterface
    private interface Operation {
        String execute() throws MinIODBException;
    }
    
    @FunctionalInterface
    private interface RunnableOperation {
        void run() throws Exception;
    }
    
    private static class ErrorSimulation {
        final String name;
        final Operation operation;
        
        ErrorSimulation(String name, Operation operation) {
            this.name = name;
            this.operation = operation;
        }
    }
    
    private static class OperationSimulation {
        final String name;
        final Duration duration;
        final boolean success;
        
        OperationSimulation(String name, Duration duration, boolean success) {
            this.name = name;
            this.duration = duration;
            this.success = success;
        }
    }
    
    private static class AnalysisQuery {
        final String name;
        final String query;
        
        AnalysisQuery(String name, String query) {
            this.name = name;
            this.query = query;
        }
    }
    
    private static class BackupOperation {
        final String name;
        final RunnableOperation operation;
        
        BackupOperation(String name, RunnableOperation operation) {
            this.name = name;
            this.operation = operation;
        }
    }
    
    private static class MaintenanceTask {
        final String name;
        final String description;
        final Duration duration;
        
        MaintenanceTask(String name, String description, Duration duration) {
            this.name = name;
            this.description = description;
            this.duration = duration;
        }
    }
    
    private static class PerformanceMetrics {
        private long operationCount = 0;
        private long successCount = 0;
        private long errorCount = 0;
        private Duration totalDuration = Duration.ZERO;
        private Duration minDuration = Duration.ofHours(1); // 初始化为很大的值
        private Duration maxDuration = Duration.ZERO;
        
        public synchronized void recordOperation(Duration duration, boolean success) {
            operationCount++;
            totalDuration = totalDuration.plus(duration);
            
            if (duration.compareTo(minDuration) < 0) {
                minDuration = duration;
            }
            if (duration.compareTo(maxDuration) > 0) {
                maxDuration = duration;
            }
            
            if (success) {
                successCount++;
            } else {
                errorCount++;
            }
        }
        
        public long getOperationCount() { return operationCount; }
        public long getSuccessCount() { return successCount; }
        public long getErrorCount() { return errorCount; }
        public double getSuccessRate() { return (double) successCount / operationCount; }
        public Duration getTotalDuration() { return totalDuration; }
        public Duration getAverageDuration() { return totalDuration.dividedBy(operationCount); }
        public Duration getMinDuration() { return minDuration; }
        public Duration getMaxDuration() { return maxDuration; }
    }
}
