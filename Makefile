.PHONY: build test lint vet clean docker docker-arm swagger proto test-coverage \
	dashboard-ui dashboard build-with-dashboard docker-dashboard docker-allinone

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS = -ldflags "-X 'minIODB/pkg/version.version=$(VERSION)' -X 'minIODB/pkg/version.gitCommit=$(GIT_COMMIT)' -X 'minIODB/pkg/version.buildTime=$(BUILD_TIME)'"

build:
	go build $(LDFLAGS) -o bin/miniodb ./cmd/

test:
	go test ./... -race -count=1

test-coverage:
	go test ./... -race -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

vet:
	go vet ./...

lint:
	golangci-lint run

clean:
	rm -rf bin/ coverage.out coverage.html

docker:
	docker build -f deploy/docker/Dockerfile -t miniodb:$(VERSION) .

docker-arm:
	docker build -f deploy/docker/Dockerfile.arm -t miniodb:$(VERSION)-arm .

swagger:
	swag init -g cmd/main.go -o docs

proto:
	protoc --go_out=. --go-grpc_out=. api/proto/miniodb/v1/miniodb.proto

# Dashboard targets
dashboard-ui:
	cd dashboard-ui && npm ci && npm run build
	rm -rf internal/dashboard/static
	cp -r dashboard-ui/out internal/dashboard/static

# Dashboard 已完全分离，仅独立部署
dashboard: dashboard-ui
	go build -tags dashboard $(LDFLAGS) -o bin/miniodb-dashboard ./cmd/dashboard/

# 兼容旧 target 名称，等同于 dashboard
build-with-dashboard: dashboard

docker-dashboard:
	docker build -f deploy/docker/Dockerfile.dashboard -t miniodb-dashboard:$(VERSION) .

docker-allinone: 
	docker build -f deploy/docker/Dockerfile --build-arg BUILD_TAGS=dashboard -t miniodb:$(VERSION)-allinone .
