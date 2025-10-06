#!/bin/bash

# Node.js TypeScript gRPC 代码生成脚本
# 此脚本用于从 proto 文件生成 Node.js gRPC 客户端代码

set -e

# 脚本目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
PROTO_DIR="$PROJECT_ROOT/../proto"
OUTPUT_DIR="$PROJECT_ROOT/src/proto"

echo "=== MinIODB Node.js gRPC 代码生成 ==="
echo "项目根目录: $PROJECT_ROOT"
echo "Proto 目录: $PROTO_DIR"  
echo "输出目录: $OUTPUT_DIR"

# 检查必要的工具
echo "检查依赖..."

# 检查 Node.js
command -v node >/dev/null 2>&1 || { echo "错误: Node.js 未安装" >&2; exit 1; }
echo "✅ Node.js 可用"

# 检查 npm
command -v npm >/dev/null 2>&1 || { echo "错误: npm 未安装" >&2; exit 1; }
echo "✅ npm 可用"

# 进入项目目录
cd "$PROJECT_ROOT"

# 检查 package.json 是否存在
if [ ! -f "package.json" ]; then
    echo "错误: package.json 不存在"
    exit 1
fi

# 安装依赖（如果需要）
if [ ! -d "node_modules" ]; then
    echo "安装项目依赖..."
    npm install
fi

# 检查 proto 文件是否存在
if [ ! -f "$PROTO_DIR/miniodb/v1/miniodb.proto" ]; then
    echo "错误: proto 文件不存在: $PROTO_DIR/miniodb/v1/miniodb.proto"
    exit 1
fi
echo "✅ Proto 文件存在"

# 创建输出目录
mkdir -p "$OUTPUT_DIR"

# 检查 grpc-tools 是否可用
if ! npx grpc_tools_node_protoc --version >/dev/null 2>&1; then
    echo "安装 grpc-tools..."
    npm install --save-dev grpc-tools
fi

# 生成 JavaScript 代码
echo "正在生成 JavaScript gRPC 代码..."
npx grpc_tools_node_protoc \
    --proto_path="$PROTO_DIR" \
    --js_out=import_style=commonjs,binary:"$OUTPUT_DIR" \
    --grpc_out=grpc_js:"$OUTPUT_DIR" \
    "$PROTO_DIR/miniodb/v1/miniodb.proto"

if [ $? -eq 0 ]; then
    echo "✅ JavaScript gRPC 代码生成成功"
else
    echo "❌ JavaScript gRPC 代码生成失败"
    exit 1
fi

# 检查 TypeScript 定义生成工具
if ! npx grpc_tools_node_protoc_ts --version >/dev/null 2>&1; then
    echo "安装 grpc_tools_node_protoc_ts..."
    npm install --save-dev grpc_tools_node_protoc_ts
fi

# 生成 TypeScript 类型定义
echo "正在生成 TypeScript 类型定义..."
npx grpc_tools_node_protoc_ts \
    --proto_path="$PROTO_DIR" \
    --ts_out=grpc_js:"$OUTPUT_DIR" \
    "$PROTO_DIR/miniodb/v1/miniodb.proto"

if [ $? -eq 0 ]; then
    echo "✅ TypeScript 类型定义生成成功"
else
    echo "❌ TypeScript 类型定义生成失败"
    exit 1
fi

# 创建索引文件
echo "创建索引文件..."
cat > "$OUTPUT_DIR/index.ts" << 'EOF'
/**
 * MinIODB gRPC 生成代码索引文件
 * 
 * 此文件导出生成的 gRPC 客户端和类型定义。
 */

// 导出生成的 gRPC 代码
export * from './miniodb/v1/miniodb_pb';
export * from './miniodb/v1/miniodb_grpc_pb';

// 注意：实际的文件名可能根据生成工具而有所不同
// 请根据实际生成的文件调整导出路径
EOF

echo "✅ 索引文件创建成功"

# 编译 TypeScript（如果存在 tsconfig.json）
if [ -f "tsconfig.json" ]; then
    echo "编译 TypeScript 代码..."
    npm run build
    if [ $? -eq 0 ]; then
        echo "✅ TypeScript 编译成功"
    else
        echo "⚠️  TypeScript 编译失败，但代码生成成功"
    fi
fi

echo ""
echo "=== Node.js gRPC 代码生成完成 ==="
echo "生成的文件位于: $OUTPUT_DIR"
echo ""
echo "下一步:"
echo "1. 运行 npm run build 编译 TypeScript 代码"
echo "2. 在客户端代码中使用生成的 gRPC 类型"
echo "3. 实现完整的客户端功能"
