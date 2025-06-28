#!/bin/bash

# MinIODB Comprehensive Performance Test Script
# Measures TPS, QPS, latency, and system resource usage

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
NC='\033[0m'

# Configuration
API_BASE="http://localhost:8081/v1"
TEST_TABLE="performance_test_$(date +%s)"
TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')
RESULTS_FILE="performance_results_$(date +%Y%m%d_%H%M%S).json"

# API Key for authentication (if needed)
API_KEY="api-key-1234567890abcdef"

echo -e "${PURPLE}=====================================================${NC}"
echo -e "${PURPLE}MinIODB 性能测试报告生成器${NC}"
echo -e "${PURPLE}=====================================================${NC}"
echo -e "${BLUE}测试时间: $TIMESTAMP${NC}"
echo -e "${BLUE}测试环境: ARM64 macOS${NC}"
echo

# Function to log with timestamp
log() {
    echo -e "${GREEN}[$(date '+%H:%M:%S')]${NC} $1"
}

# Function to test API connectivity
test_connectivity() {
    log "测试API连通性..."
    
    local response=$(curl -s -w "%{http_code}" -o /tmp/health_check.json "$API_BASE/health")
    if [ "$response" = "200" ]; then
        echo -e "${GREEN}✅ API连通性正常${NC}"
        cat /tmp/health_check.json | jq . 2>/dev/null || cat /tmp/health_check.json
        return 0
    else
        echo -e "${RED}❌ API连通性失败 (HTTP $response)${NC}"
        if [ -f /tmp/health_check.json ]; then
            echo "响应内容:"
            cat /tmp/health_check.json
        fi
        return 1
    fi
}

# Function to collect system information
collect_system_info() {
    log "收集系统信息..."
    
    cat > system_info.json << EOF
{
    "timestamp": "$TIMESTAMP",
    "hardware": {
        "cpu": "$(system_profiler SPHardwareDataType | grep 'Chip:' | sed 's/.*Chip: //')",
        "memory": "$(system_profiler SPHardwareDataType | grep 'Memory:' | sed 's/.*Memory: //')",
        "architecture": "$(uname -m)"
    },
    "software": {
        "os": "$(sw_vers -productName)",
        "version": "$(sw_vers -productVersion)",
        "docker": "$(docker --version | cut -d' ' -f3 | sed 's/,//')",
        "go": "$(go version | cut -d' ' -f3)"
    },
    "deployment": {
        "type": "Docker Compose",
        "mode": "Single Node",
        "network": "Bridge"
    }
}
EOF

    echo -e "${GREEN}✅ 系统信息收集完成${NC}"
}

# Function to test write performance (TPS)
test_write_performance() {
    log "测试写入性能 (TPS)..."
    
    local total_records=50
    local batch_size=1
    local batches=$((total_records / batch_size))
    
    echo "  - 总记录数: $total_records"
    echo "  - 批次大小: $batch_size (单条记录测试)"
    echo "  - 批次数量: $batches"
    
    local start_time=$(date +%s.%N)
    local success_count=0
    local error_count=0
    local total_latency=0
    
    # 测试单条记录写入性能
    for i in $(seq 1 $total_records); do
        local record_start=$(date +%s.%N)
        
        # 构造写入数据
        local write_data=$(cat << EOF
{
    "table": "$TEST_TABLE",
    "id": "test_record_$i",
    "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "payload": {
        "record_id": $i,
        "name": "performance_test_$i",
        "value": $(echo "scale=2; $i * 1.5" | bc),
        "category": "test",
        "status": "active"
    }
}
EOF
)
        
        local response_file="/tmp/write_response_$i.json"
        local response=$(curl -s -w "%{http_code}" -o "$response_file" \
            -X POST \
            -H "Content-Type: application/json" \
            -H "X-API-Key: $API_KEY" \
            -d "$write_data" \
            "$API_BASE/data")
        
        local record_end=$(date +%s.%N)
        local record_latency=$(echo "$record_end - $record_start" | bc -l)
        total_latency=$(echo "$total_latency + $record_latency" | bc -l)
        
        if [ "$response" = "200" ] || [ "$response" = "201" ]; then
            success_count=$((success_count + 1))
        else
            error_count=$((error_count + 1))
            # 记录错误详情
            echo "Error $i (HTTP $response): $(cat "$response_file")" >> write_errors.log
        fi
        
        # 每10条记录显示进度
        if [ $((i % 10)) -eq 0 ]; then
            echo -n "."
        fi
        
        # 轻微延迟避免rate limit
        sleep 0.1
    done
    echo
    
    local end_time=$(date +%s.%N)
    local total_time=$(echo "$end_time - $start_time" | bc -l)
    local tps=$(echo "scale=2; $success_count / $total_time" | bc -l)
    local avg_latency=$(echo "scale=3; $total_latency / $total_records" | bc -l)
    
    echo -e "${GREEN}✅ 写入性能测试完成${NC}"
    echo "  - 成功记录: $success_count"
    echo "  - 失败记录: $error_count"
    echo "  - 总时间: ${total_time}s"
    echo "  - TPS: ${tps} transactions/sec"
    echo "  - 平均延迟: ${avg_latency}s/record"
    echo "  - 成功率: $(echo "scale=1; $success_count * 100 / $total_records" | bc -l)%"
    
    # Save results
    cat >> "$RESULTS_FILE" << EOF
{
    "write_performance": {
        "total_records": $total_records,
        "success_count": $success_count,
        "error_count": $error_count,
        "total_time": $total_time,
        "tps": $tps,
        "avg_latency": $avg_latency,
        "success_rate": $(echo "scale=3; $success_count * 100 / $total_records" | bc -l),
        "batch_size": $batch_size
    },
EOF
}

# Function to test query performance (QPS)
test_query_performance() {
    log "测试查询性能 (QPS)..."
    
    local query_count=30
    local success_count=0
    local error_count=0
    local total_query_time=0
    
    echo "  - 查询次数: $query_count"
    
    # 不同类型的查询测试
    local queries=(
        "SELECT COUNT(*) as total_records FROM $TEST_TABLE"
        "SELECT AVG(value) as avg_value FROM $TEST_TABLE WHERE category = 'test'"
        "SELECT * FROM $TEST_TABLE WHERE record_id > 25 LIMIT 10"
        "SELECT category, COUNT(*) as count FROM $TEST_TABLE GROUP BY category"
        "SELECT * FROM $TEST_TABLE WHERE status = 'active' ORDER BY record_id DESC LIMIT 5"
    )
    
    for i in $(seq 1 $query_count); do
        local query_start=$(date +%s.%N)
        
        # 轮询使用不同查询
        local query_index=$((($i - 1) % ${#queries[@]}))
        local sql="${queries[$query_index]}"
        
        local query_data=$(cat << EOF
{
    "sql": "$sql"
}
EOF
)
        
        local response_file="/tmp/query_response_$i.json"
        local response=$(curl -s -w "%{http_code}" -o "$response_file" \
            -X POST \
            -H "Content-Type: application/json" \
            -H "X-API-Key: $API_KEY" \
            -d "$query_data" \
            "$API_BASE/query")
        
        local query_end=$(date +%s.%N)
        local query_time=$(echo "$query_end - $query_start" | bc -l)
        
        if [ "$response" = "200" ]; then
            success_count=$((success_count + 1))
            total_query_time=$(echo "$total_query_time + $query_time" | bc -l)
        else
            error_count=$((error_count + 1))
            # 记录错误详情
            echo "Query Error $i (HTTP $response): $(cat "$response_file")" >> query_errors.log
        fi
        
        echo -n "."
        sleep 0.1  # 避免rate limit
    done
    echo
    
    local avg_query_time=0
    local qps=0
    if [ $success_count -gt 0 ]; then
        avg_query_time=$(echo "scale=3; $total_query_time / $success_count" | bc -l)
        qps=$(echo "scale=2; $success_count / $total_query_time" | bc -l)
    fi
    
    echo -e "${GREEN}✅ 查询性能测试完成${NC}"
    echo "  - 成功查询: $success_count"
    echo "  - 失败查询: $error_count"
    echo "  - 总查询时间: ${total_query_time}s"
    echo "  - QPS: ${qps} queries/sec"
    echo "  - 平均查询延迟: ${avg_query_time}s"
    echo "  - 查询成功率: $(echo "scale=1; $success_count * 100 / $query_count" | bc -l)%"
    
    # Append to results
    sed -i '' '$s/,$//' "$RESULTS_FILE"
    cat >> "$RESULTS_FILE" << EOF
,
    "query_performance": {
        "query_count": $query_count,
        "success_count": $success_count,
        "error_count": $error_count,
        "total_query_time": $total_query_time,
        "qps": $qps,
        "avg_query_time": $avg_query_time,
        "success_rate": $(echo "scale=3; $success_count * 100 / $query_count" | bc -l)
    }
}
EOF
}

# Function to collect resource usage
collect_resource_usage() {
    log "收集系统资源使用情况..."
    
    # Memory usage
    local memory_info=$(vm_stat | grep -E "(free|active|inactive|wired|compressed)")
    local memory_pressure=$(memory_pressure 2>/dev/null | grep "System-wide memory pressure" || echo "Normal")
    
    # CPU usage
    local cpu_usage=$(top -l 1 -n 0 | grep "CPU usage" | sed 's/CPU usage: //')
    
    # Docker container stats
    local container_stats=$(docker stats --no-stream --format "table {{.Container}}\t{{.CPUPerc}}\t{{.MemUsage}}" 2>/dev/null | tail -n +2)
    
    # Disk usage
    local disk_usage=$(df -h / | tail -1 | awk '{print $5}')
    
    echo -e "${GREEN}✅ 资源使用情况收集完成${NC}"
    echo "  - CPU使用率: $cpu_usage"
    echo "  - 内存压力: $memory_pressure"
    echo "  - 磁盘使用: $disk_usage"
    echo "  - 容器统计:"
    echo "$container_stats" | while read line; do
        echo "    $line"
    done
    
    # Save resource info
    cat > resource_usage.json << EOF
{
    "timestamp": "$(date '+%Y-%m-%d %H:%M:%S')",
    "cpu_usage": "$cpu_usage",
    "memory_pressure": "$memory_pressure",
    "disk_usage": "$disk_usage",
    "container_stats": "$container_stats"
}
EOF
}

# Function to analyze errors
analyze_errors() {
    log "分析错误日志..."
    
    if [ -f "write_errors.log" ]; then
        local write_error_count=$(wc -l < write_errors.log)
        echo "  - 写入错误数: $write_error_count"
        if [ $write_error_count -gt 0 ]; then
            echo "  - 主要写入错误:"
            head -3 write_errors.log | while read line; do
                echo "    $line"
            done
        fi
    fi
    
    if [ -f "query_errors.log" ]; then
        local query_error_count=$(wc -l < query_errors.log)
        echo "  - 查询错误数: $query_error_count"
        if [ $query_error_count -gt 0 ]; then
            echo "  - 主要查询错误:"
            head -3 query_errors.log | while read line; do
                echo "    $line"
            done
        fi
    fi
}

# Main execution
main() {
    echo "{" > "$RESULTS_FILE"
    
    # Test connectivity
    if ! test_connectivity; then
        echo -e "${RED}❌ API连通性测试失败，退出${NC}"
        exit 1
    fi
    
    # Collect system info
    collect_system_info
    
    # Test write performance
    test_write_performance
    
    # Test query performance  
    test_query_performance
    
    # Collect resource usage
    collect_resource_usage
    
    # Analyze errors
    analyze_errors
    
    echo
    echo -e "${PURPLE}=====================================================${NC}"
    echo -e "${PURPLE}性能测试完成${NC}"
    echo -e "${PURPLE}=====================================================${NC}"
    echo -e "${GREEN}结果已保存到: $RESULTS_FILE${NC}"
    echo -e "${GREEN}系统信息已保存到: system_info.json${NC}"
    echo -e "${GREEN}资源使用已保存到: resource_usage.json${NC}"
    echo
    
    # Display summary
    echo -e "${YELLOW}性能摘要:${NC}"
    if [ -f "$RESULTS_FILE" ]; then
        local tps=$(cat "$RESULTS_FILE" | grep -o '"tps": [0-9.]*' | cut -d'"' -f4)
        local qps=$(cat "$RESULTS_FILE" | grep -o '"qps": [0-9.]*' | cut -d'"' -f4)
        local write_success=$(cat "$RESULTS_FILE" | grep -o '"success_rate": [0-9.]*' | head -1 | cut -d'"' -f4)
        local query_success=$(cat "$RESULTS_FILE" | grep -o '"success_rate": [0-9.]*' | tail -1 | cut -d'"' -f4)
        
        echo "📊 核心指标:"
        echo "  - TPS (每秒事务数): ${tps:-0}"
        echo "  - QPS (每秒查询数): ${qps:-0}" 
        echo "  - 写入成功率: ${write_success:-0}%"
        echo "  - 查询成功率: ${query_success:-0}%"
        
        # 性能评估
        echo
        echo "🎯 性能评估:"
        if (( $(echo "$tps > 5" | bc -l) )); then
            echo "  - 写入性能: ${GREEN}良好${NC} (TPS > 5)"
        elif (( $(echo "$tps > 1" | bc -l) )); then
            echo "  - 写入性能: ${YELLOW}一般${NC} (1 < TPS <= 5)"
        else
            echo "  - 写入性能: ${RED}需优化${NC} (TPS <= 1)"
        fi
        
        if (( $(echo "$qps > 10" | bc -l) )); then
            echo "  - 查询性能: ${GREEN}良好${NC} (QPS > 10)"
        elif (( $(echo "$qps > 3" | bc -l) )); then
            echo "  - 查询性能: ${YELLOW}一般${NC} (3 < QPS <= 10)"
        else
            echo "  - 查询性能: ${RED}需优化${NC} (QPS <= 3)"
        fi
    fi
    
    echo
    echo -e "${BLUE}📋 建议下一步:${NC}"
    echo "  1. 运行 './generate_report.sh' 生成详细HTML报告"
    echo "  2. 查看错误日志以分析性能瓶颈"
    echo "  3. 根据结果调整配置参数"
}

# Run main function
main "$@" 