#!/bin/bash

# MinIODB 依赖管理和安装脚本
# 支持自动检测、安装和离线包下载

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

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

# 检测系统信息
detect_system() {
    OS_TYPE=$(uname -s)
    ARCH=$(uname -m)

    case $OS_TYPE in
        Linux*)
            OS="linux"
            ;;
        Darwin*)
            OS="macos"
            ;;
        *)
            log_error "不支持的操作系统: $OS_TYPE"
            exit 1
            ;;
    esac

    case $ARCH in
        x86_64|amd64)
            ARCH_TYPE="x86_64"
            ARCH_SUFFIX="amd64"
            ;;
        aarch64|arm64)
            ARCH_TYPE="arm64"
            ARCH_SUFFIX="arm64"
            ;;
        *)
            log_error "不支持的架构: $ARCH"
            exit 1
            ;;
    esac

    log_info "检测到系统: $OS $ARCH_TYPE"
}

# 检测包管理器
detect_package_manager() {
    if [[ "$OS" == "linux" ]]; then
        if command -v apt-get &> /dev/null; then
            PKG_MANAGER="apt"
        elif command -v yum &> /dev/null; then
            PKG_MANAGER="yum"
        elif command -v dnf &> /dev/null; then
            PKG_MANAGER="dnf"
        elif command -v pacman &> /dev/null; then
            PKG_MANAGER="pacman"
        else
            PKG_MANAGER="unknown"
        fi
    elif [[ "$OS" == "macos" ]]; then
        if command -v brew &> /dev/null; then
            PKG_MANAGER="brew"
        else
            PKG_MANAGER="unknown"
        fi
    fi

    log_info "检测到包管理器: $PKG_MANAGER"
}

# 检查网络连接
check_network() {
    if curl -s --connect-timeout 5 https://www.google.com > /dev/null 2>&1; then
        NETWORK_AVAILABLE=true
        log_info "网络连接正常"
    else
        NETWORK_AVAILABLE=false
        log_warning "无法检测到网络连接"
    fi
}

# 离线包下载地址
show_offline_downloads() {
    local deps=$1

    log_info "========================================"
    log_info "离线安装包下载地址"
    log_info "========================================"
    echo ""

    if [[ "$deps" == *"docker"* ]]; then
        if [[ "$OS" == "linux" ]]; then
            if [[ "$ARCH_TYPE" == "x86_64" ]]; then
                echo "Docker (Linux x86_64):"
                echo "  https://download.docker.com/linux/static/stable/x86_64/docker-24.0.7.tgz"
            elif [[ "$ARCH_TYPE" == "arm64" ]]; then
                echo "Docker (Linux ARM64):"
                echo "  https://download.docker.com/linux/static/stable/aarch64/docker-24.0.7.tgz"
            fi
            echo "  安装方法: tar xzvf docker-24.0.7.tgz && sudo cp docker/* /usr/bin/"
            echo ""
        elif [[ "$OS" == "macos" ]]; then
            echo "Docker Desktop for Mac (ARM64):"
            echo "  https://desktop.docker.com/mac/main/arm64/Docker.dmg"
            echo "  下载后双击安装"
            echo ""
        fi
    fi

    if [[ "$deps" == *"docker-compose"* ]]; then
        if [[ "$OS" == "linux" ]]; then
            if [[ "$ARCH_TYPE" == "x86_64" ]]; then
                echo "Docker Compose (Linux x86_64):"
                echo "  https://github.com/docker/compose/releases/download/v2.23.0/docker-compose-linux-x86_64"
                echo "  安装方法: sudo mv docker-compose-linux-x86_64 /usr/local/bin/docker-compose && sudo chmod +x /usr/local/bin/docker-compose"
            elif [[ "$ARCH_TYPE" == "arm64" ]]; then
                echo "Docker Compose (Linux ARM64):"
                echo "  https://github.com/docker/compose/releases/download/v2.23.0/docker-compose-linux-aarch64"
                echo "  安装方法: sudo mv docker-compose-linux-aarch64 /usr/local/bin/docker-compose && sudo chmod +x /usr/local/bin/docker-compose"
            fi
            echo ""
        elif [[ "$OS" == "macos" ]]; then
            echo "Docker Compose (已包含在 Docker Desktop 中):"
            echo "  安装 Docker Desktop 即可"
            echo ""
        fi
    fi

    if [[ "$deps" == *"kubectl"* ]]; then
        if [[ "$OS" == "linux" ]]; then
            echo "kubectl (Linux):"
            echo "  https://dl.k8s.io/release/v1.28.4/bin/linux/$ARCH_SUFFIX/kubectl"
            echo "  安装方法: sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl"
            echo ""
        elif [[ "$OS" == "macos" ]]; then
            echo "kubectl (macOS ARM64):"
            echo "  https://dl.k8s.io/release/v1.28.4/bin/darwin/arm64/kubectl"
            echo "  安装方法: sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl"
            echo ""
        fi
    fi

    if [[ "$deps" == *"ansible"* ]]; then
        if [[ "$OS" == "linux" ]]; then
            echo "Ansible (Linux):"
            echo "  使用包管理器安装:"
            case $PKG_MANAGER in
                apt)
                    echo "    sudo apt update && sudo apt install -y ansible"
                    ;;
                yum|dnf)
                    echo "    sudo $PKG_MANAGER install -y ansible"
                    ;;
                pacman)
                    echo "    sudo pacman -S ansible"
                    ;;
            esac
            echo ""
        elif [[ "$OS" == "macos" ]]; then
            echo "Ansible (macOS):"
            echo "  brew install ansible"
            echo ""
        fi
    fi

    log_info "========================================"
}

# 安装 Docker
install_docker() {
    log_info "开始安装 Docker..."

    if [[ "$NETWORK_AVAILABLE" == "false" ]]; then
        show_offline_downloads "docker"
        return 1
    fi

    if [[ "$OS" == "linux" ]]; then
        case $PKG_MANAGER in
            apt)
                log_info "使用 apt 安装 Docker..."
                sudo apt-get update
                sudo apt-get install -y ca-certificates curl gnupg lsb-release
                sudo mkdir -p /etc/apt/keyrings
                curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
                echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
                sudo apt-get update
                sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
                ;;
            yum|dnf)
                log_info "使用 $PKG_MANAGER 安装 Docker..."
                sudo $PKG_MANAGER install -y yum-utils
                sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
                sudo $PKG_MANAGER install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
                ;;
            pacman)
                log_info "使用 pacman 安装 Docker..."
                sudo pacman -S docker docker-compose
                sudo systemctl start docker
                sudo systemctl enable docker
                ;;
            *)
                log_error "不支持的包管理器: $PKG_MANAGER"
                show_offline_downloads "docker"
                return 1
                ;;
        esac

        sudo usermod -aG docker $USER
        log_success "Docker 安装完成！请重新登录以使组权限生效"
    elif [[ "$OS" == "macos" ]]; then
        log_info "在 macOS 上请手动安装 Docker Desktop"
        show_offline_downloads "docker"
        return 1
    fi
}

# 安装 Docker Compose
install_docker_compose() {
    log_info "检查 Docker Compose..."

    if docker compose version &> /dev/null; then
        log_success "Docker Compose 已安装 (Compose Plugin)"
        return 0
    fi

    if command -v docker-compose &> /dev/null; then
        log_success "Docker Compose 已安装 (Standalone)"
        return 0
    fi

    if [[ "$NETWORK_AVAILABLE" == "false" ]]; then
        show_offline_downloads "docker-compose"
        return 1
    fi

    log_info "安装 Docker Compose..."

    if [[ "$OS" == "linux" ]]; then
        local compose_url="https://github.com/docker/compose/releases/download/v2.23.0/docker-compose-linux-${ARCH_SUFFIX}"
        log_info "下载 Docker Compose: $compose_url"
        sudo curl -L "$compose_url" -o /usr/local/bin/docker-compose
        sudo chmod +x /usr/local/bin/docker-compose
        log_success "Docker Compose 安装完成"
    elif [[ "$OS" == "macos" ]]; then
        log_warning "macOS 上 Docker Compose 已包含在 Docker Desktop 中"
        return 1
    fi
}

# 安装 kubectl
install_kubectl() {
    log_info "检查 kubectl..."

    if command -v kubectl &> /dev/null; then
        log_success "kubectl 已安装"
        return 0
    fi

    if [[ "$NETWORK_AVAILABLE" == "false" ]]; then
        show_offline_downloads "kubectl"
        return 1
    fi

    log_info "安装 kubectl..."

    local kubectl_url="https://dl.k8s.io/release/v1.28.4/bin/${OS}/${ARCH_SUFFIX}/kubectl"
    log_info "下载 kubectl: $kubectl_url"
    curl -LO "$kubectl_url"
    sudo chmod +x kubectl
    sudo mv kubectl /usr/local/bin/kubectl
    log_success "kubectl 安装完成"
}

# 安装 Ansible
install_ansible() {
    log_info "检查 Ansible..."

    if command -v ansible-playbook &> /dev/null; then
        log_success "Ansible 已安装"
        return 0
    fi

    if [[ "$NETWORK_AVAILABLE" == "false" ]]; then
        show_offline_downloads "ansible"
        return 1
    fi

    log_info "安装 Ansible..."

    if [[ "$OS" == "linux" ]]; then
        case $PKG_MANAGER in
            apt)
                sudo apt-get update
                sudo apt-get install -y ansible
                ;;
            yum|dnf)
                sudo $PKG_MANAGER install -y ansible
                ;;
            pacman)
                sudo pacman -S ansible
                ;;
            *)
                log_error "不支持的包管理器: $PKG_MANAGER"
                show_offline_downloads "ansible"
                return 1
                ;;
        esac
    elif [[ "$OS" == "macos" ]]; then
        if [[ "$PKG_MANAGER" == "brew" ]]; then
            brew install ansible
        else
            log_error "请先安装 Homebrew: https://brew.sh/"
            show_offline_downloads "ansible"
            return 1
        fi
    fi

    log_success "Ansible 安装完成"
}

# 检查并安装依赖
check_and_install() {
    local deps=$1
    local missing_deps=""
    local install_needed=false

    echo ""
    log_info "========================================"
    log_info "检查依赖环境"
    log_info "========================================"
    echo ""

    for dep in $deps; do
        case $dep in
            docker)
                if ! command -v docker &> /dev/null; then
                    log_error "Docker 未安装"
                    missing_deps="$missing_deps docker"
                    install_needed=true
                else
                    log_success "Docker: $(docker --version)"
                fi
                ;;
            docker-compose)
                if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
                    log_error "Docker Compose 未安装"
                    missing_deps="$missing_deps docker-compose"
                    install_needed=true
                else
                    if command -v docker-compose &> /dev/null; then
                        log_success "Docker Compose: $(docker-compose --version)"
                    else
                        log_success "Docker Compose Plugin: $(docker compose version)"
                    fi
                fi
                ;;
            kubectl)
                if ! command -v kubectl &> /dev/null; then
                    log_error "kubectl 未安装"
                    missing_deps="$missing_deps kubectl"
                    install_needed=true
                else
                    log_success "kubectl: $(kubectl version --client --short 2>/dev/null)"
                fi
                ;;
            ansible)
                if ! command -v ansible-playbook &> /dev/null; then
                    log_error "Ansible 未安装"
                    missing_deps="$missing_deps ansible"
                    install_needed=true
                else
                    log_success "Ansible: $(ansible --version | head -n1)"
                fi
                ;;
        esac
    done

    echo ""

    if [[ "$install_needed" == "false" ]]; then
        log_success "所有依赖已安装！"
        return 0
    fi

    log_warning "缺少以下依赖: $missing_deps"
    check_network

    if [[ "$NETWORK_AVAILABLE" == "true" ]]; then
        read -p "是否自动安装缺失的依赖? [y/N] " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            log_info "取消自动安装"
            show_offline_downloads "$missing_deps"
            return 1
        fi

        for dep in $missing_deps; do
            case $dep in
                docker)
                    install_docker
                    ;;
                docker-compose)
                    install_docker_compose
                    ;;
                kubectl)
                    install_kubectl
                    ;;
                ansible)
                    install_ansible
                    ;;
            esac
        done
    else
        log_warning "网络不可用，无法自动安装"
        show_offline_downloads "$missing_deps"
        return 1
    fi

    log_success "依赖安装完成！"
    return 0
}

# 主函数
main() {
    local deps=""
    local auto_install=false
    local show_downloads=false

    while [[ $# -gt 0 ]]; do
        case $1 in
            --docker)
                deps="$deps docker docker-compose"
                shift
                ;;
            --k8s)
                deps="$deps kubectl"
                shift
                ;;
            --ansible)
                deps="$deps ansible"
                shift
                ;;
            --all)
                deps="docker docker-compose kubectl ansible"
                shift
                ;;
            --auto)
                auto_install=true
                shift
                ;;
            --downloads)
                show_downloads=true
                shift
                ;;
            *)
                log_error "未知参数: $1"
                echo "用法: $0 [--docker|--k8s|--ansible|--all] [--auto] [--downloads]"
                exit 1
                ;;
        esac
    done

    detect_system
    detect_package_manager
    check_network

    if [[ "$show_downloads" == "true" ]]; then
        show_offline_downloads "$deps"
        exit 0
    fi

    check_and_install "$deps"
}

# 如果直接执行此脚本
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
