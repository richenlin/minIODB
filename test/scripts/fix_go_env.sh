#!/bin/bash

echo "=== Go 编译环境修复 ==="

# 检查当前Go环境
echo "1. 检查当前Go环境"
echo "Go版本: $(go version)"
echo "GOROOT: $GOROOT"
echo "GOPATH: $GOPATH"
echo "GOPROXY: $GOPROXY"
echo ""

# 修复Go环境配置
echo "2. 修复Go环境配置"
if [ -z "$GOROOT" ]; then
    export GOROOT=$(go env GOROOT)
    echo "设置GOROOT: $GOROOT"
fi

if [ -z "$GOPATH" ]; then
    export GOPATH=$(go env GOPATH)
    echo "设置GOPATH: $GOPATH"
fi

# 设置Go代理（国内用户）
export GOPROXY=https://goproxy.cn,direct
echo "设置GOPROXY: $GOPROXY"

# 清理Go模块缓存
echo "3. 清理Go模块缓存"
go clean -modcache

# 重新下载依赖
echo "4. 重新下载依赖"
cd ../performance
go mod tidy
go mod download

# 编译测试工具
echo "5. 编译测试工具"
cd tools
echo "编译 data_generator..."
go build -o data_generator data_generator.go
echo "编译 jwt_generator..."
go build -o jwt_generator jwt_generator.go

echo "=== Go 环境修复完成 ==="
echo "测试工具编译状态："
ls -la data_generator jwt_generator 