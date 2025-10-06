/**
 * MinIODB Node.js TypeScript SDK 基本使用示例
 * 
 * 演示如何使用 MinIODB Node.js SDK 进行基本的数据操作。
 */

import { 
    MinIODBClient, 
    MinIODBConfig,
    DataRecord,
    TableConfig,
    DataRecordFactory,
    TableConfigFactory,
    MinIODBError,
    MinIODBConnectionError,
    MinIODBAuthenticationError,
    MinIODBRequestError,
    MinIODBServerError,
    MinIODBTimeoutError
} from '../src';

async function basicUsageExample(): Promise<void> {
    console.log('MinIODB Node.js TypeScript SDK 基本使用示例');
    console.log('='.repeat(50));

    // 创建配置
    const config: MinIODBConfig = {
        host: 'localhost',
        grpcPort: 8080,
        // 可选的认证配置
        // auth: {
        //     apiKey: 'your-api-key',
        //     secret: 'your-secret'
        // }
    };

    // 创建客户端
    const client = new MinIODBClient(config);

    try {
        console.log('配置信息:', client.getConfig());
        console.log('gRPC 地址:', client.getGrpcAddress());
        console.log('连接状态:', client.isClientConnected());

        // 1. 创建表
        console.log('\n1. 创建表...');
        const tableConfig: TableConfig = {
            bufferSize: 1000,
            flushIntervalSeconds: 30,
            retentionDays: 365,
            backupEnabled: true,
            properties: {
                description: '用户数据表',
                owner: 'user-service'
            }
        };

        const createResponse = await client.createTable('users', tableConfig, true);
        console.log('表创建结果:', createResponse.success);
        console.log('响应消息:', createResponse.message);

        // 2. 写入数据
        console.log('\n2. 写入数据...');
        const record: DataRecord = DataRecordFactory.create({
            name: 'John Doe',
            age: 30,
            email: 'john@example.com',
            department: 'Engineering',
            salary: 75000,
            joinDate: new Date().toISOString(),
            active: true
        });

        const writeResponse = await client.writeData('users', record);
        console.log('写入结果:', writeResponse.success);
        console.log('处理节点:', writeResponse.nodeId);
        console.log('记录ID:', record.id);

        // 3. 批量写入
        console.log('\n3. 批量写入...');
        const records: DataRecord[] = [];
        for (let i = 0; i < 5; i++) {
            const batchRecord = DataRecordFactory.create({
                name: `User ${i}`,
                age: 25 + i,
                email: `user${i}@example.com`,
                department: i % 2 === 0 ? 'Sales' : 'Marketing',
                salary: 60000 + i * 5000,
                joinDate: new Date().toISOString(),
                active: true
            });
            records.push(batchRecord);
        }

        const streamResponse = await client.streamWrite('users', records);
        console.log('批量写入结果:', streamResponse.success);
        console.log('写入记录数:', streamResponse.recordsCount);
        console.log('错误数:', streamResponse.errors.length);

        // 4. 查询数据
        console.log('\n4. 查询数据...');
        const queryResponse = await client.queryData(
            'SELECT name, age, department FROM users WHERE age > 25 ORDER BY age',
            10
        );

        console.log('查询结果:', queryResponse.resultJson);
        console.log('是否有更多数据:', queryResponse.hasMore);

        // 5. 流式查询
        console.log('\n5. 流式查询...');
        let batchCount = 0;
        for await (const batch of client.streamQuery(
            'SELECT * FROM users ORDER BY timestamp',
            3
        )) {
            batchCount++;
            console.log(`批次 ${batchCount}: ${batch.records.length} 条记录`);
            
            // 处理批次数据
            for (const batchRecord of batch.records) {
                console.log(`  - ${batchRecord.id}: ${batchRecord.payload.name}`);
            }

            if (!batch.hasMore) {
                break;
            }
        }

        // 6. 更新数据
        console.log('\n6. 更新数据...');
        const updateResponse = await client.updateData(
            'users',
            record.id,
            {
                age: 31,
                status: 'active',
                lastUpdate: new Date().toISOString()
            },
            new Date()
        );
        console.log('更新结果:', updateResponse.success);

        // 7. 列出表
        console.log('\n7. 列出表...');
        const listResponse = await client.listTables();
        console.log('表总数:', listResponse.total);
        for (const table of listResponse.tables) {
            console.log(`- 表名: ${table.name}, 状态: ${table.status}`);
            if (table.stats) {
                console.log(`  记录数: ${table.stats.recordCount}, 大小: ${table.stats.sizeBytes} 字节`);
            }
        }

        // 8. 获取表信息
        console.log('\n8. 获取表信息...');
        const tableResponse = await client.getTable('users');
        const tableInfo = tableResponse.tableInfo;
        console.log('表名:', tableInfo.name);
        console.log('创建时间:', tableInfo.createdAt);
        console.log('配置: 缓冲区大小=', tableInfo.config.bufferSize);

        // 9. 健康检查
        console.log('\n9. 健康检查...');
        const healthResponse = await client.healthCheck();
        console.log('服务状态:', healthResponse.status);
        console.log('服务版本:', healthResponse.version);
        console.log('检查时间:', healthResponse.timestamp);

        // 10. 获取系统状态
        console.log('\n10. 获取系统状态...');
        const statusResponse = await client.getStatus();
        console.log('总节点数:', statusResponse.totalNodes);
        console.log('缓冲区统计:', statusResponse.bufferStats);

        // 11. 获取性能指标
        console.log('\n11. 获取性能指标...');
        const metricsResponse = await client.getMetrics();
        console.log('性能指标:', metricsResponse.performanceMetrics);
        console.log('资源使用:', metricsResponse.resourceUsage);

    } catch (error) {
        console.error('操作失败:', error);
        
        // 详细的错误处理
        if (error instanceof MinIODBConnectionError) {
            console.error('连接错误，可能需要检查网络连接');
        } else if (error instanceof MinIODBAuthenticationError) {
            console.error('认证错误，可能需要检查 API 密钥');
        } else if (error instanceof MinIODBRequestError) {
            console.error('请求错误，可能需要检查请求参数');
        } else if (error instanceof MinIODBServerError) {
            console.error('服务器错误，可能需要联系管理员');
        } else if (error instanceof MinIODBTimeoutError) {
            console.error('超时错误，可能需要增加超时时间或重试');
        }
    } finally {
        // 关闭客户端连接
        await client.close();
        console.log('\n客户端连接已关闭');
    }
}

async function configurationExample(): Promise<void> {
    console.log('\nMinIODB 配置示例');
    console.log('='.repeat(30));

    // 基本配置
    const basicConfig: MinIODBConfig = {
        host: 'localhost',
        grpcPort: 8080
    };

    // 带认证的配置
    const authConfig: MinIODBConfig = {
        host: 'miniodb-server',
        grpcPort: 8080,
        auth: {
            apiKey: 'your-api-key',
            secret: 'your-secret'
        }
    };

    // 完整配置
    const fullConfig: MinIODBConfig = {
        host: 'miniodb-cluster',
        grpcPort: 8080,
        restPort: 8081,
        auth: {
            apiKey: 'your-api-key',
            secret: 'your-secret'
        },
        connection: {
            maxConnections: 20,
            timeout: 60000,
            retryAttempts: 5,
            keepAliveTime: 600000
        },
        logging: {
            level: 'debug',
            format: 'json',
            enableRequestLogging: true,
            enablePerformanceLogging: true
        }
    };

    console.log('基本配置:', basicConfig);
    console.log('认证配置:', authConfig);
    console.log('完整配置:', fullConfig);
}

async function errorHandlingExample(): Promise<void> {
    console.log('\nMinIODB 错误处理示例');
    console.log('='.repeat(30));

    const config: MinIODBConfig = {
        host: 'localhost',
        grpcPort: 8080
    };

    const client = new MinIODBClient(config);

    // 演示不同类型的错误处理
    const errorExamples = [
        {
            name: '连接错误',
            error: new MinIODBConnectionError('无法连接到服务器')
        },
        {
            name: '认证错误',
            error: new MinIODBAuthenticationError('认证失败')
        },
        {
            name: '请求错误',
            error: new MinIODBRequestError('请求参数错误')
        },
        {
            name: '服务器错误',
            error: new MinIODBServerError('服务器内部错误')
        },
        {
            name: '超时错误',
            error: new MinIODBTimeoutError('请求超时')
        }
    ];

    for (const example of errorExamples) {
        console.log(`\n${example.name}:`);
        console.log(`  类型: ${example.error.constructor.name}`);
        console.log(`  错误码: ${example.error.code}`);
        console.log(`  状态码: ${example.error.statusCode}`);
        console.log(`  消息: ${example.error.message}`);
        console.log(`  JSON: ${JSON.stringify(example.error.toJSON(), null, 2)}`);
    }

    await client.close();
}

async function factoryExample(): Promise<void> {
    console.log('\nMinIODB 工厂类示例');
    console.log('='.repeat(30));

    // 使用 DataRecordFactory 创建记录
    const record1 = DataRecordFactory.create({
        name: 'Alice',
        age: 28,
        email: 'alice@example.com'
    });

    const record2 = DataRecordFactory.create(
        { name: 'Bob', age: 32 },
        'custom-id-123',
        new Date('2024-01-15')
    );

    console.log('记录1:', record1);
    console.log('记录2:', record2);

    // 使用 TableConfigFactory 创建表配置
    const defaultConfig = TableConfigFactory.createDefault();
    console.log('默认表配置:', defaultConfig);

    // 验证配置
    try {
        TableConfigFactory.validate(defaultConfig);
        console.log('表配置验证通过');
    } catch (error) {
        console.error('表配置验证失败:', error);
    }
}

// 主函数
async function main(): Promise<void> {
    try {
        await basicUsageExample();
        await configurationExample();
        await errorHandlingExample();
        await factoryExample();
        
        console.log('\n注意: 完整的客户端功能需要先生成 gRPC 代码并实现客户端类。');
        console.log('请运行: npm run generate:proto 来生成必要的 gRPC 代码。');
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
    basicUsageExample, 
    configurationExample, 
    errorHandlingExample, 
    factoryExample 
};
