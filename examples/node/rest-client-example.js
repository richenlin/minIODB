const axios = require('axios');
const winston = require('winston');

/**
 * MinIODB Node.js REST客户端示例
 * 演示如何使用Node.js客户端连接MinIODB REST API
 * 包含表管理功能支持
 */
class MinIODBRestClient {
    constructor(options = {}) {
        this.baseURL = options.baseURL || process.env.MINIODB_REST_HOST || 'http://localhost:8081';
        this.jwtToken = options.jwtToken || process.env.MINIODB_JWT_TOKEN || 'your-jwt-token-here';
        
        // 配置winston日志
        this.logger = winston.createLogger({
            level: 'info',
            format: winston.format.combine(
                winston.format.timestamp(),
                winston.format.colorize(),
                winston.format.simple()
            ),
            transports: [
                new winston.transports.Console()
            ]
        });
        
        // 配置axios客户端
        this.client = axios.create({
            baseURL: this.baseURL,
            timeout: 30000,
            headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${this.jwtToken}`
            }
        });
        
        // 添加请求拦截器
        this.client.interceptors.request.use(
            (config) => {
                this.logger.info(`发送 ${config.method.toUpperCase()} 请求到 ${config.url}`);
                return config;
            },
            (error) => {
                this.logger.error('请求错误:', error.message);
                return Promise.reject(error);
            }
        );
        
        // 添加响应拦截器
        this.client.interceptors.response.use(
            (response) => {
                this.logger.info(`收到响应: ${response.status} ${response.statusText}`);
                return response;
            },
            (error) => {
                if (error.response) {
                    this.logger.error(`HTTP错误 ${error.response.status}: ${error.response.data}`);
                } else {
                    this.logger.error('网络错误:', error.message);
                }
                return Promise.reject(error);
            }
        );
        
        this.logger.info(`REST客户端已配置，服务器地址: ${this.baseURL}`);
    }
    
    /**
     * 健康检查
     */
    async healthCheck() {
        try {
            this.logger.info('=== 健康检查 ===');
            
            const response = await this.client.get('/v1/health');
            const result = response.data;
            
            this.logger.info('健康检查结果:');
            this.logger.info(`  状态: ${result.status}`);
            this.logger.info(`  时间: ${result.timestamp}`);
            this.logger.info(`  版本: ${result.version}`);
            this.logger.info(`  详情: ${JSON.stringify(result.details, null, 2)}`);
            
            return result;
        } catch (error) {
            this.logger.error('健康检查失败:', error.message);
            throw error;
        }
    }

    /**
     * 创建表
     */
    async createTable() {
        try {
            this.logger.info('=== 创建表 ===');
            
            const requestData = {
                table_name: 'users',
                config: {
                    buffer_size: 1000,
                    flush_interval_seconds: 30,
                    retention_days: 365,
                    backup_enabled: true,
                    properties: {
                        description: '用户数据表',
                        owner: 'user-service'
                    }
                },
                if_not_exists: true
            };
            
            const response = await this.client.post('/v1/tables', requestData);
            const result = response.data;
            
            this.logger.info('创建表结果:');
            this.logger.info(`  成功: ${result.success}`);
            this.logger.info(`  消息: ${result.message}`);
            
            return result;
        } catch (error) {
            this.logger.error('创建表失败:', error.message);
            throw error;
        }
    }

    /**
     * 列出表
     */
    async listTables() {
        try {
            this.logger.info('=== 列出表 ===');
            
            const response = await this.client.get('/v1/tables');
            const result = response.data;
            
            this.logger.info('表列表:');
            this.logger.info(`  总数: ${result.total}`);
            
            result.tables.forEach(table => {
                this.logger.info(`  - 表名: ${table.name}, 状态: ${table.status}, 创建时间: ${table.created_at}`);
            });
            
            return result;
        } catch (error) {
            this.logger.error('列出表失败:', error.message);
            throw error;
        }
    }

    /**
     * 描述表
     */
    async describeTable() {
        try {
            this.logger.info('=== 描述表 ===');
            
            const response = await this.client.get('/v1/tables/users');
            const result = response.data;
            
            const tableInfo = result.table_info;
            const stats = result.stats;
            const config = tableInfo.config;
            
            this.logger.info('表详情:');
            this.logger.info(`  表名: ${tableInfo.name}`);
            this.logger.info(`  状态: ${tableInfo.status}`);
            this.logger.info(`  创建时间: ${tableInfo.created_at}`);
            this.logger.info(`  最后写入: ${tableInfo.last_write}`);
            this.logger.info(`  配置: 缓冲区大小=${config.buffer_size}, 刷新间隔=${config.flush_interval_seconds}s, 保留天数=${config.retention_days}`);
            this.logger.info(`  统计: 记录数=${stats.record_count}, 文件数=${stats.file_count}, 大小=${stats.size_bytes}字节`);
            
            return result;
        } catch (error) {
            this.logger.error('描述表失败:', error.message);
            throw error;
        }
    }
    
    /**
     * 写入数据（支持表）
     */
    async writeData() {
        try {
            this.logger.info('=== 数据写入 ===');
            
            const requestData = {
                table: 'users',  // 指定表名
                id: 'user123',
                timestamp: new Date().toISOString(),
                payload: {
                    user_id: 'user123',
                    action: 'login',
                    score: 95.5,
                    success: true,
                    metadata: {
                        browser: 'Chrome',
                        ip: '192.168.1.100'
                    }
                }
            };
            
            const response = await this.client.post('/v1/data', requestData);
            const result = response.data;
            
            this.logger.info('数据写入结果:');
            this.logger.info(`  表名: users`);
            this.logger.info(`  成功: ${result.success}`);
            this.logger.info(`  消息: ${result.message}`);
            this.logger.info(`  节点ID: ${result.node_id}`);
            
            return result;
        } catch (error) {
            this.logger.error('数据写入失败:', error.message);
            throw error;
        }
    }
    
    /**
     * 查询数据（使用表名）
     */
    async queryData() {
        try {
            this.logger.info('=== 数据查询 ===');
            
            const sql = `
                SELECT 
                    COUNT(*) as total,
                    AVG(score) as avg_score,
                    MAX(score) as max_score,
                    MIN(score) as min_score
                FROM users 
                WHERE user_id = 'user123' 
                AND timestamp >= '2024-01-01'
            `;
            
            const queryData = { sql: sql.trim() };
            const response = await this.client.post('/v1/query', queryData);
            const result = response.data;
            
            this.logger.info('查询结果:');
            this.logger.info(`  SQL: ${sql.trim()}`);
            this.logger.info(`  结果JSON: ${result.result_json}`);
            
            // 尝试解析结果JSON
            try {
                const resultData = JSON.parse(result.result_json);
                this.logger.info(`  解析后结果: ${JSON.stringify(resultData, null, 2)}`);
            } catch (parseError) {
                this.logger.warn(`  JSON解析失败: ${parseError.message}`);
            }
            
            return result;
        } catch (error) {
            this.logger.error('数据查询失败:', error.message);
            throw error;
        }
    }

    /**
     * 跨表查询
     */
    async crossTableQuery() {
        try {
            this.logger.info('=== 跨表查询 ===');
            
            // 首先创建订单表
            const orderTableData = {
                table_name: 'orders',
                config: {
                    buffer_size: 2000,
                    flush_interval_seconds: 15,
                    retention_days: 2555,
                    backup_enabled: true,
                    properties: {
                        description: '订单数据表',
                        owner: 'order-service'
                    }
                },
                if_not_exists: true
            };
            
            try {
                await this.client.post('/v1/tables', orderTableData);
            } catch (error) {
                this.logger.warn(`创建订单表失败（可能已存在）: ${error.message}`);
            }
            
            // 写入订单数据
            const orderData = {
                table: 'orders',
                id: 'order456',
                timestamp: new Date().toISOString(),
                payload: {
                    order_id: 'order456',
                    user_id: 'user123',
                    amount: 299.99,
                    status: 'completed'
                }
            };
            
            try {
                await this.client.post('/v1/data', orderData);
            } catch (error) {
                this.logger.warn(`写入订单数据失败: ${error.message}`);
            }
            
            // 执行跨表查询
            const crossSql = `
                SELECT 
                    u.user_id,
                    u.action,
                    o.order_id,
                    o.amount
                FROM users u 
                JOIN orders o ON u.user_id = o.user_id 
                WHERE u.user_id = 'user123'
            `;
            
            const crossQueryData = { sql: crossSql.trim() };
            const response = await this.client.post('/v1/query', crossQueryData);
            const result = response.data;
            
            this.logger.info('跨表查询结果:');
            this.logger.info(`  SQL: ${crossSql.trim()}`);
            this.logger.info(`  结果JSON: ${result.result_json}`);
            
            return result;
        } catch (error) {
            this.logger.error('跨表查询失败:', error.message);
            throw error;
        }
    }
    
    /**
     * 触发备份
     */
    async triggerBackup() {
        try {
            this.logger.info('=== 触发备份 ===');
            
            const backupData = {
                id: 'user123',
                day: '2024-01-15'
            };
            
            const response = await this.client.post('/v1/backup/trigger', backupData);
            const result = response.data;
            
            this.logger.info('备份结果:');
            this.logger.info(`  成功: ${result.success}`);
            this.logger.info(`  消息: ${result.message}`);
            this.logger.info(`  备份文件数: ${result.files_backed_up}`);
            
            return result;
        } catch (error) {
            this.logger.error('备份操作失败:', error.message);
            throw error;
        }
    }
    
    /**
     * 数据恢复
     */
    async recoverData() {
        try {
            this.logger.info('=== 数据恢复 ===');
            
            const recoverData = {
                time_range: {
                    start_date: '2024-01-01',
                    end_date: '2024-01-15',
                    ids: ['user123']
                },
                force_overwrite: false
            };
            
            const response = await this.client.post('/v1/backup/recover', recoverData);
            const result = response.data;
            
            this.logger.info('恢复结果:');
            this.logger.info(`  成功: ${result.success}`);
            this.logger.info(`  消息: ${result.message}`);
            this.logger.info(`  恢复文件数: ${result.files_recovered}`);
            this.logger.info(`  恢复的键: ${JSON.stringify(result.recovered_keys)}`);
            
            return result;
        } catch (error) {
            this.logger.error('数据恢复失败:', error.message);
            throw error;
        }
    }
    
    /**
     * 获取系统统计信息
     */
    async getStats() {
        try {
            this.logger.info('=== 系统统计 ===');
            
            const response = await this.client.get('/v1/stats');
            const result = response.data;
            
            this.logger.info('系统统计:');
            this.logger.info(`  时间戳: ${result.timestamp}`);
            this.logger.info(`  缓冲区统计: ${JSON.stringify(result.buffer_stats, null, 2)}`);
            this.logger.info(`  Redis统计: ${JSON.stringify(result.redis_stats, null, 2)}`);
            this.logger.info(`  MinIO统计: ${JSON.stringify(result.minio_stats, null, 2)}`);
            
            return result;
        } catch (error) {
            this.logger.error('获取统计信息失败:', error.message);
            throw error;
        }
    }
    
    /**
     * 获取节点信息
     */
    async getNodes() {
        try {
            this.logger.info('=== 节点信息 ===');
            
            const response = await this.client.get('/v1/nodes');
            const result = response.data;
            
            this.logger.info('节点信息:');
            this.logger.info(`  总数: ${result.total}`);
            this.logger.info('  节点列表:');
            
            result.nodes.forEach(node => {
                this.logger.info(`    - ID: ${node.id}, 状态: ${node.status}, 类型: ${node.type}, 地址: ${node.address}, 最后活跃: ${node.last_seen}`);
            });
            
            return result;
        } catch (error) {
            this.logger.error('获取节点信息失败:', error.message);
            throw error;
        }
    }

    /**
     * 删除表（可选，用于清理）
     */
    async dropTable() {
        try {
            this.logger.info('=== 删除表 ===');
            
            const dropData = {
                table_name: 'orders',
                if_exists: true,
                cascade: true
            };
            
            const response = await this.client.delete('/v1/tables/orders', { data: dropData });
            const result = response.data;
            
            this.logger.info('删除表结果:');
            this.logger.info(`  成功: ${result.success}`);
            this.logger.info(`  消息: ${result.message}`);
            this.logger.info(`  删除文件数: ${result.files_deleted}`);
            
            return result;
        } catch (error) {
            this.logger.error('删除表失败:', error.message);
            throw error;
        }
    }
    
    /**
     * 运行所有示例
     */
    async runAllExamples() {
        this.logger.info('开始运行MinIODB Node.js REST客户端示例（包含表管理功能）...');
        
        const examples = [
            { name: '健康检查', fn: () => this.healthCheck() },
            { name: '创建表', fn: () => this.createTable() },
            { name: '列出表', fn: () => this.listTables() },
            { name: '描述表', fn: () => this.describeTable() },
            { name: '数据写入', fn: () => this.writeData() },
            { name: '数据查询', fn: () => this.queryData() },
            { name: '跨表查询', fn: () => this.crossTableQuery() },
            { name: '触发备份', fn: () => this.triggerBackup() },
            { name: '数据恢复', fn: () => this.recoverData() },
            { name: '获取统计', fn: () => this.getStats() },
            { name: '获取节点', fn: () => this.getNodes() },
            // { name: '删除表', fn: () => this.dropTable() }, // 可选，用于清理
        ];
        
        for (const example of examples) {
            try {
                this.logger.info(`\n--- 执行: ${example.name} ---`);
                await example.fn();
                await this.sleep(500); // 短暂延迟
            } catch (error) {
                this.logger.error(`${example.name}失败:`, error.message);
            }
        }
        
        this.logger.info('所有示例运行完成!');
    }
    
    /**
     * 批量写入数据示例（扩展功能）
     */
    async batchWriteData() {
        try {
            this.logger.info('=== 批量数据写入 ===');
            
            const batchData = [
                {
                    table: 'users',
                    id: 'user456',
                    timestamp: new Date().toISOString(),
                    payload: {
                        user_id: 'user456',
                        action: 'register',
                        score: 88.0,
                        success: true
                    }
                },
                {
                    table: 'users',
                    id: 'user789',
                    timestamp: new Date().toISOString(),
                    payload: {
                        user_id: 'user789',
                        action: 'logout',
                        score: 92.3,
                        success: true
                    }
                }
            ];
            
            const promises = batchData.map(data => this.client.post('/v1/data', data));
            const responses = await Promise.all(promises);
            
            this.logger.info('批量写入结果:');
            responses.forEach((response, index) => {
                const result = response.data;
                this.logger.info(`  记录${index + 1}: 成功=${result.success}, 消息=${result.message}`);
            });
            
            return responses.map(r => r.data);
        } catch (error) {
            this.logger.error('批量数据写入失败:', error.message);
            throw error;
        }
    }

    /**
     * 短暂休眠
     */
    async sleep(ms) {
        return new Promise(resolve => setTimeout(resolve, ms));
    }
}

/**
 * 主函数
 */
async function main() {
    const client = new MinIODBRestClient();
    
    try {
        await client.runAllExamples();
        
        // 可选：运行批量写入示例
        // await client.batchWriteData();
        
    } catch (error) {
        console.error('示例运行失败:', error.message);
        process.exit(1);
    }
}

// 如果直接运行此文件，则执行主函数
if (require.main === module) {
    main();
}

module.exports = MinIODBRestClient; 