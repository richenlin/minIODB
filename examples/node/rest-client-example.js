const axios = require('axios');
const winston = require('winston');

/**
 * MinIODB Node.js REST客户端示例
 * 演示如何使用Node.js客户端连接MinIODB REST API
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
     * 写入数据
     */
    async writeData() {
        try {
            this.logger.info('=== 数据写入 ===');
            
            const requestData = {
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
     * 查询数据
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
                FROM table 
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
            
            if (result.nodes && Array.isArray(result.nodes)) {
                result.nodes.forEach(node => {
                    this.logger.info(`    - ID: ${node.id}, 状态: ${node.status}, 类型: ${node.type}, 地址: ${node.address}, 最后活跃: ${node.last_seen}`);
                });
            }
            
            return result;
        } catch (error) {
            this.logger.error('获取节点信息失败:', error.message);
            throw error;
        }
    }
    
    /**
     * 运行所有示例
     */
    async runAllExamples() {
        this.logger.info('开始运行MinIODB Node.js REST客户端示例...');
        
        const examples = [
            { name: '健康检查', fn: () => this.healthCheck() },
            { name: '数据写入', fn: () => this.writeData() },
            { name: '数据查询', fn: () => this.queryData() },
            { name: '触发备份', fn: () => this.triggerBackup() },
            { name: '数据恢复', fn: () => this.recoverData() },
            { name: '获取统计', fn: () => this.getStats() },
            { name: '获取节点', fn: () => this.getNodes() }
        ];
        
        for (const example of examples) {
            try {
                await example.fn();
                // 短暂延迟
                await new Promise(resolve => setTimeout(resolve, 500));
            } catch (error) {
                this.logger.error(`${example.name}失败:`, error.message);
            }
        }
        
        this.logger.info('所有示例运行完成!');
    }
    
    /**
     * 批量写入数据示例
     */
    async batchWriteData() {
        try {
            this.logger.info('=== 批量数据写入 ===');
            
            const promises = [];
            const userIds = ['user123', 'user456', 'user789'];
            
            for (const userId of userIds) {
                const requestData = {
                    id: userId,
                    timestamp: new Date().toISOString(),
                    payload: {
                        user_id: userId,
                        action: Math.random() > 0.5 ? 'login' : 'logout',
                        score: Math.floor(Math.random() * 100),
                        success: Math.random() > 0.1
                    }
                };
                
                promises.push(this.client.post('/v1/data', requestData));
            }
            
            const responses = await Promise.all(promises);
            
            this.logger.info(`批量写入完成，成功写入 ${responses.length} 条记录`);
            responses.forEach((response, index) => {
                this.logger.info(`  记录 ${index + 1}: ${response.data.message}`);
            });
            
            return responses.map(response => response.data);
        } catch (error) {
            this.logger.error('批量数据写入失败:', error.message);
            throw error;
        }
    }
}

// 主函数
async function main() {
    const client = new MinIODBRestClient();
    
    try {
        await client.runAllExamples();
        
        // 可选：运行批量写入示例
        // await client.batchWriteData();
        
    } catch (error) {
        client.logger.error('示例运行失败:', error.message);
        process.exit(1);
    }
}

// 如果直接运行此文件
if (require.main === module) {
    main().catch(console.error);
}

module.exports = MinIODBRestClient; 