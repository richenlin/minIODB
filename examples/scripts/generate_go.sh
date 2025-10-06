#!/bin/bash

# Go gRPC 代码生成脚本
# 此脚本用于从 proto 文件生成 Go gRPC 客户端代码

set -e

# 脚本目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
PROTO_DIR="$PROJECT_ROOT/proto"
GO_DIR="$PROJECT_ROOT/golang"

echo "=== MinIODB Go gRPC 代码生成 ==="
echo "项目根目录: $PROJECT_ROOT"
echo "Proto 目录: $PROTO_DIR"
echo "Go 目录: $GO_DIR"

# 检查必要的工具
command -v go >/dev/null 2>&1 || { echo "错误: Go 未安装" >&2; exit 1; }
command -v protoc >/dev/null 2>&1 || { echo "错误: protoc 未安装" >&2; exit 1; }

# 检查 Go protobuf 插件
command -v protoc-gen-go >/dev/null 2>&1 || {
    echo "错误: protoc-gen-go 未安装，请运行: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
    exit 1
}

command -v protoc-gen-go-grpc >/dev/null 2>&1 || {
    echo "错误: protoc-gen-go-grpc 未安装，请运行: go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
    exit 1
}

# 检查 proto 文件是否存在
if [ ! -f "$PROTO_DIR/miniodb/v1/miniodb.proto" ]; then
    echo "错误: proto 文件不存在: $PROTO_DIR/miniodb/v1/miniodb.proto"
    exit 1
fi

# 创建输出目录
mkdir -p "$GO_DIR/proto/miniodb/v1"

# 生成 Go gRPC 代码
echo "正在生成 Go gRPC 代码..."
protoc \
    --proto_path="$PROTO_DIR" \
    --go_out="$GO_DIR/proto" \
    --go_opt=paths=source_relative \
    --go-grpc_out="$GO_DIR/proto" \
    --go-grpc_opt=paths=source_relative \
    "$PROTO_DIR/miniodb/v1/miniodb.proto"

if [ $? -eq 0 ]; then
    echo "✅ Go gRPC 代码生成成功"
    echo "生成的文件位于: $GO_DIR/proto"
else
    echo "❌ Go gRPC 代码生成失败"
    exit 1
fi

# 如果存在 go.mod，尝试整理依赖
if [ -f "$GO_DIR/go.mod" ]; then
    echo "正在整理 Go 依赖..."
    cd "$GO_DIR"
    go mod tidy
    if [ $? -eq 0 ]; then
        echo "✅ Go 依赖整理成功"
    else
        echo "⚠️  Go 依赖整理失败，但代码生成成功"
    fi
fi

echo "=== Go gRPC 代码生成完成 ==="
