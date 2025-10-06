/**
 * MinIODB Node.js TypeScript SDK 数据模型模块
 * 
 * 定义了与 MinIODB 交互所需的数据模型和类型。
 */

/**
 * 数据记录接口
 */
export interface DataRecord {
    /** 记录唯一标识符 */
    id: string;
    /** 记录时间戳 */
    timestamp: Date;
    /** 数据负载 */
    payload: Record<string, any>;
}

/**
 * 表配置接口
 */
export interface TableConfig {
    /** 缓冲区大小，默认 1000 */
    bufferSize?: number;
    /** 刷新间隔（秒），默认 30 */
    flushIntervalSeconds?: number;
    /** 数据保留天数，默认 365 */
    retentionDays?: number;
    /** 是否启用备份，默认 false */
    backupEnabled?: boolean;
    /** 扩展属性 */
    properties?: Record<string, string>;
}

/**
 * 表统计信息接口
 */
export interface TableStats {
    /** 记录数量 */
    recordCount: number;
    /** 文件数量 */
    fileCount: number;
    /** 大小（字节） */
    sizeBytes: number;
    /** 最老记录时间 */
    oldestRecord?: Date;
    /** 最新记录时间 */
    newestRecord?: Date;
}

/**
 * 表信息接口
 */
export interface TableInfo {
    /** 表名 */
    name: string;
    /** 表配置 */
    config: TableConfig;
    /** 创建时间 */
    createdAt: Date;
    /** 最后写入时间 */
    lastWrite?: Date;
    /** 表状态 */
    status: string;
    /** 表统计信息 */
    stats?: TableStats;
}

/**
 * 备份信息接口
 */
export interface BackupInfo {
    /** 对象名称 */
    objectName: string;
    /** 节点ID */
    nodeId: string;
    /** 备份时间 */
    timestamp: Date;
    /** 文件大小 */
    size: number;
    /** 最后修改时间 */
    lastModified: Date;
}

/**
 * 节点信息接口
 */
export interface NodeInfo {
    /** 节点ID */
    id: string;
    /** 节点状态 */
    status: string;
    /** 节点类型 */
    type: string;
    /** 节点地址 */
    address: string;
    /** 最后见到时间 */
    lastSeen: number;
}

// ============================================================================
// 响应模型接口
// ============================================================================

/**
 * 写入数据响应接口
 */
export interface WriteDataResponse {
    /** 是否成功 */
    success: boolean;
    /** 响应消息 */
    message: string;
    /** 处理节点ID */
    nodeId?: string;
}

/**
 * 查询数据响应接口
 */
export interface QueryDataResponse {
    /** JSON 格式的查询结果 */
    resultJson: string;
    /** 是否有更多数据 */
    hasMore: boolean;
    /** 下一页游标 */
    nextCursor?: string;
}

/**
 * 更新数据响应接口
 */
export interface UpdateDataResponse {
    /** 是否成功 */
    success: boolean;
    /** 响应消息 */
    message: string;
    /** 处理节点ID */
    nodeId?: string;
}

/**
 * 删除数据响应接口
 */
export interface DeleteDataResponse {
    /** 是否成功 */
    success: boolean;
    /** 响应消息 */
    message: string;
    /** 删除的记录数 */
    deletedCount: number;
}

/**
 * 流式写入响应接口
 */
export interface StreamWriteResponse {
    /** 是否成功 */
    success: boolean;
    /** 写入记录数 */
    recordsCount: number;
    /** 错误信息列表 */
    errors: string[];
}

/**
 * 流式查询响应接口
 */
export interface StreamQueryResponse {
    /** 数据记录列表 */
    records: DataRecord[];
    /** 是否有更多数据 */
    hasMore: boolean;
    /** 当前游标 */
    cursor?: string;
}

/**
 * 创建表响应接口
 */
export interface CreateTableResponse {
    /** 是否成功 */
    success: boolean;
    /** 响应消息 */
    message: string;
}

/**
 * 列出表响应接口
 */
export interface ListTablesResponse {
    /** 表信息列表 */
    tables: TableInfo[];
    /** 总数 */
    total: number;
}

/**
 * 获取表响应接口
 */
export interface GetTableResponse {
    /** 表信息 */
    tableInfo: TableInfo;
}

/**
 * 删除表响应接口
 */
export interface DeleteTableResponse {
    /** 是否成功 */
    success: boolean;
    /** 响应消息 */
    message: string;
    /** 删除的文件数 */
    filesDeleted: number;
}

/**
 * 备份元数据响应接口
 */
export interface BackupMetadataResponse {
    /** 是否成功 */
    success: boolean;
    /** 响应消息 */
    message: string;
    /** 备份ID */
    backupId: string;
    /** 备份时间 */
    timestamp: Date;
}

/**
 * 恢复元数据请求接口
 */
export interface RestoreMetadataRequest {
    /** 备份文件路径 */
    backupFile: string;
    /** 是否从最新备份恢复 */
    fromLatest: boolean;
    /** 是否干运行 */
    dryRun: boolean;
    /** 是否覆盖现有数据 */
    overwrite: boolean;
    /** 是否验证数据 */
    validate: boolean;
    /** 是否并行执行 */
    parallel: boolean;
    /** 过滤选项 */
    filters?: Record<string, string>;
    /** 键模式过滤 */
    keyPatterns?: string[];
}

/**
 * 恢复元数据响应接口
 */
export interface RestoreMetadataResponse {
    /** 是否成功 */
    success: boolean;
    /** 响应消息 */
    message: string;
    /** 使用的备份文件 */
    backupFile: string;
    /** 总条目数 */
    entriesTotal: number;
    /** 成功条目数 */
    entriesOk: number;
    /** 跳过条目数 */
    entriesSkipped: number;
    /** 错误条目数 */
    entriesError: number;
    /** 执行时长 */
    duration: string;
    /** 错误信息列表 */
    errors: string[];
    /** 详细信息 */
    details?: Record<string, string>;
}

/**
 * 列出备份响应接口
 */
export interface ListBackupsResponse {
    /** 备份信息列表 */
    backups: BackupInfo[];
    /** 总数 */
    total: number;
}

/**
 * 获取元数据状态响应接口
 */
export interface GetMetadataStatusResponse {
    /** 节点ID */
    nodeId: string;
    /** 备份状态 */
    backupStatus: Record<string, string>;
    /** 最后备份时间 */
    lastBackup?: Date;
    /** 下次备份时间 */
    nextBackup?: Date;
    /** 健康状态 */
    healthStatus: string;
}

/**
 * 健康检查响应接口
 */
export interface HealthCheckResponse {
    /** 整体状态 */
    status: string;
    /** 检查时间 */
    timestamp: Date;
    /** 服务版本 */
    version: string;
    /** 各组件状态详情 */
    details?: Record<string, string>;
}

/**
 * 获取状态响应接口
 */
export interface GetStatusResponse {
    /** 状态时间戳 */
    timestamp: Date;
    /** 缓冲区统计 */
    bufferStats: Record<string, number>;
    /** Redis 统计 */
    redisStats: Record<string, number>;
    /** MinIO 统计 */
    minioStats: Record<string, number>;
    /** 节点信息列表 */
    nodes: NodeInfo[];
    /** 总节点数 */
    totalNodes: number;
}

/**
 * 获取指标响应接口
 */
export interface GetMetricsResponse {
    /** 指标时间戳 */
    timestamp: Date;
    /** 性能指标 */
    performanceMetrics: Record<string, number>;
    /** 资源使用情况 */
    resourceUsage: Record<string, number>;
    /** 系统信息 */
    systemInfo: Record<string, string>;
}

/**
 * 获取令牌响应接口
 */
export interface GetTokenResponse {
    /** 访问令牌 */
    accessToken: string;
    /** 刷新令牌 */
    refreshToken: string;
    /** 过期时间（秒） */
    expiresIn: number;
    /** 令牌类型 */
    tokenType: string;
}

/**
 * 刷新令牌响应接口
 */
export interface RefreshTokenResponse {
    /** 新的访问令牌 */
    accessToken: string;
    /** 新的刷新令牌 */
    refreshToken: string;
    /** 过期时间（秒） */
    expiresIn: number;
    /** 令牌类型 */
    tokenType: string;
}

/**
 * 撤销令牌响应接口
 */
export interface RevokeTokenResponse {
    /** 是否成功 */
    success: boolean;
    /** 响应消息 */
    message: string;
}

// ============================================================================
// 工具类型和辅助函数
// ============================================================================

/**
 * 数据记录工厂类
 */
export class DataRecordFactory {
    /**
     * 创建数据记录
     */
    static create(
        payload: Record<string, any>,
        id?: string,
        timestamp?: Date
    ): DataRecord {
        return {
            id: id || this.generateId(),
            timestamp: timestamp || new Date(),
            payload
        };
    }

    /**
     * 生成唯一ID
     */
    private static generateId(): string {
        return `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
    }

    /**
     * 验证数据记录
     */
    static validate(record: DataRecord): void {
        if (!record.id?.trim()) {
            throw new Error('记录 ID 不能为空');
        }
        if (!record.timestamp) {
            throw new Error('时间戳不能为空');
        }
        if (!record.payload || typeof record.payload !== 'object') {
            throw new Error('数据负载必须是对象类型');
        }
    }
}

/**
 * 表配置工厂类
 */
export class TableConfigFactory {
    /**
     * 创建默认表配置
     */
    static createDefault(): TableConfig {
        return {
            bufferSize: 1000,
            flushIntervalSeconds: 30,
            retentionDays: 365,
            backupEnabled: false
        };
    }

    /**
     * 验证表配置
     */
    static validate(config: TableConfig): void {
        if (config.bufferSize !== undefined && config.bufferSize <= 0) {
            throw new Error('缓冲区大小必须大于 0');
        }
        if (config.flushIntervalSeconds !== undefined && config.flushIntervalSeconds <= 0) {
            throw new Error('刷新间隔必须大于 0');
        }
        if (config.retentionDays !== undefined && config.retentionDays <= 0) {
            throw new Error('保留天数必须大于 0');
        }
    }
}
