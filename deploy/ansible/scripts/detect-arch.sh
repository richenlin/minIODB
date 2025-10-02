#!/bin/bash

# 架构检测脚本
# 支持自动识别ARM64和AMD64架构

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

# 检测系统架构
detect_architecture() {
    local arch=$(uname -m)
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    
    case $arch in
        x86_64|amd64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        armv7l)
            echo "armv7"
            ;;
        *)
            log_error "Unsupported architecture: $arch"
            return 1
            ;;
    esac
}

# 检测操作系统
detect_os() {
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    
    case $os in
        linux)
            if [ -f /etc/os-release ]; then
                . /etc/os-release
                echo "${ID}"
            elif [ -f /etc/redhat-release ]; then
                echo "rhel"
            elif [ -f /etc/debian_version ]; then
                echo "debian"
            else
                echo "linux"
            fi
            ;;
        darwin)
            echo "darwin"
            ;;
        *)
            log_error "Unsupported OS: $os"
            return 1
            ;;
    esac
}

# 检测系统信息
detect_system_info() {
    local arch=$(detect_architecture)
    local os=$(detect_os)
    local kernel=$(uname -r)
    local hostname=$(hostname)
    
    log_info "=== System Information ==="
    log_info "Architecture: $arch"
    log_info "OS: $os"
    log_info "Kernel: $kernel"
    log_info "Hostname: $hostname"
    log_info "=========================="
    
    # 输出JSON格式供Ansible使用
    cat << EOF
{
    "architecture": "$arch",
    "os": "$os",
    "kernel": "$kernel",
    "hostname": "$hostname"
}
EOF
}

# 验证架构支持
validate_architecture() {
    local arch=$(detect_architecture)
    
    case $arch in
        amd64|arm64)
            log_success "Architecture $arch is supported"
            return 0
            ;;
        *)
            log_error "Architecture $arch is not supported"
            log_error "Supported architectures: amd64, arm64"
            return 1
            ;;
    esac
}

# 主函数
main() {
    case "${1:-detect}" in
        "detect")
            detect_architecture
            ;;
        "validate")
            validate_architecture
            ;;
        "info")
            detect_system_info
            ;;
        "json")
            detect_system_info
            ;;
        *)
            echo "Usage: $0 [detect|validate|info|json]"
            echo "  detect   - Output architecture (default)"
            echo "  validate - Validate architecture support"
            echo "  info     - Show system information"
            echo "  json     - Output system info in JSON format"
            exit 1
            ;;
    esac
}

# 执行主函数
main "$@" 