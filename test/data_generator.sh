#!/bin/bash

# MinIODB 测试数据生成器
# 为性能测试和验证生成大量样本数据

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 配置
MINIODB_REST_API="http://localhost:8081"
DEFAULT_RECORDS=1000
DEFAULT_TABLES=5
DEFAULT_BATCH_SIZE=100

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

log_header() {
    echo -e "${PURPLE}$(printf '=%.0s' {1..80})${NC}"
    echo -e "${PURPLE}$1${NC}"
    echo -e "${PURPLE}$(printf '=%.0s' {1..80})${NC}"
}

# 显示帮助信息
show_help() {
    cat << EOF
MinIODB 测试数据生成器

用法: $0 [选项]

选项:
  --records NUM        生成记录数 (默认: 1000)
  --tables NUM         创建表数 (默认: 5)
  --batch-size NUM     批处理大小 (默认: 100)
  --endpoint URL       MinIODB API端点
  --clean              清理现有测试数据
  --verify             验证生成的数据
  -h, --help           显示帮助信息

示例:
  $0                                    # 生成默认测试数据
  $0 --records 5000 --tables 10        # 生成大量数据
  $0 --clean                           # 清理测试数据
  $0 --verify                          # 验证数据完整性

EOF
}

# 检查服务可用性
check_service() {
    if ! curl -sf "$MINIODB_REST_API/v1/health" >/dev/null 2>&1; then
        log_error "MinIODB服务不可用，请确保服务正在运行"
        exit 1
    fi
    log_success "服务连接正常"
}

# 生成随机数据
generate_random_data() {
    local record_type="$1"
    local id="$2"
    
    case $record_type in
        "user")
            echo "{\"id\":\"$id\",\"name\":\"用户_$id\",\"age\":$((20 + RANDOM % 60)),\"city\":\"城市_$((RANDOM % 100))\",\"email\":\"user$id@test.com\",\"created_at\":\"$(date -Iseconds)\",\"score\":$((RANDOM % 1000))}"
            ;;
        "product")
            echo "{\"id\":\"$id\",\"name\":\"产品_$id\",\"price\":$((RANDOM % 10000 + 100)).99,\"category\":\"分类_$((RANDOM % 20))\",\"stock\":$((RANDOM % 1000)),\"description\":\"这是产品$id的描述\",\"created_at\":\"$(date -Iseconds)\"}"
            ;;
        "order")
            echo "{\"id\":\"$id\",\"user_id\":\"user_$((RANDOM % 1000))\",\"product_id\":\"product_$((RANDOM % 500))\",\"quantity\":$((RANDOM % 10 + 1)),\"total\":$((RANDOM % 10000 + 50)).99,\"status\":\"status_$((RANDOM % 5))\",\"created_at\":\"$(date -Iseconds)\"}"
            ;;
        "log")
            local levels=("INFO" "WARN" "ERROR" "DEBUG")
            local level=${levels[$((RANDOM % 4))]}
            echo "{\"id\":\"$id\",\"level\":\"$level\",\"message\":\"这是一条$level级别的日志消息 $id\",\"service\":\"service_$((RANDOM % 10))\",\"timestamp\":\"$(date -Iseconds)\",\"metadata\":{\"thread\":\"thread_$((RANDOM % 20))\",\"session\":\"session_$((RANDOM % 100))\"}}"
            ;;
        *)
            echo "{\"id\":\"$id\",\"data\":\"通用数据_$id\",\"timestamp\":\"$(date -Iseconds)\",\"random_value\":$RANDOM}"
            ;;
    esac
}

# 创建表
create_table() {
    local table_name="$1"
    local buffer_size="$2"
    
    log_info "创建表: $table_name"
    
    local payload="{\"table_name\":\"$table_name\",\"config\":{\"buffer_size\":$buffer_size,\"flush_interval_seconds\":30,\"retention_days\":7},\"if_not_exists\":true}"
    
    if curl -sf -X POST "$MINIODB_REST_API/v1/tables" \
        -H "Content-Type: application/json" \
        -d "$payload" >/dev/null 2>&1; then
        log_success "表 $table_name 创建成功"
        return 0
    else
        log_error "表 $table_name 创建失败"
        return 1
    fi
}

# 批量写入数据
batch_write_data() {
    local table_name="$1"
    local data_type="$2"
    local start_id="$3"
    local count="$4"
    
    log_info "向表 $table_name 写入 $count 条 $data_type 数据..."
    
    local success_count=0
    local error_count=0
    
    for ((i=0; i<count; i++)); do
        local record_id="${data_type}_$((start_id + i))"
        local payload_data=$(generate_random_data "$data_type" "$record_id")
        
        local payload="{\"table\":\"$table_name\",\"id\":\"$record_id\",\"timestamp\":$(date +%s),\"payload\":$payload_data}"
        
        if curl -sf -X POST "$MINIODB_REST_API/v1/data" \
            -H "Content-Type: application/json" \
            -d "$payload" >/dev/null 2>&1; then
            success_count=$((success_count + 1))
        else
            error_count=$((error_count + 1))
        fi
        
        # 显示进度
        if [ $((i % 100)) -eq 0 ] && [ $i -gt 0 ]; then
            local progress=$((i * 100 / count))
            echo -ne "\r进度: ${progress}% ($i/$count)"
        fi
    done
    
    echo ""
    log_success "写入完成: 成功 $success_count, 失败 $error_count"
}

# 生成测试数据
generate_test_data() {
    local num_records="$1"
    local num_tables="$2"
    local batch_size="$3"
    
    log_header "生成测试数据"
    log_info "参数: $num_records 记录, $num_tables 表, 批大小 $batch_size"
    
    # 数据类型配置
    local data_types=("user" "product" "order" "log" "generic")
    local table_configs=(
        "test_users:user:2000"
        "test_products:product:1000"
        "test_orders:order:5000"
        "test_logs:log:10000"
        "test_generic:generic:1000"
    )
    
    local records_per_table=$((num_records / num_tables))
    
    for ((t=0; t<num_tables && t<${#table_configs[@]}; t++)); do
        IFS=':' read -r table_name data_type buffer_size <<< "${table_configs[$t]}"
        
        # 创建表
        create_table "$table_name" "$buffer_size"
        
        # 分批写入数据
        local total_batches=$(( (records_per_table + batch_size - 1) / batch_size ))
        
        for ((b=0; b<total_batches; b++)); do
            local start_id=$((b * batch_size))
            local current_batch_size=$batch_size
            
            # 最后一批可能不足batch_size
            if [ $((start_id + batch_size)) -gt $records_per_table ]; then
                current_batch_size=$((records_per_table - start_id))
            fi
            
            if [ $current_batch_size -gt 0 ]; then
                batch_write_data "$table_name" "$data_type" "$start_id" "$current_batch_size"
            fi
        done
    done
    
    log_success "测试数据生成完成"
}

# 验证数据
verify_data() {
    log_header "验证测试数据"
    
    # 获取表列表
    local tables_response=$(curl -sf -X GET "$MINIODB_REST_API/v1/tables" 2>/dev/null)
    
    if [ $? -ne 0 ]; then
        log_error "无法获取表列表"
        return 1
    fi
    
    # 解析表名并查询数据
    local table_names=$(echo "$tables_response" | jq -r '.tables[]?.name // empty' 2>/dev/null | grep '^test_')
    
    if [ -z "$table_names" ]; then
        log_warning "未找到测试表"
        return 0
    fi
    
    local total_records=0
    
    while IFS= read -r table_name; do
        log_info "验证表: $table_name"
        
        local query="{\"sql\":\"SELECT COUNT(*) as count FROM $table_name\"}"
        local response=$(curl -sf -X POST "$MINIODB_REST_API/v1/query" \
            -H "Content-Type: application/json" \
            -d "$query" 2>/dev/null)
        
        if [ $? -eq 0 ]; then
            local count=$(echo "$response" | jq -r '.result_json' 2>/dev/null | head -1 | jq -r '.count // 0' 2>/dev/null || echo "0")
            echo "  记录数: $count"
            total_records=$((total_records + count))
        else
            log_warning "  查询失败"
        fi
    done <<< "$table_names"
    
    log_success "总记录数: $total_records"
}

# 清理测试数据
clean_test_data() {
    log_header "清理测试数据"
    
    # 获取表列表
    local tables_response=$(curl -sf -X GET "$MINIODB_REST_API/v1/tables" 2>/dev/null)
    
    if [ $? -ne 0 ]; then
        log_error "无法获取表列表"
        return 1
    fi
    
    # 删除测试表
    local table_names=$(echo "$tables_response" | jq -r '.tables[]?.name // empty' 2>/dev/null | grep '^test_')
    
    if [ -z "$table_names" ]; then
        log_info "未找到需要清理的测试表"
        return 0
    fi
    
    while IFS= read -r table_name; do
        log_info "删除表: $table_name"
        
        if curl -sf -X DELETE "$MINIODB_REST_API/v1/tables/$table_name?if_exists=true" >/dev/null 2>&1; then
            log_success "表 $table_name 删除成功"
        else
            log_warning "表 $table_name 删除失败"
        fi
    done <<< "$table_names"
    
    log_success "测试数据清理完成"
}

# 主函数
main() {
    local num_records=$DEFAULT_RECORDS
    local num_tables=$DEFAULT_TABLES
    local batch_size=$DEFAULT_BATCH_SIZE
    local action="generate"
    
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            --records)
                num_records="$2"
                shift 2
                ;;
            --tables)
                num_tables="$2"
                shift 2
                ;;
            --batch-size)
                batch_size="$2"
                shift 2
                ;;
            --endpoint)
                MINIODB_REST_API="$2"
                shift 2
                ;;
            --clean)
                action="clean"
                shift
                ;;
            --verify)
                action="verify"
                shift
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                log_error "未知选项: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    log_header "MinIODB 测试数据生成器"
    
    # 检查服务
    check_service
    
    # 执行操作
    case $action in
        "generate")
            generate_test_data "$num_records" "$num_tables" "$batch_size"
            ;;
        "clean")
            clean_test_data
            ;;
        "verify")
            verify_data
            ;;
    esac
}

# 执行主函数
main "$@" 