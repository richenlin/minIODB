#!/usr/bin/env node

/**
 * MinIODB Node.js SDK 示例
 * 
 * 本示例展示如何通过 Redis Streams 或 Kafka 向 MinIODB 发布数据
 * 适用于第三方系统集成，实现业务实时数据同步
 * 
 * 依赖安装:
 *   npm install
 * 
 * 使用方法:
 *   node miniodb-client.js --mode redis    # 使用 Redis Streams
 *   node miniodb-client.js --mode kafka    # 使用 Kafka
 *   node miniodb-client.js --mode grpc     # 使用 gRPC 直连
 */

import { v4 as uuidv4 } from 'uuid';
import { Command } from 'commander';
import Redis from 'ioredis';
import { Kafka } from 'kafkajs';

// ===============================================
// 数据流格式定义（与 MinIODB 保持一致）
// ===============================================

/**
 * 创建数据记录
 * @param {string} id - 记录 ID
 * @param {number} timestamp - 毫秒时间戳
 * @param {Object} payload - 数据负载
 * @returns {Object} DataRecord
 */
function createDataRecord(id, timestamp, payload) {
    return {
        id,
        timestamp,
        payload,
        version: null,
        deleted_at: null
    };
}

/**
 * 创建事件元数据
 * @param {Object} options - 元数据选项
 * @returns {Object} EventMetadata
 */
function createEventMetadata(options = {}) {
    return {
        source: options.source || null,
        batch_id: options.batchId || null,
        partition_key: options.partitionKey || null,
        priority: options.priority || 'normal',
        trace_id: options.traceId || null,
        tags: options.tags || {},
        retry_count: options.retryCount || 0
    };
}

/**
 * 创建数据事件
 * @param {string} eventType - 事件类型: insert, update, delete, batch
 * @param {string} table - 目标表名
 * @param {Array} records - 数据记录列表
 * @param {Object} metadata - 元数据
 * @returns {Object} DataEvent
 */
function createDataEvent(eventType, table, records, metadata = {}) {
    return {
        event_id: `evt_${uuidv4().substring(0, 12)}`,
        event_type: eventType,
        table,
        timestamp: new Date().toISOString(),
        records,
        metadata: createEventMetadata(metadata)
    };
}

/**
 * 创建插入事件
 */
function createInsertEvent(table, record, source = null) {
    return createDataEvent('insert', table, [record], { source });
}

/**
 * 创建批量插入事件
 */
function createBatchEvent(table, records, source = null) {
    return createDataEvent('batch', table, records, { source });
}

// ===============================================
// Redis Streams 客户端
// ===============================================

class RedisStreamClient {
    /**
     * @param {Object} options - Redis 连接选项
     * @param {string} options.host - Redis 主机
     * @param {number} options.port - Redis 端口
     * @param {string} options.password - Redis 密码
     * @param {string} options.streamPrefix - Stream key 前缀
     */
    constructor(options = {}) {
        this.streamPrefix = options.streamPrefix || 'miniodb:stream:';
        
        this.client = new Redis({
            host: options.host || 'localhost',
            port: options.port || 6379,
            password: options.password || undefined,
            retryDelayOnFailover: 100,
            maxRetriesPerRequest: 3
        });
        
        this.client.on('error', (err) => {
            console.error('Redis connection error:', err.message);
        });
    }
    
    /**
     * 发布事件到 Redis Stream
     * @param {Object} event - DataEvent
     * @returns {Promise<string>} Stream Entry ID
     */
    async publish(event) {
        const streamKey = `${this.streamPrefix}${event.table}`;
        
        const fields = [
            'event_id', event.event_id,
            'event_type', event.event_type,
            'table', event.table,
            'timestamp', event.timestamp,
            'data', JSON.stringify(event)
        ];
        
        const entryId = await this.client.xadd(streamKey, '*', ...fields);
        return entryId;
    }
    
    /**
     * 批量发布事件
     * @param {Array} events - DataEvent 列表
     * @returns {Promise<Array>} Entry ID 列表
     */
    async publishBatch(events) {
        const pipeline = this.client.pipeline();
        
        for (const event of events) {
            const streamKey = `${this.streamPrefix}${event.table}`;
            
            pipeline.xadd(streamKey, '*',
                'event_id', event.event_id,
                'event_type', event.event_type,
                'table', event.table,
                'timestamp', event.timestamp,
                'data', JSON.stringify(event)
            );
        }
        
        const results = await pipeline.exec();
        return results.map(([err, id]) => {
            if (err) throw err;
            return id;
        });
    }
    
    /**
     * 关闭连接
     */
    async close() {
        await this.client.quit();
        console.log('Redis connection closed');
    }
}

// ===============================================
// Kafka 客户端
// ===============================================

class KafkaClient {
    /**
     * @param {Object} options - Kafka 连接选项
     * @param {Array<string>} options.brokers - Kafka broker 列表
     * @param {string} options.topicPrefix - Topic 前缀
     * @param {string} options.clientId - 客户端 ID
     */
    constructor(options = {}) {
        this.topicPrefix = options.topicPrefix || 'miniodb-';
        
        this.kafka = new Kafka({
            clientId: options.clientId || 'miniodb-nodejs-sdk',
            brokers: options.brokers || ['localhost:9092'],
            retry: {
                initialRetryTime: 100,
                retries: 3
            }
        });
        
        this.producer = this.kafka.producer({
            allowAutoTopicCreation: true
        });
        
        this.connected = false;
    }
    
    /**
     * 连接到 Kafka
     */
    async connect() {
        if (!this.connected) {
            await this.producer.connect();
            this.connected = true;
            console.log('Connected to Kafka');
        }
    }
    
    /**
     * 发布事件到 Kafka
     * @param {Object} event - DataEvent
     * @returns {Promise<Object>} RecordMetadata
     */
    async publish(event) {
        await this.connect();
        
        const topic = `${this.topicPrefix}${event.table}`;
        
        const result = await this.producer.send({
            topic,
            messages: [{
                key: event.table,
                value: JSON.stringify(event),
                headers: {
                    'event_id': event.event_id,
                    'event_type': event.event_type
                }
            }]
        });
        
        return result;
    }
    
    /**
     * 批量发布事件
     * @param {Array} events - DataEvent 列表
     * @returns {Promise<Array>} RecordMetadata 列表
     */
    async publishBatch(events) {
        await this.connect();
        
        // 按 topic 分组
        const topicMessages = new Map();
        
        for (const event of events) {
            const topic = `${this.topicPrefix}${event.table}`;
            
            if (!topicMessages.has(topic)) {
                topicMessages.set(topic, []);
            }
            
            topicMessages.get(topic).push({
                key: event.table,
                value: JSON.stringify(event),
                headers: {
                    'event_id': event.event_id,
                    'event_type': event.event_type
                }
            });
        }
        
        // 批量发送
        const results = [];
        for (const [topic, messages] of topicMessages) {
            const result = await this.producer.send({ topic, messages });
            results.push(result);
        }
        
        return results;
    }
    
    /**
     * 关闭连接
     */
    async close() {
        if (this.connected) {
            await this.producer.disconnect();
            this.connected = false;
            console.log('Kafka connection closed');
        }
    }
}

// ===============================================
// 工具函数
// ===============================================

/**
 * 生成测试记录
 * @param {number} index - 索引
 * @returns {Object} DataRecord
 */
function generateRecord(index) {
    return createDataRecord(
        `record_${Date.now()}_${index}`,
        Date.now(),
        {
            user_id: index + 1,
            action: 'click',
            page: `/page/${index % 10}`,
            timestamp: new Date().toISOString()
        }
    );
}

/**
 * 生成测试事件
 * @param {string} table - 表名
 * @param {number} index - 索引
 * @returns {Object} DataEvent
 */
function generateEvent(table, index) {
    const record = generateRecord(index);
    return createInsertEvent(table, record, 'nodejs-sdk-example');
}

/**
 * 生成多个测试事件
 * @param {string} table - 表名
 * @param {number} count - 数量
 * @returns {Array} DataEvent 列表
 */
function generateEvents(table, count) {
    const events = [];
    for (let i = 0; i < count; i++) {
        events.push(generateEvent(table, i));
    }
    return events;
}

// ===============================================
// 示例运行函数
// ===============================================

async function runRedisExample(options) {
    console.log(`Connecting to Redis at ${options.redisHost}:${options.redisPort}...`);
    
    const client = new RedisStreamClient({
        host: options.redisHost,
        port: options.redisPort,
        password: options.redisPassword || undefined
    });
    
    try {
        console.log(`Connected. Publishing ${options.count} events to table '${options.table}'...`);
        
        if (options.batch) {
            // 批量发送
            const events = generateEvents(options.table, options.count);
            
            const start = Date.now();
            await client.publishBatch(events);
            const elapsed = Date.now() - start;
            
            console.log(`Published ${options.count} events in batch mode in ${elapsed} ms`);
        } else {
            // 逐条发送
            for (let i = 0; i < options.count; i++) {
                const event = generateEvent(options.table, i);
                const entryId = await client.publish(event);
                console.log(`Published event ${i + 1}: ${event.event_id} -> ${entryId}`);
            }
        }
        
        console.log('Done!');
    } finally {
        await client.close();
    }
}

async function runKafkaExample(options) {
    const brokers = options.kafkaBrokers.split(',');
    console.log(`Connecting to Kafka at ${brokers.join(', ')}...`);
    
    const client = new KafkaClient({ brokers });
    
    try {
        console.log(`Publishing ${options.count} events to table '${options.table}'...`);
        
        if (options.batch) {
            // 批量发送
            const events = generateEvents(options.table, options.count);
            
            const start = Date.now();
            await client.publishBatch(events);
            const elapsed = Date.now() - start;
            
            console.log(`Published ${options.count} events in batch mode in ${elapsed} ms`);
        } else {
            // 逐条发送
            for (let i = 0; i < options.count; i++) {
                const event = generateEvent(options.table, i);
                await client.publish(event);
                console.log(`Published event ${i + 1}: ${event.event_id}`);
            }
        }
        
        console.log('Done!');
    } finally {
        await client.close();
    }
}

async function runGrpcExample(options) {
    console.log('gRPC mode requires proto file compilation.');
    console.log('Please use the Go SDK for gRPC examples, or compile the proto files first:');
    console.log('');
    console.log('  npm install @grpc/proto-loader @grpc/grpc-js');
    console.log('  # Then load the proto file dynamically in your code');
    console.log('');
    process.exit(1);
}

// ===============================================
// 主程序
// ===============================================

const program = new Command();

program
    .name('miniodb-client')
    .description('MinIODB Node.js SDK Example')
    .version('1.0.0')
    .option('-m, --mode <mode>', 'Client mode: redis, kafka, or grpc', 'redis')
    .option('--redis-host <host>', 'Redis host', 'localhost')
    .option('--redis-port <port>', 'Redis port', '6379')
    .option('--redis-password <password>', 'Redis password')
    .option('--kafka-brokers <brokers>', 'Kafka brokers (comma-separated)', 'localhost:9092')
    .option('--grpc-addr <addr>', 'gRPC server address', 'localhost:8080')
    .option('-t, --table <table>', 'Target table name', 'user_events')
    .option('-c, --count <count>', 'Number of events to send', '10')
    .option('-b, --batch', 'Use batch mode', false);

program.parse();

const options = program.opts();
options.count = parseInt(options.count, 10);
options.redisPort = parseInt(options.redisPort, 10);

console.log('MinIODB Node.js SDK Example');
console.log(`Mode: ${options.mode}, Table: ${options.table}, Count: ${options.count}, Batch: ${options.batch}`);
console.log('');

(async () => {
    try {
        switch (options.mode) {
            case 'redis':
                await runRedisExample(options);
                break;
            case 'kafka':
                await runKafkaExample(options);
                break;
            case 'grpc':
                await runGrpcExample(options);
                break;
            default:
                console.error(`Unknown mode: ${options.mode}. Use 'redis', 'kafka', or 'grpc'.`);
                process.exit(1);
        }
    } catch (error) {
        console.error('Error:', error.message);
        process.exit(1);
    }
})();

// ===============================================
// 导出供外部使用
// ===============================================

export {
    createDataRecord,
    createEventMetadata,
    createDataEvent,
    createInsertEvent,
    createBatchEvent,
    RedisStreamClient,
    KafkaClient
};
