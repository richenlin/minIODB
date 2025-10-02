package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/pool"
	"minIODB/pkg/consistenthash"

	"github.com/go-redis/redis/v8"
)

const (
	// Redis keys
	ServiceRegistryKey = "nodes:services"    // 服务注册表
	HashRingKey        = "cluster:hash_ring" // 一致性哈希环
	NodeHealthKey      = "nodes:health:%s"   // 节点健康状态

	// Default values
	DefaultHeartbeatInterval = 30 * time.Second // 心跳间隔
	DefaultNodeTTL           = 60 * time.Second // 节点TTL
	DefaultReplicas          = 150              // 一致性哈希虚拟节点数
)

// NodeInfo 节点信息
type NodeInfo struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	Port     string    `json:"port"`
	Status   string    `json:"status"`
	LastSeen time.Time `json:"last_seen"`
}

// ServiceInfo 服务信息
type ServiceInfo struct {
	NodeID   string            `json:"node_id"`
	Address  string            `json:"address"`
	Port     string            `json:"port"`
	Status   string            `json:"status"`
	LastSeen time.Time         `json:"last_seen"`
	Metadata map[string]string `json:"metadata"`
}

// HealthChecker 健康检查器
type HealthChecker struct {
	registry *ServiceRegistry
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(registry *ServiceRegistry) *HealthChecker {
	return &HealthChecker{
		registry: registry,
	}
}

// Start 启动健康检查
func (hc *HealthChecker) Start() {
	// 简单的健康检查实现
	ticker := time.NewTicker(DefaultHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 执行健康检查逻辑
			if hc.registry.redisEnabled {
				if err := hc.registry.sendHeartbeat(); err != nil {
					log.Printf("Health check failed: %v", err)
				}
			}
		case <-hc.registry.stopChan:
			return
		}
	}
}

// ServiceRegistry 服务注册器
type ServiceRegistry struct {
	config    config.Config
	nodeID    string
	grpcPort  string
	redisPool *pool.RedisPool

	// 服务发现相关
	services map[string]*ServiceInfo
	mutex    sync.RWMutex

	// 健康检查
	healthChecker *HealthChecker

	// 停止信号
	stopChan  chan struct{}
	isRunning bool

	// Redis状态
	redisEnabled bool

	// 一致性哈希环
	hashRing *consistenthash.ConsistentHash

	// 节点信息
	nodeInfo *NodeInfo
}

// NewServiceRegistry 创建服务注册器
func NewServiceRegistry(cfg config.Config, nodeID, grpcPort string) (*ServiceRegistry, error) {
	// 检查Redis是否启用 - 优先使用Network.Pools.Redis配置
	redisEnabled := cfg.Network.Pools.Redis.Enabled
	if !redisEnabled {
		// 如果Network配置中没有启用，检查旧的Redis配置
		redisEnabled = cfg.Redis.Enabled
	}

	// 声明redisConfig变量
	var redisConfig config.EnhancedRedisConfig

	var redisPool *pool.RedisPool
	if redisEnabled {
		// 使用增强的Redis配置
		redisConfig = cfg.Network.Pools.Redis

		// 如果增强配置为空，使用旧配置
		if redisConfig.Addr == "" {
			redisConfig.Addr = cfg.Redis.Addr
			redisConfig.Password = cfg.Redis.Password
			redisConfig.DB = cfg.Redis.DB
			redisConfig.Mode = cfg.Redis.Mode
			redisConfig.PoolSize = cfg.Redis.PoolSize
			redisConfig.MinIdleConns = cfg.Redis.MinIdleConns
			redisConfig.MaxConnAge = cfg.Redis.MaxConnAge
			redisConfig.PoolTimeout = cfg.Redis.PoolTimeout
			redisConfig.IdleTimeout = cfg.Redis.IdleTimeout
			redisConfig.DialTimeout = cfg.Redis.DialTimeout
			redisConfig.ReadTimeout = cfg.Redis.ReadTimeout
			redisConfig.WriteTimeout = cfg.Redis.WriteTimeout
		}

		// 只有在Redis启用时才创建Redis连接池
		poolConfig := &pool.RedisPoolConfig{
			Mode:             pool.RedisMode(redisConfig.Mode),
			Addr:             redisConfig.Addr,
			Password:         redisConfig.Password,
			DB:               redisConfig.DB,
			MasterName:       redisConfig.MasterName,
			SentinelAddrs:    redisConfig.SentinelAddrs,
			SentinelPassword: redisConfig.SentinelPassword,
			ClusterAddrs:     redisConfig.ClusterAddrs,
			PoolSize:         redisConfig.PoolSize,
			MinIdleConns:     redisConfig.MinIdleConns,
			MaxConnAge:       redisConfig.MaxConnAge,
			PoolTimeout:      redisConfig.PoolTimeout,
			IdleTimeout:      redisConfig.IdleTimeout,
			IdleCheckFreq:    redisConfig.IdleCheckFreq,
			DialTimeout:      redisConfig.DialTimeout,
			ReadTimeout:      redisConfig.ReadTimeout,
			WriteTimeout:     redisConfig.WriteTimeout,
			MaxRetries:       redisConfig.MaxRetries,
			MinRetryBackoff:  redisConfig.MinRetryBackoff,
			MaxRetryBackoff:  redisConfig.MaxRetryBackoff,
			MaxRedirects:     redisConfig.MaxRedirects,
			ReadOnly:         redisConfig.ReadOnly,
			RouteByLatency:   redisConfig.RouteByLatency,
			RouteRandomly:    redisConfig.RouteRandomly,
		}

		var err error
		redisPool, err = pool.NewRedisPool(poolConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Redis pool: %w", err)
		}
	}

	registry := &ServiceRegistry{
		config:       cfg,
		nodeID:       nodeID,
		grpcPort:     grpcPort,
		redisPool:    redisPool,
		services:     make(map[string]*ServiceInfo),
		stopChan:     make(chan struct{}),
		redisEnabled: redisEnabled,
		hashRing:     consistenthash.New(DefaultReplicas),
		nodeInfo: &NodeInfo{
			ID:       nodeID,
			Address:  "localhost",
			Port:     grpcPort,
			Status:   "healthy",
			LastSeen: time.Now(),
		},
	}

	// 创建健康检查器
	registry.healthChecker = NewHealthChecker(registry)

	if redisEnabled {
		log.Printf("Service registry initialized with Redis enabled (mode: %s)", redisConfig.Mode)
	} else {
		log.Println("Service registry initialized with Redis disabled (single-node mode)")
	}

	return registry, nil
}

// Start 启动服务注册
func (sr *ServiceRegistry) Start() error {
	sr.mutex.Lock()
	defer sr.mutex.Unlock()

	if sr.isRunning {
		return fmt.Errorf("service registry already running")
	}

	if !sr.redisEnabled {
		log.Println("Redis disabled, skipping service registration - running in single-node mode")
		sr.isRunning = true
		return nil
	}

	// 只有在Redis启用时才进行服务注册
	if err := sr.registerService(); err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}

	// 启动健康检查
	go sr.healthChecker.Start()

	// 启动服务发现
	go sr.startServiceDiscovery()

	sr.isRunning = true
	log.Printf("Service registry started for node: %s", sr.nodeID)
	return nil
}

// Stop 停止服务注册
func (sr *ServiceRegistry) Stop() error {
	sr.mutex.Lock()
	defer sr.mutex.Unlock()

	if !sr.isRunning {
		return nil
	}

	close(sr.stopChan)
	sr.isRunning = false

	if sr.redisEnabled && sr.redisPool != nil {
		// 注销服务
		if err := sr.unregisterService(); err != nil {
			log.Printf("Failed to unregister service: %v", err)
		}

		// 关闭Redis连接池
		if err := sr.redisPool.Close(); err != nil {
			log.Printf("Failed to close Redis pool: %v", err)
		}
	}

	log.Printf("Service registry stopped for node: %s", sr.nodeID)
	return nil
}

// DiscoverNodes 发现节点
func (sr *ServiceRegistry) DiscoverNodes() ([]*ServiceInfo, error) {
	if !sr.redisEnabled {
		// Redis禁用时，返回当前节点信息（单节点模式）
		currentNode := &ServiceInfo{
			NodeID:   sr.nodeID,
			Address:  "localhost:" + sr.grpcPort,
			Port:     sr.grpcPort,
			Status:   "healthy",
			LastSeen: time.Now(),
			Metadata: map[string]string{"mode": "single-node"},
		}
		return []*ServiceInfo{currentNode}, nil
	}

	// Redis启用时，从Redis发现其他节点
	return sr.discoverFromRedis()
}

// registerService 注册服务到Redis
func (sr *ServiceRegistry) registerService() error {
	if !sr.redisEnabled || sr.redisPool == nil {
		return fmt.Errorf("Redis not enabled or pool not available")
	}

	ctx := context.Background()
	client := sr.redisPool.GetRedisClient()

	// 获取本机IP地址
	address, err := getLocalIP()
	if err != nil {
		return fmt.Errorf("failed to get local IP: %w", err)
	}

	serviceInfo := &ServiceInfo{
		NodeID:   sr.nodeID,
		Address:  address,
		Port:     sr.grpcPort,
		Status:   "healthy",
		LastSeen: time.Now(),
		Metadata: map[string]string{
			"mode":          "multi-node",
			"redis_enabled": "true",
		},
	}

	serviceData, err := json.Marshal(serviceInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal service info: %w", err)
	}

	// 注册到服务列表
	if err := client.HSet(ctx, ServiceRegistryKey, sr.nodeID, serviceData).Err(); err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}

	// 设置服务健康状态
	healthKey := fmt.Sprintf(NodeHealthKey, sr.nodeID)
	if err := client.Set(ctx, healthKey, "healthy", DefaultNodeTTL).Err(); err != nil {
		return fmt.Errorf("failed to set service health: %w", err)
	}

	log.Printf("Service registered: %s at %s:%s", sr.nodeID, address, sr.grpcPort)
	return nil
}

// unregisterService 从Redis注销服务
func (sr *ServiceRegistry) unregisterService() error {
	if !sr.redisEnabled || sr.redisPool == nil {
		return nil
	}

	ctx := context.Background()
	client := sr.redisPool.GetRedisClient()

	// 从服务列表移除
	if err := client.HDel(ctx, ServiceRegistryKey, sr.nodeID).Err(); err != nil {
		log.Printf("WARN: failed to remove service from registry: %v", err)
	}

	// 删除健康状态
	healthKey := fmt.Sprintf(NodeHealthKey, sr.nodeID)
	if err := client.Del(ctx, healthKey).Err(); err != nil {
		log.Printf("WARN: failed to remove service health: %v", err)
	}

	log.Printf("Service unregistered: %s", sr.nodeID)
	return nil
}

// discoverFromRedis 从Redis发现服务
func (sr *ServiceRegistry) discoverFromRedis() ([]*ServiceInfo, error) {
	if !sr.redisEnabled || sr.redisPool == nil {
		return nil, fmt.Errorf("Redis not enabled or pool not available")
	}

	ctx := context.Background()
	client := sr.redisPool.GetRedisClient()

	// 获取所有注册的服务
	serviceMap, err := client.HGetAll(ctx, ServiceRegistryKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get registered services: %w", err)
	}

	var healthyServices []*ServiceInfo

	for serviceID, serviceData := range serviceMap {
		var service ServiceInfo
		if err := json.Unmarshal([]byte(serviceData), &service); err != nil {
			log.Printf("WARN: failed to unmarshal service data for %s: %v", serviceID, err)
			continue
		}

		// 检查服务健康状态
		healthKey := fmt.Sprintf(NodeHealthKey, serviceID)
		_, err := client.Get(ctx, healthKey).Result()
		if err == redis.Nil {
			// 服务不健康，从注册表移除
			client.HDel(ctx, ServiceRegistryKey, serviceID)
			log.Printf("Removed unhealthy service %s from registry", serviceID)
			continue
		} else if err != nil {
			log.Printf("WARN: failed to check health for service %s: %v", serviceID, err)
			continue
		}

		healthyServices = append(healthyServices, &service)
	}

	return healthyServices, nil
}

// startServiceDiscovery 启动服务发现
func (sr *ServiceRegistry) startServiceDiscovery() {
	if !sr.redisEnabled {
		return
	}

	ticker := time.NewTicker(DefaultHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 发送心跳
			if err := sr.sendHeartbeat(); err != nil {
				log.Printf("ERROR: heartbeat failed: %v", err)
			}

			// 更新服务列表
			if err := sr.updateServiceList(); err != nil {
				log.Printf("ERROR: failed to update service list: %v", err)
			}
		case <-sr.stopChan:
			return
		}
	}
}

// sendHeartbeat 发送心跳
func (sr *ServiceRegistry) sendHeartbeat() error {
	if !sr.redisEnabled || sr.redisPool == nil {
		return nil
	}

	ctx := context.Background()
	client := sr.redisPool.GetRedisClient()

	// 刷新健康状态TTL
	healthKey := fmt.Sprintf(NodeHealthKey, sr.nodeID)
	if err := client.Expire(ctx, healthKey, DefaultNodeTTL).Err(); err != nil {
		return fmt.Errorf("failed to refresh service health TTL: %w", err)
	}

	return nil
}

// updateServiceList 更新服务列表
func (sr *ServiceRegistry) updateServiceList() error {
	services, err := sr.discoverFromRedis()
	if err != nil {
		return err
	}

	sr.mutex.Lock()
	defer sr.mutex.Unlock()

	// 清空现有服务列表
	sr.services = make(map[string]*ServiceInfo)

	// 添加发现的服务
	for _, service := range services {
		sr.services[service.NodeID] = service
	}

	return nil
}

// GetNodeForKey 根据键获取对应的节点（一致性哈希）
func (sr *ServiceRegistry) GetNodeForKey(key string) string {
	if !sr.redisEnabled {
		// 单节点模式，直接返回当前节点
		return sr.nodeID
	}

	// 多节点模式，使用一致性哈希
	sr.mutex.RLock()
	defer sr.mutex.RUnlock()

	if len(sr.services) == 0 {
		return sr.nodeID
	}

	// 简单的哈希分配（实际应用中可以使用更复杂的一致性哈希）
	hash := hashString(key)
	nodeIndex := hash % len(sr.services)

	i := 0
	for nodeID := range sr.services {
		if i == nodeIndex {
			return nodeID
		}
		i++
	}

	return sr.nodeID
}

// IsNodeHealthy 检查节点是否健康
func (sr *ServiceRegistry) IsNodeHealthy(nodeID string) bool {
	if !sr.redisEnabled {
		return nodeID == sr.nodeID
	}

	sr.mutex.RLock()
	defer sr.mutex.RUnlock()

	service, exists := sr.services[nodeID]
	if !exists {
		return false
	}

	return service.Status == "healthy"
}

// GetHealthyNodes 获取健康的节点列表（向后兼容）
func (sr *ServiceRegistry) GetHealthyNodes() ([]*NodeInfo, error) {
	services, err := sr.DiscoverNodes()
	if err != nil {
		return nil, err
	}

	var nodes []*NodeInfo
	for _, service := range services {
		node := &NodeInfo{
			ID:       service.NodeID,
			Address:  service.Address,
			Port:     service.Port,
			Status:   service.Status,
			LastSeen: service.LastSeen,
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// hashString 简单的字符串哈希函数
func hashString(s string) int {
	h := 0
	for _, c := range s {
		h = 31*h + int(c)
	}
	if h < 0 {
		h = -h
	}
	return h
}

// getLocalIP 获取本机IP地址
func getLocalIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// GetHashRing 获取当前哈希环
func (sr *ServiceRegistry) GetHashRing() *consistenthash.ConsistentHash {
	return sr.hashRing
}

// GetNodeInfo 获取当前节点信息
func (sr *ServiceRegistry) GetNodeInfo() *NodeInfo {
	return sr.nodeInfo
}
