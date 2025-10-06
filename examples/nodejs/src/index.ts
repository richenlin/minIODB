/**
 * MinIODB Node.js TypeScript SDK 主入口文件
 */

// 导出配置相关
export {
    MinIODBConfig,
    AuthConfig,
    ConnectionConfig,
    LoggingConfig,
    LogLevel,
    LogFormat,
    DEFAULT_CONFIG,
    ConfigValidator,
    ConfigUtils
} from './config';

// 导出错误相关
export {
    MinIODBError,
    MinIODBConnectionError,
    MinIODBAuthenticationError,
    MinIODBRequestError,
    MinIODBServerError,
    MinIODBTimeoutError,
    ErrorCode,
    ErrorUtils
} from './errors';

// 导出模型相关
export {
    DataRecord,
    TableConfig,
    TableStats,
    TableInfo,
    BackupInfo,
    NodeInfo,
    WriteDataResponse,
    QueryDataResponse,
    UpdateDataResponse,
    DeleteDataResponse,
    StreamWriteResponse,
    StreamQueryResponse,
    CreateTableResponse,
    ListTablesResponse,
    GetTableResponse,
    DeleteTableResponse,
    BackupMetadataResponse,
    RestoreMetadataRequest,
    RestoreMetadataResponse,
    ListBackupsResponse,
    GetMetadataStatusResponse,
    HealthCheckResponse,
    GetStatusResponse,
    GetMetricsResponse,
    GetTokenResponse,
    RefreshTokenResponse,
    RevokeTokenResponse,
    DataRecordFactory,
    TableConfigFactory
} from './models';

// 导出客户端（注意：实际实现需要在生成 gRPC 代码后完成）
export { MinIODBClient } from './client';

// 版本信息
export const VERSION = '1.0.0';
