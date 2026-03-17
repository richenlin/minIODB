# MinIODB Makefile

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X minIODB/pkg/version.version=$(VERSION)"

.PHONY: build build-ui build-go clean test lint vet

# 构建 MinIODB（仅 Go 二进制，前端独立部署）
build:
	go build $(LDFLAGS) -o bin/miniodb ./cmd/

# 仅构建 Go 二进制（别名）
build-go:
	go build $(LDFLAGS) -o bin/miniodb ./cmd/

# 构建前端（独立部署）
build-ui:
	@if [ -d dashboard-ui ]; then \
		echo "Building Dashboard UI..."; \
		cd dashboard-ui && npm run build; \
		echo "Dashboard UI built in dashboard-ui/out/"; \
	else \
		echo "Skipping Dashboard UI build (dashboard-ui/ not found)"; \
	fi

# Docker 构建
docker:
	docker build -f deploy/docker/Dockerfile -t miniodb:$(VERSION) .

# 测试
test:
	go test ./...

# 代码检查
lint:
	golangci-lint run ./...

vet:
	go vet ./...

# 清理
clean:
	rm -rf bin/
