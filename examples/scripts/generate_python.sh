#!/bin/bash

# Python gRPC 代码生成脚本
# 此脚本用于从 proto 文件生成 Python gRPC 客户端代码

set -e

# 脚本目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
PROTO_DIR="$PROJECT_ROOT/proto"
PYTHON_DIR="$PROJECT_ROOT/python"

echo "=== MinIODB Python gRPC 代码生成 ==="
echo "项目根目录: $PROJECT_ROOT"
echo "Proto 目录: $PROTO_DIR"
echo "Python 目录: $PYTHON_DIR"

# 检查必要的工具
command -v python3 >/dev/null 2>&1 || { echo "错误: Python3 未安装" >&2; exit 1; }
command -v protoc >/dev/null 2>&1 || { echo "错误: protoc 未安装" >&2; exit 1; }

# 检查 Python gRPC 工具
python3 -c "import grpc_tools.protoc" 2>/dev/null || {
    echo "错误: grpcio-tools 未安装，请运行: pip install grpcio-tools"
    exit 1
}

# 检查 proto 文件是否存在
if [ ! -f "$PROTO_DIR/miniodb/v1/miniodb.proto" ]; then
    echo "错误: proto 文件不存在: $PROTO_DIR/miniodb/v1/miniodb.proto"
    exit 1
fi

# 创建输出目录
mkdir -p "$PYTHON_DIR/miniodb_sdk/proto"

# 生成 Python gRPC 代码
echo "正在生成 Python gRPC 代码..."
python3 -m grpc_tools.protoc \
    --proto_path="$PROTO_DIR" \
    --python_out="$PYTHON_DIR/miniodb_sdk/proto" \
    --grpc_python_out="$PYTHON_DIR/miniodb_sdk/proto" \
    "$PROTO_DIR/miniodb/v1/miniodb.proto"

if [ $? -eq 0 ]; then
    echo "✅ Python gRPC 代码生成成功"
    echo "生成的文件位于: $PYTHON_DIR/miniodb_sdk/proto"
else
    echo "❌ Python gRPC 代码生成失败"
    exit 1
fi

# 创建 __init__.py 文件
touch "$PYTHON_DIR/miniodb_sdk/__init__.py"
touch "$PYTHON_DIR/miniodb_sdk/proto/__init__.py"

# 修复导入路径（Python gRPC 的常见问题）
echo "正在修复 Python 导入路径..."
if [ -f "$PYTHON_DIR/miniodb_sdk/proto/miniodb/v1/miniodb_pb2_grpc.py" ]; then
    sed -i 's/import miniodb_pb2/from . import miniodb_pb2/g' \
        "$PYTHON_DIR/miniodb_sdk/proto/miniodb/v1/miniodb_pb2_grpc.py"
fi

# 如果存在 requirements.txt，尝试安装依赖
if [ -f "$PYTHON_DIR/requirements.txt" ]; then
    echo "正在安装 Python 依赖..."
    cd "$PYTHON_DIR"
    pip3 install -r requirements.txt
    if [ $? -eq 0 ]; then
        echo "✅ Python 依赖安装成功"
    else
        echo "⚠️  Python 依赖安装失败，但代码生成成功"
    fi
fi

echo "=== Python gRPC 代码生成完成 ==="
