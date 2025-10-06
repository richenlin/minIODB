# MinIODB Node.js TypeScript SDK

MinIODB Node.js TypeScript SDK 是用于与 MinIODB 服务交互的官方 Node.js 客户端库。

## 特性

- 🚀 **高性能**: 基于 gRPC 的高性能通信
- 🔄 **异步优先**: 完全基于 Promise 和 async/await
- 🛡️ **错误处理**: 完善的错误处理和重试机制
- 📊 **流式操作**: 支持大数据量的流式读写
- 🔐 **认证支持**: 支持 API 密钥认证
- 📝 **TypeScript**: 完整的 TypeScript 类型定义
- 🎯 **现代化**: 支持 ES6+ 和现代 Node.js 特性

## 安装

### 使用 npm
```bash
npm install @miniodb/nodejs-sdk
```

### 使用 yarn
```bash
yarn add @miniodb/nodejs-sdk
```

### 使用 pnpm
```bash
pnpm add @miniodb/nodejs-sdk
```

## 快速开始

### 基本用法

```typescript
import { MinIODBClient, MinIODBConfig } from '@miniodb/nodejs-sdk';
import { DataRecord, TableConfig } from '@miniodb/nodejs-sdk/models';

async function main() {
    // 创建配置
    const config: MinIODBConfig = {
        host: 'localhost',
        grpcPort: 8080,
    };

    // 创建客户端
    const client = new MinIODBClient(config);

    try {
        // 写入数据
        const record: DataRecord = {
            id: 'user-123',
            timestamp: new Date(),
            payload: {
                name: 'John Doe',
                age: 30,
                email: 'john@example.com'
            }
        };

        const writeResponse = await client.writeData('users', record);
        console.log('写入成功:', writeResponse.success);

        // 查询数据
        const queryResponse = await client.queryData(
            'SELECT * FROM users WHERE age > 25',
            10
        );
        
        console.log('查询结果:', queryResponse.resultJson);

        // 创建表
        const tableConfig: TableConfig = {
            bufferSize: 1000,
            flushIntervalSeconds: 30,
            retentionDays: 365,
            backupEnabled: true
        };

        const createResponse = await client.createTable('products', tableConfig, true);
        console.log('表创建成功:', createResponse.success);

    } finally {
        // 关闭客户端
        await client.close();
    }
}

main().catch(console.error);
```

### 使用 ES6 模块

```typescript
import { MinIODBClient } from '@miniodb/nodejs-sdk';

// 使用 ES6 模块语法
const client = new MinIODBClient({
    host: 'localhost',
    grpcPort: 8080
});
```

### 使用 CommonJS

```javascript
const { MinIODBClient } = require('@miniodb/nodejs-sdk');

// 使用 CommonJS 语法
const client = new MinIODBClient({
    host: 'localhost',
    grpcPort: 8080
});
```

## 核心功能

### 数据操作

#### 写入数据
```typescript
const record: DataRecord = {
    id: 'record-id',
    timestamp: new Date(),
    payload: { key: 'value' }
};

const response = await client.writeData('table_name', record);
```

#### 批量写入
```typescript
const records: DataRecord[] = [record1, record2, record3];
const response = await client.streamWrite('table_name', records);
```

#### 查询数据
```typescript
// 基本查询
const response = await client.queryData('SELECT * FROM users', 100);

// 分页查询
let cursor: string | undefined;
do {
    const page = await client.queryData('SELECT * FROM users', 50, cursor);
    // 处理结果
    cursor = page.nextCursor;
} while (page.hasMore);
```

#### 流式查询
```typescript
const stream = client.streamQuery('SELECT * FROM large_table', 1000);

for await (const batch of stream) {
    // 处理批次数据
    for (const record of batch.records) {
        console.log(record);
    }
}
```

#### 更新数据
```typescript
const response = await client.updateData(
    'users', 
    'user-123', 
    { age: 31, status: 'active' },
    new Date()
);
```

#### 删除数据
```typescript
// 软删除
const response = await client.deleteData('users', 'user-123', true);

// 硬删除
const response = await client.deleteData('users', 'user-123', false);
```

### 表管理

#### 创建表
```typescript
const config: TableConfig = {
    bufferSize: 2000,
    flushIntervalSeconds: 60,
    retentionDays: 730,
    backupEnabled: true,
    properties: {
        description: '用户数据表'
    }
};

const response = await client.createTable('users', config, true);
```

#### 列出表
```typescript
const response = await client.listTables('user*');
for (const table of response.tables) {
    console.log(`表名: ${table.name}, 记录数: ${table.stats?.recordCount}`);
}
```

#### 获取表信息
```typescript
const response = await client.getTable('users');
const info = response.tableInfo;
console.log(`表状态: ${info.status}`);
console.log(`记录数: ${info.stats?.recordCount}`);
```

#### 删除表
```typescript
const response = await client.deleteTable('old_table', true, true);
```

### 元数据管理

#### 备份元数据
```typescript
const response = await client.backupMetadata(true);
console.log(`备份ID: ${response.backupId}`);
```

#### 恢复元数据
```typescript
const response = await client.restoreMetadata({
    backupFile: 'backup_20240115_103000.json',
    fromLatest: false,
    dryRun: false,
    overwrite: true,
    validate: true,
    parallel: true,
    filters: { tablePattern: 'users*' },
    keyPatterns: ['table:*', 'index:*']
});
```

#### 列出备份
```typescript
const response = await client.listBackups(7);
for (const backup of response.backups) {
    console.log(`备份: ${backup.objectName}, 时间: ${backup.timestamp}`);
}
```

### 监控和健康检查

#### 健康检查
```typescript
const response = await client.healthCheck();
console.log(`服务状态: ${response.status}`);
console.log(`版本: ${response.version}`);
```

#### 获取系统状态
```typescript
const response = await client.getStatus();
console.log(`总节点数: ${response.totalNodes}`);
console.log(`缓冲区统计:`, response.bufferStats);
```

#### 获取性能指标
```typescript
const response = await client.getMetrics();
console.log(`性能指标:`, response.performanceMetrics);
console.log(`资源使用:`, response.resourceUsage);
```

## 配置选项

### 基本配置
```typescript
const config: MinIODBConfig = {
    host: 'localhost',          // 服务器地址
    grpcPort: 8080,            // gRPC 端口
    restPort: 8081,            // REST 端口（可选）
};
```

### 认证配置
```typescript
const config: MinIODBConfig = {
    host: 'localhost',
    grpcPort: 8080,
    auth: {
        apiKey: 'your-api-key',
        secret: 'your-secret'
    }
};
```

### 连接配置
```typescript
const config: MinIODBConfig = {
    host: 'localhost',
    grpcPort: 8080,
    connection: {
        maxConnections: 10,
        timeout: 30000,           // 30 秒
        retryAttempts: 3,
        keepAliveTime: 300000,    // 5 分钟
    }
};
```

### 完整配置示例
```typescript
const config: MinIODBConfig = {
    host: 'miniodb-server',
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
        keepAliveTime: 600000,
        maxReceiveMessageLength: 4 * 1024 * 1024, // 4MB
        maxSendMessageLength: 4 * 1024 * 1024     // 4MB
    },
    logging: {
        level: 'info',
        format: 'json',
        enableRequestLogging: true,
        enablePerformanceLogging: true
    }
};
```

## 错误处理

SDK 提供了完善的错误处理机制：

```typescript
import { 
    MinIODBConnectionError,
    MinIODBAuthenticationError,
    MinIODBRequestError,
    MinIODBServerError,
    MinIODBTimeoutError
} from '@miniodb/nodejs-sdk/errors';

try {
    const response = await client.writeData('users', record);
    if (!response.success) {
        console.log(`写入失败: ${response.message}`);
    }
} catch (error) {
    if (error instanceof MinIODBConnectionError) {
        console.error('连接错误:', error.message);
    } else if (error instanceof MinIODBAuthenticationError) {
        console.error('认证失败:', error.message);
    } else if (error instanceof MinIODBRequestError) {
        console.error('请求错误:', error.message);
    } else if (error instanceof MinIODBServerError) {
        console.error('服务器错误:', error.message);
    } else if (error instanceof MinIODBTimeoutError) {
        console.error('请求超时:', error.message);
    } else {
        console.error('未知错误:', error);
    }
}
```

## 异步操作

### Promise 和 async/await
```typescript
// 使用 async/await
async function writeMultipleRecords() {
    const promises = records.map(record => 
        client.writeData('users', record)
    );
    
    const results = await Promise.all(promises);
    const successCount = results.filter(r => r.success).length;
    console.log(`成功写入 ${successCount}/${results.length} 条记录`);
}

// 使用 Promise
function writeRecord(record: DataRecord) {
    return client.writeData('users', record)
        .then(response => {
            console.log('写入成功:', response.success);
            return response;
        })
        .catch(error => {
            console.error('写入失败:', error);
            throw error;
        });
}
```

### 流式处理
```typescript
async function processLargeDataset() {
    const stream = client.streamQuery(
        'SELECT * FROM large_table ORDER BY timestamp', 
        1000
    );

    for await (const batch of stream) {
        // 异步处理每个批次
        await processBatch(batch.records);
    }
}
```

### 并发控制
```typescript
import { EventEmitter } from 'events';

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
            const wrappedTask = async () => {
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

    private processQueue() {
        if (this.queue.length > 0 && this.currentConcurrency < this.maxConcurrency) {
            const task = this.queue.shift()!;
            this.currentConcurrency++;
            task();
        }
    }
}

// 使用并发控制
const controller = new ConcurrencyController(10);

const tasks = records.map(record => 
    () => client.writeData('users', record)
);

const results = await Promise.all(
    tasks.map(task => controller.execute(task))
);
```

## 最佳实践

### 1. 连接管理
```typescript
// 推荐：重用客户端实例
class DataService {
    private client: MinIODBClient;

    constructor(config: MinIODBConfig) {
        this.client = new MinIODBClient(config);
    }

    async writeData(table: string, record: DataRecord) {
        return this.client.writeData(table, record);
    }

    async close() {
        await this.client.close();
    }
}

// 在应用退出时清理资源
process.on('SIGINT', async () => {
    await dataService.close();
    process.exit(0);
});
```

### 2. 批量操作
```typescript
// 推荐：批量写入大量数据
const batchSize = 1000;
const batches = [];

for (let i = 0; i < records.length; i += batchSize) {
    const batch = records.slice(i, i + batchSize);
    batches.push(client.streamWrite('table', batch));
}

const results = await Promise.all(batches);

// 避免：逐条写入大量数据
for (const record of records) {
    await client.writeData('table', record); // 不推荐
}
```

### 3. 错误处理和重试
```typescript
async function withRetry<T>(
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

            // 不重试的错误类型
            if (error instanceof MinIODBAuthenticationError || 
                error instanceof MinIODBRequestError) {
                throw error;
            }

            if (attempt < maxRetries) {
                const delay = baseDelay * Math.pow(2, attempt); // 指数退避
                await new Promise(resolve => setTimeout(resolve, delay));
            }
        }
    }

    throw lastError!;
}

// 使用重试机制
const response = await withRetry(() => 
    client.writeData('users', record)
);
```

### 4. 类型安全
```typescript
// 使用严格的类型定义
interface UserRecord {
    id: string;
    name: string;
    email: string;
    age: number;
    createdAt: Date;
}

function createUserRecord(user: UserRecord): DataRecord {
    return {
        id: user.id,
        timestamp: user.createdAt,
        payload: {
            name: user.name,
            email: user.email,
            age: user.age
        }
    };
}

// 使用泛型提高类型安全
class TypedMinIODBClient<T = any> {
    constructor(private client: MinIODBClient) {}

    async writeTypedData(table: string, id: string, data: T): Promise<WriteDataResponse> {
        const record: DataRecord = {
            id,
            timestamp: new Date(),
            payload: data as Record<string, any>
        };
        return this.client.writeData(table, record);
    }
}
```

## 开发和测试

### 构建项目
```bash
npm run build
```

### 运行测试
```bash
npm test
```

### 监视模式开发
```bash
npm run build:watch
npm run test:watch
```

### 代码检查
```bash
npm run lint
npm run format
```

### 生成 gRPC 代码
```bash
npm run generate:proto
```

## 许可证

本项目采用 BSD-3-Clause 许可证。详见 [LICENSE](../LICENSE) 文件。
