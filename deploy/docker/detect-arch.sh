#!/bin/bash

# MinIODB 架构检测脚本
# 根据主机架构自动选择合适的Dockerfile和镜像标签

set -e

# 获取系统架构
ARCH=$(uname -m)

# 根据架构设置环境变量
case $ARCH in
    x86_64)
        echo "检测到 AMD64 架构"
        export DOCKERFILE="Dockerfile"
        export PLATFORM_TAG="amd64"
        export BUILD_PLATFORM="linux/amd64"
        ;;
    arm64|aarch64)
        echo "检测到 ARM64 架构"
        export DOCKERFILE="Dockerfile.arm"
        export PLATFORM_TAG="arm64"
        export BUILD_PLATFORM="linux/arm64"
        ;;
    *)
        echo "不支持的架构: $ARCH"
        echo "支持的架构: x86_64, arm64, aarch64"
        exit 1
        ;;
esac

# 显示检测结果
echo "架构信息:"
echo "  系统架构: $ARCH"
echo "  Dockerfile: $DOCKERFILE"
echo "  镜像标签: $PLATFORM_TAG"
echo "  构建平台: $BUILD_PLATFORM"

# 导出环境变量到 .env.arch 文件
cat > .env.arch << EOF
# 自动生成的架构配置文件
# 由 detect-arch.sh 脚本生成，请勿手动修改

DOCKERFILE=$DOCKERFILE
PLATFORM_TAG=$PLATFORM_TAG
BUILD_PLATFORM=$BUILD_PLATFORM
ARCH=$ARCH
EOF

echo "架构配置已保存到 .env.arch 文件" 