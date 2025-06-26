#!/bin/bash

# MinIO 初始化脚本
# 用于自动创建存储桶和设置相关配置

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 环境变量默认值
MINIO_ROOT_USER=${MINIO_ROOT_USER:-minioadmin}
MINIO_ROOT_PASSWORD=${MINIO_ROOT_PASSWORD:-minioadmin123}
MINIO_BACKUP_USER=${MINIO_BACKUP_USER:-minioadmin}
MINIO_BACKUP_PASSWORD=${MINIO_BACKUP_PASSWORD:-minioadmin123}
MINIO_BUCKET=${MINIO_BUCKET:-miniodb-data}
MINIO_BACKUP_BUCKET=${MINIO_BACKUP_BUCKET:-miniodb-backup}

# MinIO 服务地址
MINIO_PRIMARY_URL="http://minio:9000"
MINIO_BACKUP_URL="http://minio-backup:9000"

# 最大重试次数和等待时间
MAX_RETRIES=30
RETRY_INTERVAL=5

log_info "Starting MinIO initialization..."
log_info "Primary MinIO: ${MINIO_PRIMARY_URL}"
log_info "Backup MinIO: ${MINIO_BACKUP_URL}"
log_info "Primary bucket: ${MINIO_BUCKET}"
log_info "Backup bucket: ${MINIO_BACKUP_BUCKET}"

# 等待MinIO服务启动
wait_for_minio() {
    local url=$1
    local name=$2
    local retries=0
    
    log_info "Waiting for ${name} to be ready..."
    
    while [ $retries -lt $MAX_RETRIES ]; do
        if curl -f -s "${url}/minio/health/live" > /dev/null 2>&1; then
            log_success "${name} is ready!"
            return 0
        fi
        
        retries=$((retries + 1))
        log_info "Attempt ${retries}/${MAX_RETRIES}: ${name} not ready yet, waiting ${RETRY_INTERVAL}s..."
        sleep $RETRY_INTERVAL
    done
    
    log_error "${name} failed to start after ${MAX_RETRIES} attempts"
    return 1
}

# 配置MinIO客户端别名
configure_aliases() {
    log_info "Configuring MinIO client aliases..."
    
    # 配置主MinIO
    if mc alias set minio "${MINIO_PRIMARY_URL}" "${MINIO_ROOT_USER}" "${MINIO_ROOT_PASSWORD}"; then
        log_success "Primary MinIO alias configured"
    else
        log_error "Failed to configure primary MinIO alias"
        return 1
    fi
    
    # 配置备份MinIO
    if mc alias set minio-backup "${MINIO_BACKUP_URL}" "${MINIO_BACKUP_USER}" "${MINIO_BACKUP_PASSWORD}"; then
        log_success "Backup MinIO alias configured"
    else
        log_error "Failed to configure backup MinIO alias"
        return 1
    fi
}

# 创建存储桶
create_bucket() {
    local alias=$1
    local bucket=$2
    local description=$3
    
    log_info "Creating bucket '${bucket}' on ${description}..."
    
    if mc mb "${alias}/${bucket}" --ignore-existing; then
        log_success "Bucket '${bucket}' created/verified on ${description}"
    else
        log_error "Failed to create bucket '${bucket}' on ${description}"
        return 1
    fi
}

# 设置存储桶策略
set_bucket_policy() {
    local alias=$1
    local bucket=$2
    local policy=$3
    local description=$4
    
    log_info "Setting policy '${policy}' for bucket '${bucket}' on ${description}..."
    
    if mc policy set "${policy}" "${alias}/${bucket}"; then
        log_success "Policy '${policy}' set for bucket '${bucket}' on ${description}"
    else
        log_warning "Failed to set policy for bucket '${bucket}' on ${description}"
        # 不返回错误，因为策略设置失败不应该阻止整个初始化过程
    fi
}

# 设置存储桶版本控制
set_bucket_versioning() {
    local alias=$1
    local bucket=$2
    local description=$3
    
    log_info "Enabling versioning for bucket '${bucket}' on ${description}..."
    
    if mc version enable "${alias}/${bucket}"; then
        log_success "Versioning enabled for bucket '${bucket}' on ${description}"
    else
        log_warning "Failed to enable versioning for bucket '${bucket}' on ${description}"
        # 不返回错误，因为版本控制失败不应该阻止整个初始化过程
    fi
}

# 创建生命周期策略
create_lifecycle_policy() {
    local alias=$1
    local bucket=$2
    local description=$3
    
    log_info "Creating lifecycle policy for bucket '${bucket}' on ${description}..."
    
    # 创建临时的生命周期配置文件
    cat > /tmp/lifecycle.json << EOF
{
    "Rules": [
        {
            "ID": "DeleteOldVersions",
            "Status": "Enabled",
            "Filter": {
                "Prefix": ""
            },
            "NoncurrentVersionExpiration": {
                "NoncurrentDays": 30
            }
        },
        {
            "ID": "DeleteIncompleteUploads",
            "Status": "Enabled",
            "Filter": {
                "Prefix": ""
            },
            "AbortIncompleteMultipartUpload": {
                "DaysAfterInitiation": 7
            }
        }
    ]
}
EOF
    
    if mc ilm import "${alias}/${bucket}" < /tmp/lifecycle.json; then
        log_success "Lifecycle policy created for bucket '${bucket}' on ${description}"
    else
        log_warning "Failed to create lifecycle policy for bucket '${bucket}' on ${description}"
    fi
    
    # 清理临时文件
    rm -f /tmp/lifecycle.json
}

# 验证存储桶状态
verify_bucket() {
    local alias=$1
    local bucket=$2
    local description=$3
    
    log_info "Verifying bucket '${bucket}' on ${description}..."
    
    if mc ls "${alias}/${bucket}" > /dev/null 2>&1; then
        log_success "Bucket '${bucket}' is accessible on ${description}"
        
        # 显示存储桶信息
        log_info "Bucket info for '${bucket}' on ${description}:"
        mc stat "${alias}/${bucket}" || log_warning "Could not get bucket stats"
        
        return 0
    else
        log_error "Bucket '${bucket}' is not accessible on ${description}"
        return 1
    fi
}

# 主函数
main() {
    log_info "=== MinIO Initialization Started ==="
    
    # 等待MinIO服务启动
    if ! wait_for_minio "${MINIO_PRIMARY_URL}" "Primary MinIO"; then
        exit 1
    fi
    
    if ! wait_for_minio "${MINIO_BACKUP_URL}" "Backup MinIO"; then
        exit 1
    fi
    
    # 配置别名
    if ! configure_aliases; then
        exit 1
    fi
    
    # 创建主存储桶
    if ! create_bucket "minio" "${MINIO_BUCKET}" "Primary MinIO"; then
        exit 1
    fi
    
    # 创建备份存储桶
    if ! create_bucket "minio-backup" "${MINIO_BACKUP_BUCKET}" "Backup MinIO"; then
        exit 1
    fi
    
    # 设置存储桶策略（使用download策略而不是public，更安全）
    set_bucket_policy "minio" "${MINIO_BUCKET}" "download" "Primary MinIO"
    set_bucket_policy "minio-backup" "${MINIO_BACKUP_BUCKET}" "download" "Backup MinIO"
    
    # 启用版本控制
    set_bucket_versioning "minio" "${MINIO_BUCKET}" "Primary MinIO"
    set_bucket_versioning "minio-backup" "${MINIO_BACKUP_BUCKET}" "Backup MinIO"
    
    # 创建生命周期策略
    create_lifecycle_policy "minio" "${MINIO_BUCKET}" "Primary MinIO"
    create_lifecycle_policy "minio-backup" "${MINIO_BACKUP_BUCKET}" "Backup MinIO"
    
    # 验证存储桶
    if ! verify_bucket "minio" "${MINIO_BUCKET}" "Primary MinIO"; then
        exit 1
    fi
    
    if ! verify_bucket "minio-backup" "${MINIO_BACKUP_BUCKET}" "Backup MinIO"; then
        exit 1
    fi
    
    log_success "=== MinIO Initialization Completed Successfully ==="
    
    # 显示总结信息
    echo ""
    log_info "=== Initialization Summary ==="
    log_info "Primary MinIO URL: ${MINIO_PRIMARY_URL}"
    log_info "Primary Bucket: ${MINIO_BUCKET}"
    log_info "Backup MinIO URL: ${MINIO_BACKUP_URL}"
    log_info "Backup Bucket: ${MINIO_BACKUP_BUCKET}"
    log_info "Versioning: Enabled"
    log_info "Lifecycle Policy: Enabled"
    echo ""
    
    # 保持容器运行一段时间以便查看日志
    log_info "Initialization complete. Container will exit in 10 seconds..."
    sleep 10
}

# 错误处理
trap 'log_error "Script interrupted"; exit 1' INT TERM

# 执行主函数
main "$@" 