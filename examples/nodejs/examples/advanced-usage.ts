/**
 * MinIODB Node.js TypeScript SDK 高级使用示例
 * 
 * 演示 MinIODB Node.js SDK 的高级功能，包括：
 * - 异步批量操作
 * - 并发控制和流式处理
 * - 错误处理和重试机制
 * - 性能监控和指标收集
 * - 数据分析和备份维护
 */

import { EventEmitter } from 'events';
import { 
    MinIODBClient, 
    MinIODBConfig,
    AuthConfig,
    ConnectionConfig,
    LoggingConfig,
    LogLevel,
    LogFormat,
    DataRecord,
    TableConfig,
    DataRecordFactory,
    TableConfigFactory,
    MinIODBError,
    MinIODBConnectionError,
    MinIODBAuthenticationError,
    MinIODBRequestError,
    MinIODBServerError,
    MinIODBTimeoutError,
    ErrorUtils
} from '../src';

// 常量配置
const THREAD_POOL_SIZE = 10;
const BATCH_SIZE = 100;
const TOTAL_RECORDS = 1000;

/**
 * 高级配置示例
 */
async function advancedConfigurationExample(): Promise<void> {
    console.log('\n1. 高级配置示例');
    console.log('-'.repeat(30));

    // 创建高级配置
    const config: MinIODBConfig = {
        host: 'miniodb-cluster',
        grpcPort: 8080,
        restPort: 8081,
        auth: {
            apiKey: 'demo-api-key',
            secret: 'demo-secret',
            tokenType: 'Bearer'
        },
        connection: {
            maxConnections: 20,
            timeout: 60000,
            retryAttempts: 5,
            keepAliveTime: 600000,
            keepAliveTimeout: 10000,
            maxReceiveMessageLength: 8 * 1024 * 1024, // 8MB
            maxSendMessageLength: 8 * 1024 * 1024     // 8MB
        },
        logging: {
            level: LogLevel.INFO,
            format: LogFormat.JSON,
            enableRequestLogging: true,
            enablePerformanceLogging: true
        }
    };

    console.log('高级配置:', JSON.stringify(config, null, 2));

    const client = new MinIODBClient(config);
    console.log('gRPC 地址:', client.getGrpcAddress());
    console.log('连接状态:', client.isClientConnected());
    
    await client.close();
}

/**
 * 并发操作示例
 */
async function concurrentOperationsExample(): Promise<void> {
    console.log('\n2. 并发操作示例');
    console.log('-'.repeat(30));

    const config: MinIODBConfig = {
        host: 'localhost',
        grpcPort: 8080,
        connection: {
            maxConnections: THREAD_POOL_SIZE
        }
    };

    const client = new MinIODBClient(config);

    try {
        // 准备测试数据
        const records: DataRecord[] = [];
        for (let i = 0; i < TOTAL_RECORDS; i++) {
            const record = DataRecordFactory.create({
                user_id: `user_${i.toString().padStart(4, '0')}`,
                name: `User ${i}`,
                email: `user${i}@example.com`,
                age: 20 + (i % 50),
                department: ['Engineering', 'Sales', 'Marketing', 'HR'][i % 4],
                salary: 50000 + (i * 100),
                join_date: new Date(Date.now() - i * 86400000).toISOString(),
                active: i % 10 !== 0 // 90% 活跃用户
            });
            records.push(record);
        }

        console.log(`准备了 ${records.length} 条测试记录`);

        // 并发写入控制器
        const concurrencyController = new ConcurrencyController(THREAD_POOL_SIZE);

        console.log('并发写入示例:');
        const startTime = Date.now();

        // 创建并发写入任务
        const writePromises = records.map(record => 
            concurrencyController.execute(async () => {
                return await client.writeData('users', record);
            })
        );

        // 等待所有写入完成
        const results = await Promise.allSettled(writePromises);
        const elapsed = Date.now() - startTime;

        // 统计结果
        const successCount = results.filter(r => 
            r.status === 'fulfilled' && r.value.success
        ).length;
        const errorCount = results.length - successCount;

        console.log(`并发写入完成: ${successCount} 条记录，耗时 ${elapsed} ms`);
        console.log(`写入速度: ${Math.round(successCount / (elapsed / 1000))} 记录/秒`);
        console.log(`错误数: ${errorCount}`);

        // 批量操作示例
        console.log('\n批量操作示例:');
        const totalBatches = Math.ceil(records.length / BATCH_SIZE);
        console.log(`将 ${records.length} 条记录分为 ${totalBatches} 个批次，每批 ${BATCH_SIZE} 条`);

        for (let i = 0; i < totalBatches; i++) {
            const start = i * BATCH_SIZE;
            const end = Math.min(start + BATCH_SIZE, records.length);
            const batch = records.slice(start, end);

            console.log(`批次 ${i + 1}: ${batch.length} 条记录 (索引 ${start}-${end - 1})`);

            // 实际实现中这里会调用 client.streamWrite
            // const response = await client.streamWrite('users', batch);
        }

    } finally {
        await client.close();
    }
}

/**
 * 错误处理和重试示例
 */
async function errorHandlingExample(): Promise<void> {
    console.log('\n3. 错误处理和重试示例');
    console.log('-'.repeat(30));

    const config: MinIODBConfig = {
        host: 'localhost',
        grpcPort: 8080
    };

    const client = new MinIODBClient(config);

    try {
        // 重试机制示例
        console.log('重试机制示例:');

        const errorSimulations = [
            {
                name: '模拟连接错误',
                operation: async (): Promise<string> => {
                    if (Math.random() < 0.7) {
                        throw new MinIODBConnectionError('模拟连接失败');
                    }
                    return '连接成功';
                }
            },
            {
                name: '模拟认证错误',
                operation: async (): Promise<string> => {
                    throw new MinIODBAuthenticationError('模拟认证失败');
                }
            },
            {
                name: '模拟服务器错误',
                operation: async (): Promise<string> => {
                    if (Math.random() < 0.5) {
                        throw new MinIODBServerError('模拟服务器错误');
                    }
                    return '服务器响应正常';
                }
            }
        ];

        for (const simulation of errorSimulations) {
            console.log(`\n执行操作: ${simulation.name}`);
            try {
                const result = await executeWithRetry(simulation.operation, 3);
                console.log('操作成功:', result);
            } catch (error) {
                console.log('操作最终失败:', error);
            }
        }

        // 演示实际错误处理流程
        console.log('\n实际错误处理示例:');
        await demonstrateErrorHandling(client);

    } finally {
        await client.close();
    }
}

/**
 * 性能监控示例
 */
async function performanceMonitoringExample(): Promise<void> {
    console.log('\n4. 性能监控示例');
    console.log('-'.repeat(30));

    const config: MinIODBConfig = {
        host: 'localhost',
        grpcPort: 8080
    };

    const client = new MinIODBClient(config);

    try {
        // 性能指标收集器
        const metrics = new PerformanceMetrics();

        // 模拟操作监控
        const operations = [
            { name: '写入操作', duration: 50, success: true },
            { name: '查询操作', duration: 30, success: true },
            { name: '更新操作', duration: 45, success: false },
            { name: '删除操作', duration: 25, success: true },
            { name: '批量操作', duration: 200, success: true }
        ];

        console.log('执行模拟操作:');
        for (const operation of operations) {
            await simulateOperation(metrics, operation.name, operation.duration, operation.success);
        }

        // 计算和显示统计信息
        console.log('\n性能统计:');
        console.log(`  总操作数: ${metrics.getOperationCount()}`);
        console.log(`  成功操作: ${metrics.getSuccessCount()}`);
        console.log(`  失败操作: ${metrics.getErrorCount()}`);
        console.log(`  成功率: ${(metrics.getSuccessRate() * 100).toFixed(2)}%`);
        console.log(`  总耗时: ${metrics.getTotalDuration()} ms`);
        console.log(`  平均耗时: ${metrics.getAverageDuration().toFixed(2)} ms`);
        console.log(`  最小耗时: ${metrics.getMinDuration()} ms`);
        console.log(`  最大耗时: ${metrics.getMaxDuration()} ms`);

        // 监控系统状态
        console.log('\n系统状态监控:');
        try {
            const statusResponse = await client.getStatus();
            console.log('系统状态:');
            console.log(`  总节点数: ${statusResponse.totalNodes}`);
            console.log(`  缓冲区统计:`, statusResponse.bufferStats);
            console.log(`  Redis 统计:`, statusResponse.redisStats);
            console.log(`  MinIO 统计:`, statusResponse.minioStats);
        } catch (error) {
            console.log('获取系统状态失败:', error);
        }

        // 监控性能指标
        console.log('\n性能指标监控:');
        try {
            const metricsResponse = await client.getMetrics();
            console.log('性能指标:');
            console.log(`  性能指标:`, metricsResponse.performanceMetrics);
            console.log(`  资源使用:`, metricsResponse.resourceUsage);
            console.log(`  系统信息:`, metricsResponse.systemInfo);
        } catch (error) {
            console.log('获取性能指标失败:', error);
        }

    } finally {
        await client.close();
    }
}

/**
 * 数据分析示例
 */
async function dataAnalysisExample(): Promise<void> {
    console.log('\n5. 数据分析示例');
    console.log('-'.repeat(30));

    const config: MinIODBConfig = {
        host: 'localhost',
        grpcPort: 8080
    };

    const client = new MinIODBClient(config);

    try {
        // 复杂的分析查询
        const analysisQueries = [
            {
                name: '部门统计分析',
                query: `
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
                `
            },
            {
                name: '年龄分布分析',
                query: `
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
                `
            },
            {
                name: '薪资趋势分析',
                query: `
                    SELECT 
                        DATE_FORMAT(join_date, '%Y-%m') as join_month,
                        COUNT(*) as new_hires,
                        AVG(salary) as avg_starting_salary
                    FROM users
                    WHERE join_date >= DATE_SUB(NOW(), INTERVAL 12 MONTH)
                    GROUP BY join_month
                    ORDER BY join_month
                `
            }
        ];

        for (const analysis of analysisQueries) {
            console.log(`\n执行分析: ${analysis.name}`);
            console.log(`查询语句:${analysis.query}`);

            try {
                const startTime = Date.now();
                const response = await client.queryData(analysis.query, 100);
                const elapsed = Date.now() - startTime;

                console.log(`  查询耗时: ${elapsed} ms`);

                // 解析和显示结果
                if (response.resultJson) {
                    try {
                        const results = JSON.parse(response.resultJson);
                        console.log(`  结果数量: ${results.length} 行`);

                        // 显示前几行结果
                        for (let i = 0; i < Math.min(5, results.length); i++) {
                            console.log(`    ${i + 1}: ${JSON.stringify(results[i])}`);
                        }

                        if (results.length > 5) {
                            console.log(`    ... 还有 ${results.length - 5} 行`);
                        }
                    } catch (parseError) {
                        console.log('  结果解析失败:', parseError);
                    }
                }
            } catch (error) {
                console.log(`  分析查询失败: ${error}`);
            }
        }

        // 流式数据分析
        console.log('\n流式数据分析示例:');
        try {
            let totalRecords = 0;
            let batchCount = 0;

            for await (const batch of client.streamQuery(
                'SELECT * FROM users ORDER BY timestamp',
                1000
            )) {
                batchCount++;
                totalRecords += batch.records.length;
                console.log(`批次 ${batchCount}: 处理 ${batch.records.length} 条记录`);

                // 这里可以进行实时数据分析
                // await analyzeDataBatch(batch.records);

                if (!batch.hasMore) {
                    break;
                }
            }

            console.log(`流式分析完成，总共处理 ${totalRecords} 条记录`);
        } catch (error) {
            console.log('流式分析失败:', error);
        }

    } finally {
        await client.close();
    }
}

/**
 * 备份和维护示例
 */
async function backupMaintenanceExample(): Promise<void> {
    console.log('\n6. 备份和维护示例');
    console.log('-'.repeat(30));

    const config: MinIODBConfig = {
        host: 'localhost',
        grpcPort: 8080
    };

    const client = new MinIODBClient(config);

    try {
        // 手动触发备份
        console.log('备份操作示例:');
        try {
            console.log('触发元数据备份...');
            const backupResponse = await client.backupMetadata(true);

            if (backupResponse.success) {
                console.log('备份成功:');
                console.log(`  备份ID: ${backupResponse.backupId}`);
                console.log(`  备份时间: ${backupResponse.timestamp}`);
            } else {
                console.log(`备份失败: ${backupResponse.message}`);
            }
        } catch (error) {
            console.log('备份操作失败:', error);
        }

        // 列出最近的备份
        try {
            console.log('\n查询最近7天的备份...');
            const backupsResponse = await client.listBackups(7);

            console.log(`找到 ${backupsResponse.total} 个备份:`);
            for (const backup of backupsResponse.backups) {
                console.log(`  - ${backup.objectName}`);
                console.log(`    节点: ${backup.nodeId}`);
                console.log(`    时间: ${backup.timestamp}`);
                console.log(`    大小: ${backup.size} 字节`);
            }
        } catch (error) {
            console.log('列出备份失败:', error);
        }

        // 获取元数据状态
        try {
            console.log('\n获取元数据状态...');
            const statusResponse = await client.getMetadataStatus();

            console.log('元数据状态:');
            console.log(`  节点ID: ${statusResponse.nodeId}`);
            console.log(`  健康状态: ${statusResponse.healthStatus}`);
            console.log(`  上次备份: ${statusResponse.lastBackup}`);
            console.log(`  下次备份: ${statusResponse.nextBackup}`);
            console.log(`  备份状态:`, statusResponse.backupStatus);
        } catch (error) {
            console.log('获取元数据状态失败:', error);
        }

        // 维护任务示例
        console.log('\n维护任务示例:');
        const maintenanceTasks = [
            { name: '清理过期数据', description: '删除超过保留期的数据', duration: 200 },
            { name: '优化索引', description: '重建和优化数据库索引', duration: 500 },
            { name: '压缩数据文件', description: '压缩存储文件以节省空间', duration: 300 },
            { name: '健康检查', description: '检查系统组件健康状态', duration: 100 }
        ];

        for (const task of maintenanceTasks) {
            console.log(`\n执行维护任务: ${task.name}`);
            console.log(`描述: ${task.description}`);

            const startTime = Date.now();
            await new Promise(resolve => setTimeout(resolve, task.duration)); // 模拟任务执行
            const elapsed = Date.now() - startTime;

            console.log(`任务完成，耗时: ${elapsed} ms`);
        }

    } finally {
        await client.close();
    }
}

// 辅助类和工具函数

/**
 * 并发控制器
 */
class ConcurrencyController extends EventEmitter {
    private readonly maxConcurrency: number;
    private currentConcurrency = 0;
    private queue: Array<() => Promise<void>> = [];

    constructor(maxConcurrency: number) {
        super();
        this.maxConcurrency = maxConcurrency;
    }

    async execute<T>(task: () => Promise<T>): Promise<T> {
        return new Promise((resolve, reject) => {
            const wrappedTask = async (): Promise<void> => {
                try {
                    const result = await task();
                    resolve(result);
                } catch (error) {
                    reject(error);
                } finally {
                    this.currentConcurrency--;
                    this.processQueue();
                }
            };

            if (this.currentConcurrency < this.maxConcurrency) {
                this.currentConcurrency++;
                wrappedTask();
            } else {
                this.queue.push(wrappedTask);
            }
        });
    }

    private processQueue(): void {
        if (this.queue.length > 0 && this.currentConcurrency < this.maxConcurrency) {
            const task = this.queue.shift()!;
            this.currentConcurrency++;
            task();
        }
    }
}

/**
 * 性能指标收集器
 */
class PerformanceMetrics {
    private operationCount = 0;
    private successCount = 0;
    private errorCount = 0;
    private totalDuration = 0;
    private minDuration = Number.MAX_VALUE;
    private maxDuration = 0;

    recordOperation(duration: number, success: boolean): void {
        this.operationCount++;
        this.totalDuration += duration;

        if (duration < this.minDuration) {
            this.minDuration = duration;
        }
        if (duration > this.maxDuration) {
            this.maxDuration = duration;
        }

        if (success) {
            this.successCount++;
        } else {
            this.errorCount++;
        }
    }

    getOperationCount(): number { return this.operationCount; }
    getSuccessCount(): number { return this.successCount; }
    getErrorCount(): number { return this.errorCount; }
    getSuccessRate(): number { return this.successCount / this.operationCount; }
    getTotalDuration(): number { return this.totalDuration; }
    getAverageDuration(): number { return this.totalDuration / this.operationCount; }
    getMinDuration(): number { return this.minDuration; }
    getMaxDuration(): number { return this.maxDuration; }
}

/**
 * 带重试的操作执行
 */
async function executeWithRetry<T>(
    operation: () => Promise<T>,
    maxRetries: number = 3,
    baseDelay: number = 1000
): Promise<T> {
    let lastError: Error;

    for (let attempt = 0; attempt <= maxRetries; attempt++) {
        try {
            return await operation();
        } catch (error) {
            lastError = error as Error;

            // 检查错误类型决定是否重试
            if (error instanceof MinIODBAuthenticationError || 
                error instanceof MinIODBRequestError) {
                console.log(`不可重试的错误: ${error.message}`);
                throw error;
            }

            if (attempt < maxRetries) {
                const delay = baseDelay * Math.pow(2, attempt); // 指数退避
                console.log(`尝试 ${attempt + 1} 失败: ${error}, ${delay} ms 后重试`);
                await new Promise(resolve => setTimeout(resolve, delay));
            }
        }
    }

    console.log(`所有重试都失败，最后错误: ${lastError!.message}`);
    throw lastError!;
}

/**
 * 模拟操作执行
 */
async function simulateOperation(
    metrics: PerformanceMetrics,
    name: string,
    duration: number,
    success: boolean
): Promise<void> {
    const startTime = Date.now();

    // 模拟操作
    await new Promise(resolve => setTimeout(resolve, duration));

    const elapsed = Date.now() - startTime;

    // 更新指标
    metrics.recordOperation(elapsed, success);

    console.log(`${name}: 耗时 ${elapsed} ms, 成功: ${success}`);
}

/**
 * 演示错误处理
 */
async function demonstrateErrorHandling(client: MinIODBClient): Promise<void> {
    const record = DataRecordFactory.create({
        test: 'error_handling',
        timestamp: new Date().toISOString()
    });

    try {
        const response = await client.writeData('test_table', record);
        if (!response.success) {
            console.log(`写入失败: ${response.message}`);
        }
    } catch (error) {
        if (ErrorUtils.isConnectionError(error)) {
            console.error('连接错误:', error);
        } else if (ErrorUtils.isAuthenticationError(error)) {
            console.error('认证失败:', error);
        } else if (ErrorUtils.isRequestError(error)) {
            console.error('请求错误:', error);
        } else if (ErrorUtils.isServerError(error)) {
            console.error('服务器错误:', error);
        } else if (ErrorUtils.isTimeoutError(error)) {
            console.error('请求超时:', error);
        } else {
            console.error('未知错误:', error);
        }
    }
}

// 主函数
async function main(): Promise<void> {
    try {
        console.log('MinIODB Node.js TypeScript SDK 高级使用示例');
        console.log('='.repeat(50));

        await advancedConfigurationExample();
        await concurrentOperationsExample();
        await errorHandlingExample();
        await performanceMonitoringExample();
        await dataAnalysisExample();
        await backupMaintenanceExample();

        console.log('\n' + '='.repeat(50));
        console.log('高级示例完成');
        console.log('注意: 完整功能需要实现 MinIODBClient 类');
        console.log('请运行 npm run generate:proto 生成 gRPC 代码');
    } catch (error) {
        console.error('示例执行失败:', error);
        process.exit(1);
    }
}

// 运行示例
if (require.main === module) {
    main().catch(console.error);
}

export { 
    main as advancedUsageExample,
    ConcurrencyController,
    PerformanceMetrics,
    executeWithRetry,
    simulateOperation
};
