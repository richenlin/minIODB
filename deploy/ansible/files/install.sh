#!/bin/bash

# MinIODB离线安装脚本
set -e

ARCH=$(uname -m)
case $ARCH in
    x86_64) ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

INSTALL_DIR="/opt/miniodb"
CONFIG_DIR="/etc/miniodb"
LOG_DIR="/var/log/miniodb"
DATA_DIR="/opt/miniodb/data"

echo "Installing MinIODB for $ARCH..."

# 创建用户和目录
useradd -r -s /bin/false minio || true
useradd -r -s /bin/false redis || true
useradd -r -s /bin/false miniodb || true

mkdir -p $INSTALL_DIR/bin
mkdir -p $INSTALL_DIR/config
mkdir -p $CONFIG_DIR
mkdir -p $LOG_DIR
mkdir -p $DATA_DIR/{minio,redis}

# 安装二进制文件
cp $ARCH/bin/* $INSTALL_DIR/bin/
chmod +x $INSTALL_DIR/bin/*

# 安装配置文件
cp configs/* $INSTALL_DIR/config/

# 设置权限
chown -R minio:minio $DATA_DIR/minio
chown -R redis:redis $DATA_DIR/redis
chown -R miniodb:miniodb $INSTALL_DIR
chown -R miniodb:miniodb $LOG_DIR

echo "MinIODB installed successfully!"
echo "Please configure the services and start them." 