#!/bin/bash

# MinIODB客户端示例运行脚本
# 用于快速测试所有语言的客户端示例

set -e

echo "🚀 MinIODB客户端示例运行脚本"
echo "================================"

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 检查MinIODB服务器是否运行
check_server() {
    echo -e "${BLUE}检查MinIODB服务器状态...${NC}"
    
    # 检查gRPC服务器
    if ! nc -z localhost 8080 2>/dev/null; then
        echo -e "${RED}❌ gRPC服务器 (localhost:8080) 未运行${NC}"
        echo "请先启动MinIODB服务器: go run cmd/server/main.go"
        exit 1
    fi
    
    # 检查REST服务器
    if ! curl -s http://localhost:8081/v1/health >/dev/null 2>&1; then
        echo -e "${RED}❌ REST服务器 (localhost:8081) 未运行${NC}"
        echo "请先启动MinIODB服务器: go run cmd/server/main.go"
        exit 1
    fi
    
    echo -e "${GREEN}✅ MinIODB服务器正在运行${NC}"
}

# 运行Go示例
run_go_example() {
    echo -e "${BLUE}运行Go客户端示例...${NC}"
    
    if [ ! -d "go" ]; then
        echo -e "${YELLOW}⚠️  Go示例目录不存在${NC}"
        return
    fi
    
    cd go
    
    # 检查go.mod文件
    if [ ! -f "go.mod" ]; then
        echo -e "${RED}❌ go.mod文件不存在${NC}"
        cd ..
        return
    fi
    
    # 下载依赖
    echo "下载Go依赖..."
    go mod tidy
    
    # 运行REST客户端示例
    echo "运行Go REST客户端示例..."
    if go run rest_client_example.go; then
        echo -e "${GREEN}✅ Go REST客户端示例运行成功${NC}"
    else
        echo -e "${RED}❌ Go REST客户端示例运行失败${NC}"
    fi
    
    cd ..
}

# 运行Node.js示例
run_node_example() {
    echo -e "${BLUE}运行Node.js客户端示例...${NC}"
    
    if [ ! -d "node" ]; then
        echo -e "${YELLOW}⚠️  Node.js示例目录不存在${NC}"
        return
    fi
    
    cd node
    
    # 检查package.json文件
    if [ ! -f "package.json" ]; then
        echo -e "${RED}❌ package.json文件不存在${NC}"
        cd ..
        return
    fi
    
    # 检查node和npm
    if ! command -v node &> /dev/null; then
        echo -e "${RED}❌ Node.js未安装${NC}"
        cd ..
        return
    fi
    
    if ! command -v npm &> /dev/null; then
        echo -e "${RED}❌ npm未安装${NC}"
        cd ..
        return
    fi
    
    # 安装依赖
    echo "安装Node.js依赖..."
    npm install
    
    # 运行REST客户端示例
    echo "运行Node.js REST客户端示例..."
    if npm run rest-example; then
        echo -e "${GREEN}✅ Node.js REST客户端示例运行成功${NC}"
    else
        echo -e "${RED}❌ Node.js REST客户端示例运行失败${NC}"
    fi
    
    cd ..
}

# 运行Java示例
run_java_example() {
    echo -e "${BLUE}运行Java客户端示例...${NC}"
    
    if [ ! -d "java" ]; then
        echo -e "${YELLOW}⚠️  Java示例目录不存在${NC}"
        return
    fi
    
    cd java
    
    # 检查pom.xml文件
    if [ ! -f "pom.xml" ]; then
        echo -e "${RED}❌ pom.xml文件不存在${NC}"
        cd ..
        return
    fi
    
    # 检查Maven
    if ! command -v mvn &> /dev/null; then
        echo -e "${RED}❌ Maven未安装${NC}"
        cd ..
        return
    fi
    
    # 编译项目
    echo "编译Java项目..."
    mvn clean compile
    
    # 运行REST客户端示例
    echo "运行Java REST客户端示例..."
    if mvn exec:java -Dexec.mainClass="com.miniodb.examples.RestClientExample" -q; then
        echo -e "${GREEN}✅ Java REST客户端示例运行成功${NC}"
    else
        echo -e "${RED}❌ Java REST客户端示例运行失败${NC}"
    fi
    
    cd ..
}

# 显示帮助信息
show_help() {
    echo "用法: $0 [选项]"
    echo
    echo "选项:"
    echo "  --all, -a     运行所有语言的示例"
    echo "  --go, -g      仅运行Go示例"
    echo "  --node, -n    仅运行Node.js示例"
    echo "  --java, -j    仅运行Java示例"
    echo "  --check, -c   仅检查服务器状态"
    echo "  --help, -h    显示帮助信息"
    echo
    echo "示例:"
    echo "  $0 --all      # 运行所有示例"
    echo "  $0 --go       # 仅运行Go示例"
    echo "  $0 --check    # 检查服务器状态"
}

# 主函数
main() {
    # 切换到examples目录
    cd "$(dirname "$0")"
    
    case "$1" in
        --all|-a)
            check_server
            run_go_example
            echo
            run_node_example
            echo
            run_java_example
            ;;
        --go|-g)
            check_server
            run_go_example
            ;;
        --node|-n)
            check_server
            run_node_example
            ;;
        --java|-j)
            check_server
            run_java_example
            ;;
        --check|-c)
            check_server
            ;;
        --help|-h)
            show_help
            ;;
        "")
            echo -e "${YELLOW}请指定要运行的示例类型，使用 --help 查看帮助${NC}"
            show_help
            ;;
        *)
            echo -e "${RED}未知选项: $1${NC}"
            show_help
            exit 1
            ;;
    esac
}

# 运行主函数
main "$@"

echo
echo -e "${GREEN}🎉 示例运行完成！${NC}" 