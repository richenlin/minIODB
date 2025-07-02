#!/bin/bash

# MinIODB 性能监控脚本
# 实时监控系统资源和数据库性能指标

set -e

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
MONITOR_INTERVAL=${1:-5}  # 监控间隔，默认5秒
RESULTS_DIR=${2:-./test_results}
LOG_FILE="$RESULTS_DIR/performance_monitor.log"
API_BASE_URL="http://localhost:8080"

# 创建结果目录
mkdir -p "$RESULTS_DIR"

echo -e "${GREEN}MinIODB 性能监控启动${NC}"
echo -e "${YELLOW}监控间隔: ${MONITOR_INTERVAL}秒${NC}"
echo -e "${YELLOW}日志文件: ${LOG_FILE}${NC}"
echo -e "${YELLOW}按 Ctrl+C 停止监控${NC}"
echo ""

# 初始化日志文件
cat > "$LOG_FILE" << EOF
# MinIODB Performance Monitor Log
# Started at: $(date)
# Monitor Interval: ${MONITOR_INTERVAL}s
# Format: timestamp,cpu_percent,memory_used_gb,memory_percent,disk_used_percent,network_rx_mb,network_tx_mb,api_response_time_ms,qps,active_connections
EOF

# 获取网络接口名称
NETWORK_INTERFACE=$(ip route | grep default | awk '{print $5}' | head -1)

# 监控函数
monitor_system() {
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    
    # CPU使用率
    local cpu_percent=$(top -bn1 | grep "Cpu(s)" | awk '{print $2}' | awk -F'%' '{print $1}')
    
    # 内存使用情况
    local memory_info=$(free -m | grep Mem)
    local memory_total=$(echo $memory_info | awk '{print $2}')
    local memory_used=$(echo $memory_info | awk '{print $3}')
    local memory_percent=$(echo "scale=2; $memory_used * 100 / $memory_total" | bc)
    local memory_used_gb=$(echo "scale=2; $memory_used / 1024" | bc)
    
    # 磁盘使用率
    local disk_percent=$(df -h / | awk 'NR==2 {print $5}' | sed 's/%//')
    
    # 网络流量
    local network_stats=$(cat /proc/net/dev | grep "$NETWORK_INTERFACE:")
    local rx_bytes=$(echo $network_stats | awk '{print $2}')
    local tx_bytes=$(echo $network_stats | awk '{print $10}')
    local rx_mb=$(echo "scale=2; $rx_bytes / 1024 / 1024" | bc)
    local tx_mb=$(echo "scale=2; $tx_bytes / 1024 / 1024" | bc)
    
    # API响应时间
    local api_response_time=0
    if curl -s -w "%{time_total}" -o /dev/null "$API_BASE_URL/health" 2>/dev/null; then
        api_response_time=$(curl -s -w "%{time_total}" -o /dev/null "$API_BASE_URL/health" 2>/dev/null)
        api_response_time=$(echo "$api_response_time * 1000" | bc | cut -d. -f1)
    fi
    
    # Docker容器统计
    local docker_stats=""
    if command -v docker &> /dev/null; then
        docker_stats=$(docker stats --no-stream --format "{{.Container}}\t{{.CPUPerc}}\t{{.MemUsage}}" 2>/dev/null | grep -E "miniodb|redis|minio" || echo "")
    fi
    
    # 显示监控信息
    echo -e "${BLUE}=== $(date) ===${NC}"
    echo -e "${GREEN}系统资源:${NC}"
    echo -e "  CPU使用率: ${cpu_percent}%"
    echo -e "  内存使用: ${memory_used_gb}GB / ${memory_percent}%"
    echo -e "  磁盘使用: ${disk_percent}%"
    echo -e "  网络流量: RX ${rx_mb}MB, TX ${tx_mb}MB"
    
    echo -e "${GREEN}API性能:${NC}"
    echo -e "  响应时间: ${api_response_time}ms"
    
    if [ -n "$docker_stats" ]; then
        echo -e "${GREEN}容器状态:${NC}"
        echo "$docker_stats" | while read line; do
            echo -e "  $line"
        done
    fi
    
    # 记录到日志文件
    echo "$timestamp,$cpu_percent,$memory_used_gb,$memory_percent,$disk_percent,$rx_mb,$tx_mb,$api_response_time,0,0" >> "$LOG_FILE"
    
    echo ""
}

# 监控数据库性能指标
monitor_database() {
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    
    # 尝试获取数据库统计信息
    local table_count=0
    local query_count=0
    local avg_query_time=0
    
    # 获取表数量
    if curl -s "$API_BASE_URL/api/tables" &>/dev/null; then
        table_count=$(curl -s "$API_BASE_URL/api/tables" | jq length 2>/dev/null || echo "0")
    fi
    
    echo -e "${GREEN}数据库指标:${NC}"
    echo -e "  表数量: $table_count"
    echo -e "  查询数: $query_count"
    echo -e "  平均查询时间: ${avg_query_time}ms"
}

# 生成性能报告摘要
generate_summary() {
    if [ ! -f "$LOG_FILE" ]; then
        echo -e "${RED}日志文件不存在${NC}"
        return
    fi
    
    echo -e "${GREEN}生成性能监控摘要...${NC}"
    
    local summary_file="$RESULTS_DIR/monitor_summary.txt"
    
    cat > "$summary_file" << EOF
MinIODB 性能监控摘要
生成时间: $(date)
监控时长: $(tail -n +2 "$LOG_FILE" | wc -l) 个数据点

系统资源统计:
EOF
    
    # 分析CPU使用率
    local avg_cpu=$(tail -n +2 "$LOG_FILE" | awk -F',' '{sum+=$2; count++} END {printf "%.2f", sum/count}')
    local max_cpu=$(tail -n +2 "$LOG_FILE" | awk -F',' '{if($2>max) max=$2} END {print max}')
    
    # 分析内存使用
    local avg_memory=$(tail -n +2 "$LOG_FILE" | awk -F',' '{sum+=$4; count++} END {printf "%.2f", sum/count}')
    local max_memory=$(tail -n +2 "$LOG_FILE" | awk -F',' '{if($4>max) max=$4} END {print max}')
    
    # 分析API响应时间
    local avg_response=$(tail -n +2 "$LOG_FILE" | awk -F',' '{sum+=$8; count++} END {printf "%.0f", sum/count}')
    local max_response=$(tail -n +2 "$LOG_FILE" | awk -F',' '{if($8>max) max=$8} END {print max}')
    
    cat >> "$summary_file" << EOF
  平均CPU使用率: ${avg_cpu}%
  最大CPU使用率: ${max_cpu}%
  平均内存使用率: ${avg_memory}%
  最大内存使用率: ${max_memory}%

API性能统计:
  平均响应时间: ${avg_response}ms
  最大响应时间: ${max_response}ms

性能评估:
EOF
    
    # 性能评估
    if (( $(echo "$avg_cpu < 50" | bc -l) )); then
        echo "  CPU性能: 良好" >> "$summary_file"
    elif (( $(echo "$avg_cpu < 80" | bc -l) )); then
        echo "  CPU性能: 一般" >> "$summary_file"
    else
        echo "  CPU性能: 需要优化" >> "$summary_file"
    fi
    
    if (( $(echo "$avg_memory < 70" | bc -l) )); then
        echo "  内存性能: 良好" >> "$summary_file"
    elif (( $(echo "$avg_memory < 90" | bc -l) )); then
        echo "  内存性能: 一般" >> "$summary_file"
    else
        echo "  内存性能: 需要优化" >> "$summary_file"
    fi
    
    if (( $(echo "$avg_response < 100" | bc -l) )); then
        echo "  API响应: 优秀" >> "$summary_file"
    elif (( $(echo "$avg_response < 500" | bc -l) )); then
        echo "  API响应: 良好" >> "$summary_file"
    else
        echo "  API响应: 需要优化" >> "$summary_file"
    fi
    
    echo -e "${GREEN}摘要已保存到: $summary_file${NC}"
    cat "$summary_file"
}

# 信号处理函数
cleanup() {
    echo -e "\n${YELLOW}监控停止，生成摘要...${NC}"
    generate_summary
    echo -e "${GREEN}监控完成${NC}"
    exit 0
}

# 设置信号处理
trap cleanup SIGINT SIGTERM

# 检查依赖
check_dependencies() {
    local missing_deps=()
    
    if ! command -v bc &> /dev/null; then
        missing_deps+=("bc")
    fi
    
    if ! command -v jq &> /dev/null; then
        missing_deps+=("jq")
    fi
    
    if ! command -v curl &> /dev/null; then
        missing_deps+=("curl")
    fi
    
    if [ ${#missing_deps[@]} -ne 0 ]; then
        echo -e "${RED}缺少依赖: ${missing_deps[*]}${NC}"
        echo -e "${YELLOW}请安装缺少的依赖:${NC}"
        echo "  Ubuntu/Debian: sudo apt-get install ${missing_deps[*]}"
        echo "  CentOS/RHEL: sudo yum install ${missing_deps[*]}"
        exit 1
    fi
}

# 主监控循环
main() {
    check_dependencies
    
    # 检查API是否可用
    if ! curl -s "$API_BASE_URL/health" &>/dev/null; then
        echo -e "${YELLOW}警告: MinIODB API 不可用，某些指标可能无法获取${NC}"
    fi
    
    while true; do
        monitor_system
        monitor_database
        sleep "$MONITOR_INTERVAL"
    done
}

# 如果脚本被直接执行
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi 