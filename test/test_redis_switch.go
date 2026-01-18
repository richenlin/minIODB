package main

import (
	"minIODB/pkg/logger"

	"minIODB/config"
	"minIODB/internal/discovery"

	"go.uber.org/zap"
)

func main() {
	logger.Logger.Info("=== Redis开关功能测试 ===")

	// 测试1: Redis启用的情况
	logger.Logger.Info("\n1. 测试Redis启用的情况:")
	testRedisEnabled()

	// 测试2: Redis禁用的情况
	logger.Logger.Info("\n2. 测试Redis禁用的情况:")
	testRedisDisabled()

	logger.Logger.Info("\n=== 测试完成 ===")
}

func testRedisEnabled() {
	cfg := config.Config{
		Server: config.ServerConfig{
			NodeID:   "test-node-1",
			GrpcPort: "50051",
			RestPort: "8080",
		},
		Redis: config.RedisConfig{
			Enabled:  true,
			Mode:     "standalone",
			Addr:     "localhost:6379",
			Password: "",
			DB:       0,
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled:  true,
					Mode:     "standalone",
					Addr:     "localhost:6379",
					Password: "",
					DB:       0,
				},
			},
		},
	}

	registry, err := discovery.NewServiceRegistry(cfg, "test-node-1", "50051", logger.GetLogger())
	if err != nil {
		logger.Logger.Error("  ❌ 创建服务注册器失败", zap.Error(err))
		return
	}

	logger.Logger.Info("  ✅ 服务注册器创建成功（Redis启用）")

	// 启动服务注册
	if err := registry.Start(); err != nil {
		logger.Logger.Error("  ❌ 启动服务注册失败", zap.Error(err))
		return
	}

	logger.Logger.Info("  ✅ 服务注册启动成功")

	// 测试服务发现
	services, err := registry.DiscoverNodes()
	if err != nil {
		logger.Logger.Error("  ❌ 服务发现失败", zap.Error(err))
	} else {
		logger.Logger.Info("  ✅ 发现服务数量", zap.Int("count", len(services)))
		for _, service := range services {
			logger.Logger.Info("    - 节点ID", zap.String("node_id", service.NodeID), zap.String("address", service.Address), zap.String("status", service.Status))
		}
	}

	// 停止服务注册
	registry.Stop()
	logger.Logger.Info("  ✅ 服务注册已停止")
}

func testRedisDisabled() {
	cfg := config.Config{
		Server: config.ServerConfig{
			NodeID:   "test-node-2",
			GrpcPort: "50052",
			RestPort: "8081",
		},
		Redis: config.RedisConfig{
			Enabled:  false, // 禁用Redis
			Mode:     "standalone",
			Addr:     "localhost:6379",
			Password: "",
			DB:       0,
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled:  false, // 禁用Redis
					Mode:     "standalone",
					Addr:     "localhost:6379",
					Password: "",
					DB:       0,
				},
			},
		},
	}

	registry, err := discovery.NewServiceRegistry(cfg, "test-node-2", "50052", logger.GetLogger())
	if err != nil {
		logger.Logger.Error("  ❌ 创建服务注册器失败", zap.Error(err))
		return
	}

	logger.Logger.Info("  ✅ 服务注册器创建成功（Redis禁用）")

	// 启动服务注册
	if err := registry.Start(); err != nil {
		logger.Logger.Error("  ❌ 启动服务注册失败", zap.Error(err))
		return
	}

	logger.Logger.Info("  ✅ 服务注册启动成功（单节点模式）")

	// 测试服务发现
	services, err := registry.DiscoverNodes()
	if err != nil {
		logger.Logger.Error("  ❌ 服务发现失败", zap.Error(err))
	} else {
		logger.Logger.Info("  ✅ 发现服务数量", zap.Int("count", len(services)))
		for _, service := range services {
			logger.Logger.Info("    - 节点ID", zap.String("node_id", service.NodeID), zap.String("address", service.Address), zap.String("status", service.Status), zap.String("mode", service.Metadata["mode"]))
		}
	}

	// 测试节点健康检查
	isHealthy := registry.IsNodeHealthy("test-node-2")
	logger.Logger.Info("  ✅ 节点健康状态", zap.Bool("is_healthy", isHealthy))

	// 测试获取节点（一致性哈希）
	nodeID := registry.GetNodeForKey("test-key")
	logger.Logger.Info("  ✅ 键 'test-key' 分配到节点", zap.String("node_id", nodeID))

	// 停止服务注册
	registry.Stop()
	logger.Logger.Info("  ✅ 服务注册已停止")
}
