#!/bin/bash

# Java gRPC 代码生成脚本
# 此脚本用于从 proto 文件生成 Java gRPC 客户端代码

set -e

# 脚本目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
PROTO_DIR="$PROJECT_ROOT/proto"
JAVA_DIR="$PROJECT_ROOT/java"

echo "=== MinIODB Java gRPC 代码生成 ==="
echo "项目根目录: $PROJECT_ROOT"
echo "Proto 目录: $PROTO_DIR"
echo "Java 目录: $JAVA_DIR"

# 检查必要的工具
command -v protoc >/dev/null 2>&1 || { echo "错误: protoc 未安装" >&2; exit 1; }
command -v mvn >/dev/null 2>&1 || { echo "错误: Maven 未安装" >&2; exit 1; }

# 检查 proto 文件是否存在
if [ ! -f "$PROTO_DIR/miniodb/v1/miniodb.proto" ]; then
    echo "错误: proto 文件不存在: $PROTO_DIR/miniodb/v1/miniodb.proto"
    exit 1
fi

# 创建输出目录
mkdir -p "$JAVA_DIR/src/main/java"

# 生成 Java gRPC 代码
echo "正在生成 Java gRPC 代码..."
protoc \
    --proto_path="$PROTO_DIR" \
    --java_out="$JAVA_DIR/src/main/java" \
    --grpc-java_out="$JAVA_DIR/src/main/java" \
    --plugin=protoc-gen-grpc-java="$(which protoc-gen-grpc-java)" \
    "$PROTO_DIR/miniodb/v1/miniodb.proto"

if [ $? -eq 0 ]; then
    echo "✅ Java gRPC 代码生成成功"
    echo "生成的文件位于: $JAVA_DIR/src/main/java"
else
    echo "❌ Java gRPC 代码生成失败"
    exit 1
fi

# 如果存在 pom.xml，尝试编译
if [ -f "$JAVA_DIR/pom.xml" ]; then
    echo "正在编译 Java 项目..."
    cd "$JAVA_DIR"
    mvn compile
    if [ $? -eq 0 ]; then
        echo "✅ Java 项目编译成功"
    else
        echo "⚠️  Java 项目编译失败，但代码生成成功"
    fi
fi

echo "=== Java gRPC 代码生成完成 ==="
