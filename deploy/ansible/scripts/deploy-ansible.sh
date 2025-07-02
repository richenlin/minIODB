#!/bin/bash

# MinIODB Ansible 一键部署脚本
# 支持现有服务模式和自动部署模式

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 日志函数
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

# 显示帮助信息
show_help() {
    cat << EOF
MinIODB Ansible 一键部署脚本

用法:
    $0 [选项] <部署模式>

部署模式:
    existing    使用现有Redis、MinIO服务
    auto        自动部署Redis、MinIO容器服务

选项:
    -h, --help              显示帮助信息
    -v, --verbose           详细输出
    -i, --inventory FILE    指定清单文件
    -t, --tags TAGS         指定标签
    -l, --limit HOSTS       限制执行的主机
    --check                 检查模式（不执行实际操作）
    --diff                  显示差异
    --ask-vault-pass        询问vault密码
    --vault-password-file   指定vault密码文件

示例:
    $0 auto                                    # 自动部署模式
    $0 existing                                # 现有服务模式
    $0 -v auto                                 # 详细输出的自动部署
    $0 -i custom-inventory.yml auto            # 使用自定义清单
    $0 -t docker,miniodb auto                  # 只执行指定标签
    $0 -l node1,node2 auto                     # 只在指定主机执行

EOF
}

# 检查依赖
check_dependencies() {
    log_info "检查部署依赖..."
    
    if ! command -v ansible &> /dev/null; then
        log_error "Ansible 未安装"
        log_info "请参考README.md安装Ansible"
        exit 1
    fi
    
    if ! command -v ansible-playbook &> /dev/null; then
        log_error "ansible-playbook 未安装"
        exit 1
    fi
    
    # 检查Python依赖
    if ! python3 -c "import docker" &> /dev/null; then
        log_warning "Python docker库未安装，尝试自动安装..."
        pip3 install docker || {
            log_error "无法安装Python docker库"
            exit 1
        }
    fi
    
    log_success "依赖检查通过"
}

# 验证清单文件
validate_inventory() {
    local inventory_file=$1
    
    log_info "验证清单文件: $inventory_file"
    
    if [[ ! -f "$inventory_file" ]]; then
        log_error "清单文件不存在: $inventory_file"
        exit 1
    fi
    
    # 检查清单语法
    if ! ansible-inventory -i "$inventory_file" --list &> /dev/null; then
        log_error "清单文件语法错误"
        exit 1
    fi
    
    # 检查主机连通性
    log_info "检查主机连通性..."
    if ! ansible -i "$inventory_file" all -m ping $ANSIBLE_EXTRA_ARGS; then
        log_error "主机连通性检查失败"
        log_info "请检查SSH配置和主机可达性"
        exit 1
    fi
    
    log_success "清单文件验证通过"
}

# 创建vault密码文件
setup_vault() {
    if [[ ! -f ".vault_pass" && "$ASK_VAULT_PASS" != "true" ]]; then
        log_warning "未找到vault密码文件，将创建示例文件"
        echo "change-this-vault-password" > .vault_pass
        chmod 600 .vault_pass
        log_warning "请编辑.vault_pass文件设置实际的vault密码"
    fi
}

# 创建示例vault文件
create_sample_vault() {
    local vault_file="group_vars/all/vault.yml"
    
    if [[ ! -f "$vault_file" ]]; then
        log_info "创建示例vault文件..."
        mkdir -p "$(dirname "$vault_file")"
        
        cat > "$vault_file" << EOF
---
# 示例vault文件 - 请使用 ansible-vault encrypt 加密此文件
vault_jwt_secret: "super-secret-jwt-key-change-this"
vault_redis_password: "redis-password-change-this"
vault_minio_access_key: "minioadmin"
vault_minio_secret_key: "minioadmin123"
vault_minio_backup_access_key: "minioadmin"
vault_minio_backup_secret_key: "minioadmin123"
EOF
        
        log_warning "请编辑 $vault_file 并使用 ansible-vault encrypt 加密"
        log_warning "命令: ansible-vault encrypt $vault_file"
    fi
}

# 执行部署前检查
pre_deploy_check() {
    local inventory_file=$1
    
    log_info "执行部署前检查..."
    
    # 检查磁盘空间
    log_info "检查磁盘空间..."
    ansible -i "$inventory_file" all -m shell -a "df -h /" $ANSIBLE_EXTRA_ARGS
    
    # 检查内存
    log_info "检查内存..."
    ansible -i "$inventory_file" all -m shell -a "free -h" $ANSIBLE_EXTRA_ARGS
    
    # 检查Docker状态（如果已安装）
    log_info "检查Docker状态..."
    ansible -i "$inventory_file" all -m shell -a "docker --version || echo 'Docker not installed'" $ANSIBLE_EXTRA_ARGS
    
    log_success "部署前检查完成"
}

# 执行部署
deploy() {
    local inventory_file=$1
    
    log_info "开始执行部署..."
    
    # 构建ansible-playbook命令
    local cmd="ansible-playbook -i $inventory_file site.yml"
    
    # 添加额外参数
    if [[ -n "$ANSIBLE_TAGS" ]]; then
        cmd="$cmd --tags $ANSIBLE_TAGS"
    fi
    
    if [[ -n "$ANSIBLE_LIMIT" ]]; then
        cmd="$cmd --limit $ANSIBLE_LIMIT"
    fi
    
    if [[ "$CHECK_MODE" == "true" ]]; then
        cmd="$cmd --check"
    fi
    
    if [[ "$DIFF_MODE" == "true" ]]; then
        cmd="$cmd --diff"
    fi
    
    if [[ "$ASK_VAULT_PASS" == "true" ]]; then
        cmd="$cmd --ask-vault-pass"
    elif [[ -n "$VAULT_PASSWORD_FILE" ]]; then
        cmd="$cmd --vault-password-file $VAULT_PASSWORD_FILE"
    fi
    
    cmd="$cmd $ANSIBLE_EXTRA_ARGS"
    
    log_info "执行命令: $cmd"
    
    # 执行部署
    if eval "$cmd"; then
        log_success "部署执行完成"
    else
        log_error "部署执行失败"
        exit 1
    fi
}

# 执行部署后检查
post_deploy_check() {
    local inventory_file=$1
    
    log_info "执行部署后检查..."
    
    # 等待服务启动
    sleep 30
    
    # 检查容器状态
    log_info "检查容器状态..."
    ansible -i "$inventory_file" all -m shell -a "docker ps" $ANSIBLE_EXTRA_ARGS
    
    # 健康检查
    log_info "执行健康检查..."
    ansible-playbook -i "$inventory_file" site.yml --tags cluster-check $ANSIBLE_EXTRA_ARGS
    
    log_success "部署后检查完成"
}

# 显示部署结果
show_deployment_info() {
    local inventory_file=$1
    
    log_success "MinIODB 集群部署完成!"
    echo
    echo "集群信息:"
    ansible -i "$inventory_file" miniodb_nodes -m setup -a "filter=ansible_default_ipv4" $ANSIBLE_EXTRA_ARGS | \
    grep -E "(ansible_default_ipv4|address)" | \
    while read line; do
        if [[ "$line" =~ "address" ]]; then
            ip=$(echo "$line" | sed 's/.*"\(.*\)".*/\1/')
            echo "  节点: $ip"
            echo "    - REST API: http://$ip:8081"
            echo "    - gRPC API: $ip:8080"
            echo "    - 监控指标: http://$ip:9090/metrics"
        fi
    done
    
    echo
    echo "常用命令:"
    echo "  查看集群状态: ansible -i $inventory_file all -m shell -a 'docker ps'"
    echo "  查看日志: ansible -i $inventory_file all -m shell -a 'docker logs miniodb-app'"
    echo "  重启服务: ansible -i $inventory_file all -m shell -a 'docker restart miniodb-app'"
    echo "  健康检查: ./scripts/health-check.sh"
}

# 主函数
main() {
    # 默认值
    DEPLOY_MODE=""
    VERBOSE=false
    INVENTORY_FILE=""
    ANSIBLE_TAGS=""
    ANSIBLE_LIMIT=""
    CHECK_MODE=false
    DIFF_MODE=false
    ASK_VAULT_PASS=false
    VAULT_PASSWORD_FILE=""
    ANSIBLE_EXTRA_ARGS=""
    
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -v|--verbose)
                VERBOSE=true
                ANSIBLE_EXTRA_ARGS="$ANSIBLE_EXTRA_ARGS -v"
                shift
                ;;
            -i|--inventory)
                INVENTORY_FILE="$2"
                shift 2
                ;;
            -t|--tags)
                ANSIBLE_TAGS="$2"
                shift 2
                ;;
            -l|--limit)
                ANSIBLE_LIMIT="$2"
                shift 2
                ;;
            --check)
                CHECK_MODE=true
                shift
                ;;
            --diff)
                DIFF_MODE=true
                shift
                ;;
            --ask-vault-pass)
                ASK_VAULT_PASS=true
                shift
                ;;
            --vault-password-file)
                VAULT_PASSWORD_FILE="$2"
                shift 2
                ;;
            existing|auto)
                DEPLOY_MODE="$1"
                shift
                ;;
            *)
                log_error "未知参数: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    # 检查部署模式
    if [[ -z "$DEPLOY_MODE" ]]; then
        log_error "请指定部署模式: existing 或 auto"
        show_help
        exit 1
    fi
    
    # 设置默认清单文件
    if [[ -z "$INVENTORY_FILE" ]]; then
        case $DEPLOY_MODE in
            existing)
                INVENTORY_FILE="inventory/existing-services.yml"
                ;;
            auto)
                INVENTORY_FILE="inventory/auto-deploy.yml"
                ;;
        esac
    fi
    
    # 显示配置信息
    log_info "部署配置:"
    log_info "  部署模式: $DEPLOY_MODE"
    log_info "  清单文件: $INVENTORY_FILE"
    log_info "  详细输出: $VERBOSE"
    log_info "  检查模式: $CHECK_MODE"
    [[ -n "$ANSIBLE_TAGS" ]] && log_info "  标签: $ANSIBLE_TAGS"
    [[ -n "$ANSIBLE_LIMIT" ]] && log_info "  限制主机: $ANSIBLE_LIMIT"
    echo
    
    # 切换到ansible目录
    cd "$(dirname "$0")/.."
    
    # 检查依赖
    check_dependencies
    
    # 设置vault
    setup_vault
    
    # 创建示例vault文件
    create_sample_vault
    
    # 验证清单文件
    validate_inventory "$INVENTORY_FILE"
    
    # 执行部署前检查
    if [[ "$CHECK_MODE" != "true" ]]; then
        pre_deploy_check "$INVENTORY_FILE"
    fi
    
    # 执行部署
    deploy "$INVENTORY_FILE"
    
    # 执行部署后检查
    if [[ "$CHECK_MODE" != "true" ]]; then
        post_deploy_check "$INVENTORY_FILE"
        show_deployment_info "$INVENTORY_FILE"
    fi
    
    log_success "部署完成!"
}

# 脚本入口
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi 