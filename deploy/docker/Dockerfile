# AMD64架构专用Dockerfile
# 支持 All-in-One 模式（包含 Dashboard 前端）

# Stage 1: Build Next.js SPA (Dashboard UI)
FROM node:22-alpine AS ui-builder
WORKDIR /app/dashboard-ui
COPY dashboard-ui/package*.json ./
RUN npm ci --registry=https://registry.npmmirror.com
COPY dashboard-ui/ ./
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.24 AS builder

# 设置工作目录
WORKDIR /app

# 更新包管理器并安装必要的系统依赖
RUN apt-get update && apt-get install -y \
    git \
    ca-certificates \
    tzdata \
    gcc \
    g++ \
    libc6-dev \
    pkg-config \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# 设置环境变量
ENV CGO_ENABLED=1
ENV GOOS=linux
ENV GOARCH=amd64
ENV GOPROXY='https://goproxy.cn,direct'

# 复制go.mod和go.sum文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 复制前端构建产物到 internal/dashboard/static（供 embed.FS 嵌入）
COPY --from=ui-builder /app/dashboard-ui/out /app/dashboard-ui/out
RUN rm -rf internal/dashboard/static && cp -r dashboard-ui/out internal/dashboard/static

# 构建应用（带 dashboard build tag，支持 Dashboard UI）
RUN go build -tags dashboard -o miniodb cmd/main.go

# Stage 3: Runtime
FROM ubuntu:24.04

# 避免 apt/tzdata 交互式提示
ENV DEBIAN_FRONTEND=noninteractive

# 安装运行时依赖
RUN apt-get update && apt-get install -y \
    ca-certificates \
    tzdata \
    curl \
    && rm -rf /var/lib/apt/lists/*

# 设置时区
ENV TZ=Asia/Shanghai
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

# 创建非root用户
RUN groupadd -g 1001 miniodb && \
    useradd -r -u 1001 -g miniodb miniodb

# 设置工作目录
WORKDIR /app

# 创建必要的目录
RUN mkdir -p /app/logs /app/config /app/certs && \
    chown -R miniodb:miniodb /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/miniodb /app/miniodb

# 复制配置文件（如果存在）
COPY --from=builder --chown=miniodb:miniodb /app/config/config.yaml /app/config/config.yaml

# 设置权限
RUN chmod +x /app/miniodb

# 切换到非root用户
USER miniodb

# 暴露端口
# 8080: gRPC API
# 8081: REST API
# 9090: Dashboard UI
EXPOSE 8080 8081 9090

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8081/v1/health || exit 1

# 设置启动命令
ENTRYPOINT ["/app/miniodb"]
