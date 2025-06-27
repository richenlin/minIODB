# MinIODB 性能测试 Makefile

.PHONY: help setup deploy status test-quick test-concurrent test-tb-scale test-full cleanup
.DEFAULT_GOAL := help

# 颜色定义
GREEN := \033[0;32m
YELLOW := \033[1;33m
RED := \033[0;31m
BLUE := \033[0;34m
NC := \033[0m # No Color

# 项目配置
PROJECT_ROOT := $(shell pwd)/../..
DOCKER_COMPOSE_FILE := $(PROJECT_ROOT)/deploy/docker/docker-compose.yml
TEST_RESULTS_DIR := ./test_results

help: ## 显示帮助信息
	@echo "$(GREEN)MinIODB 高并发性能测试命令$(NC)"
	@echo ""
	@echo "$(YELLOW)环境管理:$(NC)"
	@echo "  setup           设置测试环境"
	@echo "  deploy          部署MinIODB服务"
	@echo "  status          检查服务状态"
	@echo "  cleanup         清理测试环境"
	@echo ""
	@echo "$(YELLOW)性能测试:$(NC)"
	@echo "  test-quick      快速性能测试"
	@echo "  test-concurrent 高并发性能测试"
	@echo "  test-tb-scale   TB级数据查询测试"
	@echo "  test-full       完整性能测试套件"
	@echo ""
	@echo "$(YELLOW)高级测试:$(NC)"
	@echo "  test-stress     压力测试"
	@echo "  test-olap       OLAP专项测试"
	@echo "  test-columnar   列式存储测试"
	@echo ""
	@echo "$(YELLOW)报告生成:$(NC)"
	@echo "  report          生成性能报告"
	@echo "  benchmark       运行基准测试并生成报告"
	@echo ""
	@echo "$(YELLOW)监控和调试:$(NC)"
	@echo "  monitor         监控系统资源"
	@echo "  logs            查看服务日志"
	@echo "  debug-api       调试API连接"

setup: ## 设置测试环境
	@echo "$(GREEN)Setting up performance testing environment...$(NC)"
	@mkdir -p $(TEST_RESULTS_DIR)
	@cd $(PROJECT_ROOT) && go mod tidy
	@echo "$(GREEN)Environment setup completed$(NC)"

deploy: setup ## 部署MinIODB服务
	@echo "$(GREEN)Deploying MinIODB service...$(NC)"
	@cd $(PROJECT_ROOT) && docker-compose -f $(DOCKER_COMPOSE_FILE) up -d
	@echo "$(YELLOW)Waiting for services to start...$(NC)"
	@sleep 30
	@echo "$(GREEN)MinIODB service deployed$(NC)"

status: ## 检查服务状态
	@echo "$(GREEN)Checking service status...$(NC)"
	@cd $(PROJECT_ROOT) && docker-compose -f $(DOCKER_COMPOSE_FILE) ps
	@echo "$(BLUE)API Health Check:$(NC)"
	@curl -s http://localhost:8080/health || echo "$(RED)API not responding$(NC)"

test-quick: setup ## 运行快速性能测试
	@echo "$(GREEN)Running quick performance tests...$(NC)"
	@go test -v -run="TestBasicPerformance|TestAPIConnection" -timeout=5m > $(TEST_RESULTS_DIR)/quick_test.log 2>&1
	@echo "$(GREEN)Quick tests completed. Results saved to $(TEST_RESULTS_DIR)/quick_test.log$(NC)"

test-concurrent: deploy ## 运行高并发性能测试
	@echo "$(GREEN)Running high concurrency performance tests...$(NC)"
	@echo "$(YELLOW)Testing concurrent reads, writes, and mixed workloads...$(NC)"
	@go test -v -run="TestHighConcurrency|TestMixedConcurrentWorkload" -timeout=30m > $(TEST_RESULTS_DIR)/concurrent_test.log 2>&1
	@echo "$(GREEN)Concurrent tests completed. Results saved to $(TEST_RESULTS_DIR)/concurrent_test.log$(NC)"

test-tb-scale: deploy ## 运行TB级数据查询测试
	@echo "$(GREEN)Running TB scale query performance tests...$(NC)"
	@echo "$(YELLOW)This may take a long time for data preparation...$(NC)"
	@go test -v -run="TestTBScale" -timeout=120m > $(TEST_RESULTS_DIR)/tb_scale_test.log 2>&1
	@echo "$(GREEN)TB scale tests completed. Results saved to $(TEST_RESULTS_DIR)/tb_scale_test.log$(NC)"

test-stress: deploy ## 运行压力测试
	@echo "$(GREEN)Running stress tests...$(NC)"
	@go test -v -run="TestStress|TestTBScaleStressTest" -timeout=60m > $(TEST_RESULTS_DIR)/stress_test.log 2>&1
	@echo "$(GREEN)Stress tests completed. Results saved to $(TEST_RESULTS_DIR)/stress_test.log$(NC)"

test-olap: deploy ## 运行OLAP专项测试
	@echo "$(GREEN)Running OLAP feature tests...$(NC)"
	@go test -v -run="TestOLAP|TestColumnarStorage" -timeout=30m > $(TEST_RESULTS_DIR)/olap_test.log 2>&1
	@echo "$(GREEN)OLAP tests completed. Results saved to $(TEST_RESULTS_DIR)/olap_test.log$(NC)"

test-columnar: deploy ## 运行列式存储测试
	@echo "$(GREEN)Running columnar storage optimization tests...$(NC)"
	@go test -v -run="TestColumnarStorageOptimization" -timeout=15m > $(TEST_RESULTS_DIR)/columnar_test.log 2>&1
	@echo "$(GREEN)Columnar storage tests completed. Results saved to $(TEST_RESULTS_DIR)/columnar_test.log$(NC)"

test-full: deploy ## 运行完整性能测试套件
	@echo "$(GREEN)Running full performance test suite...$(NC)"
	@echo "$(YELLOW)This will run all performance tests and may take several hours...$(NC)"
	@$(MAKE) test-quick
	@$(MAKE) test-concurrent
	@$(MAKE) test-olap
	@$(MAKE) test-columnar
	@echo "$(BLUE)Starting TB scale tests (this may take 1-2 hours)...$(NC)"
	@$(MAKE) test-tb-scale
	@$(MAKE) test-stress
	@echo "$(GREEN)Full performance test suite completed$(NC)"
	@$(MAKE) report

benchmark: deploy ## 运行基准测试并生成报告
	@echo "$(GREEN)Running benchmark tests...$(NC)"
	@go test -v -bench=. -benchmem -run=^$$ > $(TEST_RESULTS_DIR)/benchmark.log 2>&1
	@echo "$(GREEN)Benchmark tests completed$(NC)"
	@$(MAKE) report

report: ## 生成性能报告
	@echo "$(GREEN)Generating performance report...$(NC)"
	@./generate_report.sh $(TEST_RESULTS_DIR)
	@echo "$(GREEN)Performance report generated$(NC)"

monitor: ## 监控系统资源
	@echo "$(GREEN)Monitoring system resources...$(NC)"
	@echo "$(YELLOW)Press Ctrl+C to stop monitoring$(NC)"
	@while true; do \
		echo "=== $(shell date) ==="; \
		echo "CPU Usage:"; \
		top -bn1 | grep "Cpu(s)" | awk '{print $$2}' | awk -F'%' '{print $$1}'; \
		echo "Memory Usage:"; \
		free -h | grep Mem; \
		echo "Disk Usage:"; \
		df -h | grep -E "/$|/var"; \
		echo "Docker Stats:"; \
		docker stats --no-stream --format "table {{.Container}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}\t{{.BlockIO}}"; \
		echo ""; \
		sleep 5; \
	done

logs: ## 查看服务日志
	@echo "$(GREEN)Showing service logs...$(NC)"
	@cd $(PROJECT_ROOT) && docker-compose -f $(DOCKER_COMPOSE_FILE) logs -f --tail=100

debug-api: ## 调试API连接
	@echo "$(GREEN)Debugging API connection...$(NC)"
	@echo "$(BLUE)Testing API endpoints:$(NC)"
	@echo "Health check:"
	@curl -s -w "Status: %{http_code}, Time: %{time_total}s\n" http://localhost:8080/health || echo "$(RED)Failed$(NC)"
	@echo "Tables list:"
	@curl -s -w "Status: %{http_code}, Time: %{time_total}s\n" http://localhost:8080/api/tables || echo "$(RED)Failed$(NC)"
	@echo "$(BLUE)Service connectivity test completed$(NC)"

cleanup: ## 清理测试环境
	@echo "$(GREEN)Cleaning up test environment...$(NC)"
	@cd $(PROJECT_ROOT) && docker-compose -f $(DOCKER_COMPOSE_FILE) down -v
	@docker system prune -f
	@echo "$(GREEN)Cleanup completed$(NC)"

# 高级测试场景
test-scenario-read-heavy: deploy ## 读密集型测试场景
	@echo "$(GREEN)Running read-heavy workload scenario...$(NC)"
	@go test -v -run="TestHighConcurrencyReads" -timeout=20m > $(TEST_RESULTS_DIR)/read_heavy_test.log 2>&1
	@echo "$(GREEN)Read-heavy scenario completed$(NC)"

test-scenario-write-heavy: deploy ## 写密集型测试场景
	@echo "$(GREEN)Running write-heavy workload scenario...$(NC)"
	@go test -v -run="TestHighConcurrencyWrites" -timeout=20m > $(TEST_RESULTS_DIR)/write_heavy_test.log 2>&1
	@echo "$(GREEN)Write-heavy scenario completed$(NC)"

test-scenario-mixed: deploy ## 混合工作负载测试场景
	@echo "$(GREEN)Running mixed workload scenarios...$(NC)"
	@go test -v -run="TestMixedConcurrentWorkload" -timeout=30m > $(TEST_RESULTS_DIR)/mixed_workload_test.log 2>&1
	@echo "$(GREEN)Mixed workload scenarios completed$(NC)"

# 性能调优建议
tune-suggestions: ## 显示性能调优建议
	@echo "$(GREEN)MinIODB 性能调优建议:$(NC)"
	@echo ""
	@echo "$(YELLOW)1. 硬件配置建议:$(NC)"
	@echo "  - CPU: 16+ 核心，支持向量化指令"
	@echo "  - 内存: 64GB+ RAM，用于缓存和查询处理"
	@echo "  - 存储: NVMe SSD，RAID 0/10配置"
	@echo "  - 网络: 10Gbps+ 网络带宽"
	@echo ""
	@echo "$(YELLOW)2. 系统配置优化:$(NC)"
	@echo "  - 增加文件描述符限制: ulimit -n 65536"
	@echo "  - 调整内核参数: vm.swappiness=1"
	@echo "  - 优化TCP参数: net.core.rmem_max=134217728"
	@echo ""
	@echo "$(YELLOW)3. MinIODB配置优化:$(NC)"
	@echo "  - 增加连接池大小: max_connections=1000"
	@echo "  - 调整缓存大小: cache_size=32GB"
	@echo "  - 优化分区策略: partition_count=CPU核心数*4"
	@echo ""
	@echo "$(YELLOW)4. 查询优化建议:$(NC)"
	@echo "  - 使用列式存储索引"
	@echo "  - 合理设计分区键"
	@echo "  - 避免全表扫描"
	@echo "  - 使用预聚合表" 