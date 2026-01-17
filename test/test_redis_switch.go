package main

import (
	"fmt"
	"log"

	"minIODB/config"
	"minIODB/internal/discovery"
)

func main() {
	fmt.Println("=== Redis开关功能测试 ===")

	// 测试1: Redis启用的情况
	fmt.Println("\n1. 测试Redis启用的情况:")
	testRedisEnabled()

	// 测试2: Redis禁用的情况
	fmt.Println("\n2. 测试Redis禁用的情况:")
	testRedisDisabled()

	fmt.Println("\n=== 测试完成 ===")
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

	registry, err := discovery.NewServiceRegistry(cfg, "test-node-1", "50051")
	if err != nil {
		log.Printf("  ❌ 创建服务注册器失败: %v", err)
		return
	}

	fmt.Println("  ✅ 服务注册器创建成功（Redis启用）")

	// 启动服务注册
	if err := registry.Start(); err != nil {
		log.Printf("  ❌ 启动服务注册失败: %v", err)
		return
	}

	fmt.Println("  ✅ 服务注册启动成功")

	// 测试服务发现
	services, err := registry.DiscoverNodes()
	if err != nil {
		log.Printf("  ❌ 服务发现失败: %v", err)
	} else {
		fmt.Printf("  ✅ 发现服务数量: %d\n", len(services))
		for _, service := range services {
			fmt.Printf("    - 节点ID: %s, 地址: %s, 状态: %s\n",
				service.NodeID, service.Address, service.Status)
		}
	}

	// 停止服务注册
	registry.Stop()
	fmt.Println("  ✅ 服务注册已停止")
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

	registry, err := discovery.NewServiceRegistry(cfg, "test-node-2", "50052")
	if err != nil {
		log.Printf("  ❌ 创建服务注册器失败: %v", err)
		return
	}

	fmt.Println("  ✅ 服务注册器创建成功（Redis禁用）")

	// 启动服务注册
	if err := registry.Start(); err != nil {
		log.Printf("  ❌ 启动服务注册失败: %v", err)
		return
	}

	fmt.Println("  ✅ 服务注册启动成功（单节点模式）")

	// 测试服务发现
	services, err := registry.DiscoverNodes()
	if err != nil {
		log.Printf("  ❌ 服务发现失败: %v", err)
	} else {
		fmt.Printf("  ✅ 发现服务数量: %d（单节点模式）\n", len(services))
		for _, service := range services {
			fmt.Printf("    - 节点ID: %s, 地址: %s, 状态: %s, 模式: %s\n",
				service.NodeID, service.Address, service.Status, service.Metadata["mode"])
		}
	}

	// 测试节点健康检查
	isHealthy := registry.IsNodeHealthy("test-node-2")
	fmt.Printf("  ✅ 节点健康状态: %v\n", isHealthy)

	// 测试获取节点（一致性哈希）
	nodeID := registry.GetNodeForKey("test-key")
	fmt.Printf("  ✅ 键 'test-key' 分配到节点: %s\n", nodeID)

	// 停止服务注册
	registry.Stop()
	fmt.Println("  ✅ 服务注册已停止")
}
