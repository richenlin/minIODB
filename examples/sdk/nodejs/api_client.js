#!/usr/bin/env node

/**
 * MinIODB Node.js gRPC 客户端示例
 *
 * 本示例展示如何使用 gRPC 调用 MinIODB 的各种 API
 * 包括数据操作、表管理、流式操作等
 *
 * 依赖安装:
 *   npm install @grpc/grpc-js @grpc/proto-loader
 *
 * 需要先加载 proto 文件
 *
 * 使用方法:
 *   node api_client.js --addr localhost:8080
 */

import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

// 加载 proto 文件
const PROTO_PATH = join(__dirname, '../../../api/proto/miniodb/v1/miniodb.proto');

/**
 * MinioDB gRPC 客户端封装
 */
class MinioDBGrpcClient {
    /**
     * 创建 MinIODB gRPC 客户端
     * @param {string} addr - MinIODB gRPC 服务器地址
     */
    constructor(addr) {
        this.addr = addr;

        // 加载 proto 定义
        const packageDefinition = protoLoader.loadSync(PROTO_PATH, {
            keepCase: true,
            longs: String,
            enums: String,
            defaults: true,
            oneofs: true
        });

        const miniodbProto = grpc.loadPackageDefinition(packageDefinition).miniodb.v1;

        // 创建客户端
        this.client = new miniodbProto.MinIODBService(addr, grpc.credentials.createInsecure());

        console.log(`Connected to MinIODB at ${addr}`);
    }

    /**
     * 关闭连接
     */
    close() {
        if (this.channel) {
            this.channel.close();
            console.log('Connection closed');
        }
    }

    // ===============================================
    // 数据操作
    // ===============================================

    /**
     * 写入数据
     * @param {string} table - 表名
     * @param {string} recordId - 记录 ID
     * @param {Object} payload - 数据负载
     * @returns {Promise<Object>} 响应数据
     */
    writeData(table, recordId, payload) {
        return new Promise((resolve, reject) => {
            const request = {
                table: table,
                data: {
                    id: recordId,
                    timestamp: { seconds: Math.floor(Date.now() / 1000), nanos: 0 },
                    payload: { fields: this.convertToStruct(payload) }
                }
            };

            this.client.writeData(request, (error, response) => {
                if (error) {
                    console.error(`✗ Write failed: ${error.message}`);
                    return reject(error);
                }

                if (response.success) {
                    console.log(`✓ Data written successfully to table '${table}', ID: ${recordId}, Node: ${response.node_id}`);
                } else {
                    console.error(`✗ Write failed: ${response.message}`);
                }

                resolve(response);
            });
        });
    }

    /**
     * 查询数据
     * @param {string} sql - SQL 查询语句
     * @param {number} limit - 返回记录数限制
     * @returns {Promise<Object>} 查询结果
     */
    queryData(sql, limit = 100) {
        return new Promise((resolve, reject) => {
            const request = {
                sql: sql,
                limit: limit
            };

            this.client.queryData(request, (error, response) => {
                if (error) {
                    console.error(`✗ Query failed: ${error.message}`);
                    return reject(error);
                }

                console.log(`✓ Query executed successfully, has_more: ${response.has_more}`);
                resolve(response);
            });
        });
    }

    /**
     * 更新数据
     * @param {string} table - 表名
     * @param {string} recordId - 记录 ID
     * @param {Object} payload - 更新的数据
     * @returns {Promise<Object>} 响应数据
     */
    updateData(table, recordId, payload) {
        return new Promise((resolve, reject) => {
            const request = {
                table: table,
                id: recordId,
                timestamp: { seconds: Math.floor(Date.now() / 1000), nanos: 0 },
                payload: { fields: this.convertToStruct(payload) }
            };

            this.client.updateData(request, (error, response) => {
                if (error) {
                    console.error(`✗ Update failed: ${error.message}`);
                    return reject(error);
                }

                if (response.success) {
                    console.log(`✓ Data updated successfully, ID: ${recordId}`);
                } else {
                    console.error(`✗ Update failed: ${response.message}`);
                }

                resolve(response);
            });
        });
    }

    /**
     * 删除数据
     * @param {string} table - 表名
     * @param {string} recordId - 记录 ID
     * @param {boolean} softDelete - 是否软删除
     * @returns {Promise<Object>} 响应数据
     */
    deleteData(table, recordId, softDelete = false) {
        return new Promise((resolve, reject) => {
            const request = {
                table: table,
                id: recordId,
                soft_delete: softDelete
            };

            this.client.deleteData(request, (error, response) => {
                if (error) {
                    console.error(`✗ Delete failed: ${error.message}`);
                    return reject(error);
                }

                if (response.success) {
                    console.log(`✓ Data deleted successfully, deleted_count: ${response.deleted_count}`);
                } else {
                    console.error(`✗ Delete failed: ${response.message}`);
                }

                resolve(response);
            });
        });
    }

    // ===============================================
    // 流式操作
    // ===============================================

    /**
     * 批量流式写入
     * @param {string} table - 表名
     * @param {Array} recordsData - 记录数据列表
     * @returns {Promise<Object>} 响应数据
     */
    streamWrite(table, recordsData) {
        return new Promise((resolve, reject) => {
            console.log(`Stream writing ${recordsData.length} records to table '${table}'`);

            const stream = this.client.streamWrite();

            // 分批发送
            const batchSize = 100;
            let sentCount = 0;

            for (let i = 0; i < recordsData.length; i += batchSize) {
                const end = Math.min(i + batchSize, recordsData.length);
                const batch = recordsData.slice(i, end);

                const records = batch.map(r => ({
                    id: r.id,
                    timestamp: { seconds: Math.floor(Date.now() / 1000), nanos: 0 },
                    payload: { fields: this.convertToStruct(r.payload) }
                }));

                stream.write({
                    table: table,
                    records: records
                });

                sentCount += batch.length;
                console.log(`  Sent batch ${i}-${end - 1}`);
            }

            stream.end();

            stream.on('data', (response) => {
                console.log(`✓ Stream write completed: ${response.records_count} records, ${response.errors.length} errors`);
            });

            stream.on('error', (error) => {
                console.error(`✗ Stream error: ${error.message}`);
                reject(error);
            });

            stream.on('end', () => {
                console.log('Stream write ended');
            });

            stream.on('status', (status) => {
                if (status.code === grpc.status.OK) {
                    resolve({ success: true, recordsCount: sentCount });
                } else {
                    reject(new Error(`Stream failed with status: ${status.details}`));
                }
            });
        });
    }

    /**
     * 流式查询
     * @param {string} sql - SQL 查询语句
     * @param {number} batchSize - 批次大小
     * @returns {Promise<Array>} 所有记录列表
     */
    streamQuery(sql, batchSize = 100) {
        return new Promise((resolve, reject) => {
            const allRecords = [];

            const stream = this.client.streamQuery({
                sql: sql,
                batch_size: batchSize,
                cursor: ''
            });

            stream.on('data', (response) => {
                allRecords.push(...response.records);
                console.log(`  Received batch: ${response.records.length} records, has_more: ${response.has_more}, cursor: ${response.cursor}`);

                if (!response.has_more) {
                    stream.cancel();
                }
            });

            stream.on('error', (error) => {
                console.error(`✗ Stream error: ${error.message}`);
                reject(error);
            });

            stream.on('end', () => {
                console.log(`✓ Stream query completed: ${allRecords.length} total records`);
                resolve(allRecords);
            });

            stream.on('status', (status) => {
                if (status.code !== grpc.status.OK && status.code !== grpc.status.CANCELLED) {
                    reject(new Error(`Stream failed with status: ${status.details}`));
                }
            });
        });
    }

    // ===============================================
    // 表管理
    // ===============================================

    /**
     * 创建表
     * @param {string} tableName - 表名
     * @param {Object} config - 表配置
     * @returns {Promise<Object>} 响应数据
     */
    createTable(tableName, config) {
        return new Promise((resolve, reject) => {
            const request = {
                table_name: tableName,
                config: this.convertTableConfig(config),
                if_not_exists: true
            };

            this.client.createTable(request, (error, response) => {
                if (error) {
                    console.error(`✗ Create table failed: ${error.message}`);
                    return reject(error);
                }

                if (response.success) {
                    console.log(`✓ Table '${tableName}' created successfully`);
                } else {
                    console.error(`✗ Create table failed: ${response.message}`);
                }

                resolve(response);
            });
        });
    }

    /**
     * 列出表
     * @param {string} pattern - 表名模式过滤
     * @returns {Promise<Array>} 表信息列表
     */
    listTables(pattern = '*') {
        return new Promise((resolve, reject) => {
            const request = {
                pattern: pattern
            };

            this.client.listTables(request, (error, response) => {
                if (error) {
                    console.error(`✗ List tables failed: ${error.message}`);
                    return reject(error);
                }

                console.log(`✓ Listed ${response.total} tables`);
                resolve(response.tables);
            });
        });
    }

    /**
     * 获取表信息
     * @param {string} tableName - 表名
     * @returns {Promise<Object>} 表信息
     */
    getTable(tableName) {
        return new Promise((resolve, reject) => {
            const request = {
                table_name: tableName
            };

            this.client.getTable(request, (error, response) => {
                if (error) {
                    console.error(`✗ Get table info failed: ${error.message}`);
                    return reject(error);
                }

                console.log(`✓ Got table info for '${tableName}'`);
                resolve(response.table_info);
            });
        });
    }

    /**
     * 删除表
     * @param {string} tableName - 表名
     * @returns {Promise<Object>} 响应数据
     */
    deleteTable(tableName) {
        return new Promise((resolve, reject) => {
            const request = {
                table_name: tableName,
                if_exists: false,
                cascade: false
            };

            this.client.deleteTable(request, (error, response) => {
                if (error) {
                    console.error(`✗ Delete table failed: ${error.message}`);
                    return reject(error);
                }

                if (response.success) {
                    console.log(`✓ Table '${tableName}' deleted successfully, files deleted: ${response.files_deleted}`);
                } else {
                    console.error(`✗ Delete table failed: ${response.message}`);
                }

                resolve(response);
            });
        });
    }

    // ===============================================
    // 元数据管理
    // ===============================================

    /**
     * 备份元数据
     * @returns {Promise<Object>} 响应数据
     */
    backupMetadata() {
        return new Promise((resolve, reject) => {
            const request = {
                force: false
            };

            this.client.backupMetadata(request, (error, response) => {
                if (error) {
                    console.error(`✗ Backup failed: ${error.message}`);
                    return reject(error);
                }

                if (response.success) {
                    console.log(`✓ Metadata backed up successfully, ID: ${response.backup_id}`);
                } else {
                    console.error(`✗ Backup failed: ${response.message}`);
                }

                resolve(response);
            });
        });
    }

    /**
     * 恢复元数据
     * @param {boolean} fromLatest - 是否从最新备份恢复
     * @returns {Promise<Object>} 响应数据
     */
    restoreMetadata(fromLatest = true) {
        return new Promise((resolve, reject) => {
            const request = {
                from_latest: fromLatest,
                dry_run: false,
                overwrite: false,
                validate: true,
                parallel: true
            };

            this.client.restoreMetadata(request, (error, response) => {
                if (error) {
                    console.error(`✗ Restore failed: ${error.message}`);
                    return reject(error);
                }

                if (response.success) {
                    console.log(`✓ Metadata restored successfully: total=${response.entries_total}, ok=${response.entries_ok}, skipped=${response.entries_skipped}, errors=${response.entries_error}`);
                } else {
                    console.error(`✗ Restore failed: ${response.message}`);
                }

                resolve(response);
            });
        });
    }

    /**
     * 列出备份
     * @param {number} days - 查询最近多少天的备份
     * @returns {Promise<Array>} 备份信息列表
     */
    listBackups(days = 30) {
        return new Promise((resolve, reject) => {
            const request = {
                days: days
            };

            this.client.listBackups(request, (error, response) => {
                if (error) {
                    console.error(`✗ List backups failed: ${error.message}`);
                    return reject(error);
                }

                console.log(`✓ Listed ${response.total} backups`);
                resolve(response.backups);
            });
        });
    }

    // ===============================================
    // 健康检查和监控
    // ===============================================

    /**
     * 健康检查
     * @returns {Promise<Object>} 响应数据
     */
    healthCheck() {
        return new Promise((resolve, reject) => {
            const request = {};

            this.client.healthCheck(request, (error, response) => {
                if (error) {
                    console.error(`✗ Health check failed: ${error.message}`);
                    return reject(error);
                }

                console.log(`✓ Health check: status=${response.status}, version=${response.version}`);
                resolve(response);
            });
        });
    }

    /**
     * 获取状态
     * @returns {Promise<Object>} 状态数据
     */
    getStatus() {
        return new Promise((resolve, reject) => {
            const request = {};

            this.client.getStatus(request, (error, response) => {
                if (error) {
                    console.error(`✗ Get status failed: ${error.message}`);
                    return reject(error);
                }

                console.log(`✓ Got status: total_nodes=${response.total_nodes}`);
                resolve(response);
            });
        });
    }

    // ===============================================
    // 辅助方法
    // ===============================================

    /**
     * 将普通对象转换为 protobuf Struct
     * @param {Object} obj - 普通对象
     * @returns {Object} protobuf Struct
     */
    convertToStruct(obj) {
        const fields = {};

        for (const [key, value] of Object.entries(obj)) {
            fields[key] = this.convertToValue(value);
        }

        return fields;
    }

    /**
     * 将值转换为 protobuf Value
     * @param {*} value - 值
     * @returns {Object} protobuf Value
     */
    convertToValue(value) {
        if (value === null || value === undefined) {
            return { nullValue: 0 };
        } else if (typeof value === 'string') {
            return { stringValue: value };
        } else if (typeof value === 'number') {
            return { numberValue: value };
        } else if (typeof value === 'boolean') {
            return { boolValue: value };
        } else if (Array.isArray(value)) {
            return {
                listValue: {
                    values: value.map(v => this.convertToValue(v))
                }
            };
        } else if (typeof value === 'object') {
            return {
                structValue: {
                    fields: this.convertToStruct(value)
                }
            };
        }
        return { stringValue: String(value) };
    }

    /**
     * 转换表配置
     * @param {Object} config - 表配置
     * @returns {Object} protobuf TableConfig
     */
    convertTableConfig(config) {
        return {
            buffer_size: config.bufferSize || 1000,
            flush_interval_seconds: config.flushIntervalSeconds || 60,
            retention_days: config.retentionDays || 30,
            backup_enabled: config.backupEnabled || true,
            id_strategy: config.idStrategy || 'uuid',
            auto_generate_id: config.autoGenerateId || true,
            id_validation: {
                max_length: config.idValidation?.maxLength || 255,
                pattern: config.idValidation?.pattern || '^[a-zA-Z0-9_-]+$'
            }
        };
    }
}

// ===============================================
// 主程序
// ===============================================

import { Command } from 'commander';

const program = new Command();

program
    .name('miniodb-api-client')
    .description('MinIODB Node.js gRPC API Client Example')
    .version('1.0.0')
    .option('--addr <address>', 'MinIODB gRPC server address', 'localhost:8080');

program.parse();

const options = program.opts();

async function main() {
    const client = new MinioDBGrpcClient(options.addr);

    try {
        console.log('========================================');
        console.log('MinIODB Node.js gRPC Client Example');
        console.log(`Server: ${options.addr}`);
        console.log('========================================');

        // 1. 健康检查
        console.log('\n--- Health Check ---');
        await client.healthCheck();

        // 2. 创建表
        console.log('\n--- Create Table ---');
        const tableName = 'users';
        const tableConfig = {
            bufferSize: 1000,
            flushIntervalSeconds: 60,
            retentionDays: 30,
            backupEnabled: true,
            idStrategy: 'uuid',
            autoGenerateId: true,
            idValidation: {
                maxLength: 255,
                pattern: '^[a-zA-Z0-9_-]+$'
            }
        };
        await client.createTable(tableName, tableConfig);

        // 3. 写入数据
        console.log('\n--- Write Data ---');
        for (let i = 0; i < 5; i++) {
            const payload = {
                name: `User ${i}`,
                email: `user${i}@example.com`,
                age: 25 + i,
                active: true,
                created_at: new Date().toISOString()
            };
            const recordId = `user_${i}`;
            await client.writeData(tableName, recordId, payload);
            await new Promise(resolve => setTimeout(resolve, 100));
        }

        // 4. 查询数据
        console.log('\n--- Query Data ---');
        const result = await client.queryData(`SELECT * FROM ${tableName} LIMIT 3`, 3);
        console.log('Query result:');
        console.log(result.result_json);

        // 5. 更新数据
        console.log('\n--- Update Data ---');
        const updatePayload = {
            name: 'Updated User 0',
            updated_at: new Date().toISOString()
        };
        await client.updateData(tableName, 'user_0', updatePayload);

        // 6. 删除数据
        console.log('\n--- Delete Data ---');
        await client.deleteData(tableName, 'user_4');

        // 7. 流式写入
        console.log('\n--- Stream Write ---');
        const streamRecords = [];
        for (let i = 10; i < 20; i++) {
            streamRecords.push({
                id: `stream_user_${i}`,
                payload: {
                    name: `Stream User ${i}`,
                    batch: true,
                    created_at: new Date().toISOString()
                }
            });
        }
        await client.streamWrite(tableName, streamRecords);

        // 8. 列出表
        console.log('\n--- List Tables ---');
        const tables = await client.listTables('*');
        for (const t of tables) {
            console.log(`  - Table: ${t.name}, Status: ${t.status}`);
            if (t.stats) {
                console.log(`    Records: ${t.stats.record_count}, Files: ${t.stats.file_count}`);
            }
        }

        // 9. 获取表信息
        console.log('\n--- Get Table Info ---');
        const tableInfo = await client.getTable(tableName);
        if (tableInfo && tableInfo.stats) {
            console.log(`  - Record count: ${tableInfo.stats.record_count}`);
            console.log(`  - File count: ${tableInfo.stats.file_count}`);
            console.log(`  - Size: ${tableInfo.stats.size_bytes} bytes`);
        }

        // 10. 获取状态
        console.log('\n--- Get Status ---');
        const status = await client.getStatus();
        console.log(`  - Total nodes: ${status.total_nodes}`);
        console.log(`  - Buffer stats: ${JSON.stringify(status.buffer_stats, null, 2)}`);
        console.log(`  - Redis stats: ${JSON.stringify(status.redis_stats, null, 2)}`);

        // 11. 列出备份
        console.log('\n--- List Backups ---');
        const backups = await client.listBackups(30);
        console.log(`  Found ${backups.length} backups in the last 30 days`);
        for (const b of backups) {
            console.log(`  - Backup: ${b.object_name}, Size: ${b.size}, Node: ${b.node_id}`);
        }

        console.log('\n========================================');
        console.log('Example completed successfully!');
        console.log('========================================');

    } catch (error) {
        console.error('Error:', error.message);
        process.exit(1);
    } finally {
        client.close();
    }
}

main();
