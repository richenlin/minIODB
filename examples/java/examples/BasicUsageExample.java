package com.miniodb.examples;

import com.miniodb.client.config.MinIODBConfig;
import com.miniodb.client.model.DataRecord;
import com.miniodb.client.model.TableConfig;

import java.time.Instant;
import java.util.Map;

/**
 * MinIODB Java SDK 基本使用示例
 * 
 * 演示如何使用 MinIODB Java SDK 进行基本的数据操作。
 * 
 * @author MinIODB Team
 * @version 1.0.0
 */
public class BasicUsageExample {
    
    public static void main(String[] args) {
        // 创建配置
        MinIODBConfig config = MinIODBConfig.builder()
                .host("localhost")
                .grpcPort(8080)
                .build();
        
        System.out.println("MinIODB Java SDK 基本使用示例");
        System.out.println("================================");
        
        // 注意：这里是示例代码，实际的客户端类还需要实现
        // try (MinIODBClient client = new MinIODBClient(config)) {
        //     
        //     // 1. 创建表
        //     System.out.println("1. 创建表...");
        //     TableConfig tableConfig = TableConfig.builder()
        //             .bufferSize(1000)
        //             .flushIntervalSeconds(30)
        //             .retentionDays(365)
        //             .backupEnabled(true)
        //             .build();
        //     
        //     CreateTableResponse createResponse = client.createTable("users", tableConfig, true);
        //     System.out.println("表创建结果: " + createResponse.isSuccess());
        //     
        //     // 2. 写入数据
        //     System.out.println("\\n2. 写入数据...");
        //     DataRecord record = DataRecord.builder()
        //             .id("user-123")
        //             .timestamp(Instant.now())
        //             .payload(Map.of(
        //                     "name", "John Doe",
        //                     "age", 30,
        //                     "email", "john@example.com",
        //                     "department", "Engineering"
        //             ))
        //             .build();
        //     
        //     WriteDataResponse writeResponse = client.writeData("users", record);
        //     System.out.println("写入结果: " + writeResponse.isSuccess());
        //     System.out.println("处理节点: " + writeResponse.getNodeId());
        //     
        //     // 3. 查询数据
        //     System.out.println("\\n3. 查询数据...");
        //     QueryDataResponse queryResponse = client.queryData(
        //             "SELECT * FROM users WHERE age > 25", 
        //             10, 
        //             null
        //     );
        //     
        //     System.out.println("查询结果: " + queryResponse.getResultJson());
        //     System.out.println("是否有更多数据: " + queryResponse.isHasMore());
        //     
        //     // 4. 更新数据
        //     System.out.println("\\n4. 更新数据...");
        //     UpdateDataResponse updateResponse = client.updateData(
        //             "users", 
        //             "user-123", 
        //             Map.of("age", 31, "status", "active"),
        //             Instant.now()
        //     );
        //     System.out.println("更新结果: " + updateResponse.isSuccess());
        //     
        //     // 5. 列出表
        //     System.out.println("\\n5. 列出表...");
        //     ListTablesResponse listResponse = client.listTables(null);
        //     System.out.println("表总数: " + listResponse.getTotal());
        //     listResponse.getTablesList().forEach(table -> {
        //         System.out.println("- 表名: " + table.getName() + 
        //                           ", 记录数: " + table.getStats().getRecordCount());
        //     });
        //     
        //     // 6. 健康检查
        //     System.out.println("\\n6. 健康检查...");
        //     HealthCheckResponse healthResponse = client.healthCheck();
        //     System.out.println("服务状态: " + healthResponse.getStatus());
        //     System.out.println("服务版本: " + healthResponse.getVersion());
        //     
        // } catch (MinIODBConnectionException e) {
        //     System.err.println("连接错误: " + e.getMessage());
        // } catch (MinIODBAuthenticationException e) {
        //     System.err.println("认证失败: " + e.getMessage());
        // } catch (MinIODBException e) {
        //     System.err.println("操作失败: " + e.getMessage());
        // }
        
        // 当前仅展示配置和模型的创建
        System.out.println("配置信息: " + config);
        
        DataRecord sampleRecord = DataRecord.builder()
                .id("sample-123")
                .timestamp(Instant.now())
                .payload(Map.of(
                        "name", "Sample User",
                        "age", 25,
                        "email", "sample@example.com"
                ))
                .build();
        
        System.out.println("示例记录: " + sampleRecord);
        
        TableConfig sampleConfig = TableConfig.builder()
                .bufferSize(2000)
                .flushIntervalSeconds(60)
                .retentionDays(730)
                .backupEnabled(true)
                .build();
        
        System.out.println("示例表配置: " + sampleConfig);
        
        System.out.println("\\n注意: 完整的客户端功能需要先生成 gRPC 代码并实现客户端类。");
        System.out.println("请运行: ./scripts/generate_java.sh 来生成必要的 gRPC 代码。");
    }
}
