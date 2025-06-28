#!/bin/bash

# MinIO 初始化脚本 - 简化版本
# 用于自动创建存储桶

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

log_info "Starting MinIO initialization..."
log_info "Primary MinIO: ${MINIO_PRIMARY_URL}"
log_info "Backup MinIO: ${MINIO_BACKUP_URL}"
log_info "Primary bucket: ${MINIO_BUCKET}"
log_info "Backup bucket: ${MINIO_BACKUP_BUCKET}"

# 等待并设置MinIO服务
setup_minio() {
    local name=$1
    local url=$2
    local user=$3
    local password=$4
    local alias=$5
    
    log_info "Setting up ${name}..."
    
    # 配置MinIO客户端别名 (初始配置，可能失败)
    log_info "Configuring ${name} alias..."
    mc alias set "${alias}" "${url}" "${user}" "${password}" > /dev/null 2>&1 || true
    
    # 等待服务就绪 - 使用mc ping检查连接性
    local retries=0
    local max_retries=30
    
    while [ $retries -lt $max_retries ]; do
        # 使用mc ping检查服务状态
        if mc ping "${alias}" -c 1 > /dev/null 2>&1; then
            log_success "${name} is ready!"
            break
        fi
        
        retries=$((retries + 1))
        log_info "Waiting for ${name}... (${retries}/${max_retries})"
        sleep 5
        
        # 重新配置别名
        mc alias set "${alias}" "${url}" "${user}" "${password}" > /dev/null 2>&1 || true
        
        if [ $retries -eq $max_retries ]; then
            log_error "${name} failed to become ready"
            return 1
        fi
    done
    
    # 最终验证别名配置 - 使用mc admin info来验证连接
    if mc admin info "${alias}" > /dev/null 2>&1; then
        log_success "${name} alias configured successfully"
    else
        log_error "Failed to configure ${name} alias"
        return 1
    fi
    
    return 0
}

# 创建存储桶
create_bucket() {
    local alias=$1
    local bucket=$2
    local description=$3
    
    log_info "Creating bucket '${bucket}' on ${description}..."
    
    # 尝试创建存储桶
    if mc mb "${alias}/${bucket}" --ignore-existing > /dev/null 2>&1; then
        log_success "Bucket '${bucket}' created/verified on ${description}"
        
        # 验证存储桶是否可访问
        if mc ls "${alias}/${bucket}" > /dev/null 2>&1; then
            log_success "Bucket '${bucket}' is accessible on ${description}"
        else
            log_warning "Bucket '${bucket}' created but not accessible on ${description}"
        fi
        
        return 0
    else
        log_error "Failed to create bucket '${bucket}' on ${description}"
        return 1
    fi
}

# 主函数
main() {
    log_info "=== MinIO Initialization Started ==="
    
    # 设置主MinIO
    if ! setup_minio "Primary MinIO" "${MINIO_PRIMARY_URL}" "${MINIO_ROOT_USER}" "${MINIO_ROOT_PASSWORD}" "minio"; then
        log_error "Failed to setup Primary MinIO"
        exit 1
    fi
    
    # 设置备份MinIO
    if ! setup_minio "Backup MinIO" "${MINIO_BACKUP_URL}" "${MINIO_BACKUP_USER}" "${MINIO_BACKUP_PASSWORD}" "minio-backup"; then
        log_error "Failed to setup Backup MinIO"
        exit 1
    fi
    
    # 创建主存储桶
    if ! create_bucket "minio" "${MINIO_BUCKET}" "Primary MinIO"; then
        log_error "Failed to create primary bucket"
        exit 1
    fi
    
    # 创建元数据存储桶（在主MinIO上）
    if ! create_bucket "minio" "miniodb-metadata" "Primary MinIO (metadata)"; then
        log_error "Failed to create metadata bucket"
        exit 1
    fi
    
    # 创建备份存储桶
    if ! create_bucket "minio-backup" "${MINIO_BACKUP_BUCKET}" "Backup MinIO"; then
        log_error "Failed to create backup bucket"
        exit 1
    fi
    
    log_success "=== MinIO Initialization Completed Successfully ==="
    
    # 显示最终状态
    log_info "Final bucket status:"
    mc ls minio/ 2>/dev/null || log_warning "Could not list primary buckets"
    mc ls minio-backup/ 2>/dev/null || log_warning "Could not list backup buckets"
}

# 执行主函数
main "$@" 