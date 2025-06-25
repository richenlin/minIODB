# 使用官方Go镜像作为构建环境
FROM golang:1.24-alpine AS builder

# 设置工作目录
WORKDIR /app

# 安装必要的系统依赖
RUN apk add --no-cache \
    git \
    ca-certificates \
    tzdata \
    gcc \
    musl-dev \
    sqlite-dev

# 复制go.mod和go.sum文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o miniodb cmd/server/main.go

# 使用Alpine Linux作为运行时镜像
FROM alpine:latest

# 安装运行时依赖
RUN apk --no-cache add \
    ca-certificates \
    tzdata \
    curl \
    sqlite

# 设置时区
ENV TZ=Asia/Shanghai

# 创建非root用户
RUN addgroup -g 1001 miniodb && \
    adduser -D -s /bin/sh -u 1001 -G miniodb miniodb

# 设置工作目录
WORKDIR /app

# 创建必要的目录
RUN mkdir -p /app/logs /app/config /app/certs && \
    chown -R miniodb:miniodb /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/miniodb /app/miniodb

# 复制配置文件
COPY --chown=miniodb:miniodb config.yaml /app/config/

# 设置权限
RUN chmod +x /app/miniodb

# 切换到非root用户
USER miniodb

# 暴露端口
EXPOSE 8080 8081 9090

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8081/v1/health || exit 1

# 设置启动命令
ENTRYPOINT ["/app/miniodb"] 