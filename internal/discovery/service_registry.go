package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"minIODB/config"
	"minIODB/internal/metrics"
	"minIODB/pkg/consistenthash"
	"minIODB/pkg/pool"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

const (
	// Redis keys
	ServiceRegistryKey  = "nodes:services"    // 服务注册表
	HashRingKey         = "cluster:hash_ring" // 一致性哈希环
	NodeHealthKey       = "nodes:health:%s"   // 节点健康状态
	NodeMetricsKey      = "nodes:metrics:%s"  // 节点指标
	NodeEventsKey       = "nodes:events:%s"   // 节点事件
	NodeMetricsTTL      = 120 * time.Second   // 指标TTL
	NodeEventsMaxCount  = 100                 // 事件最大保留数
	SuspiciousThreshold = 30 * time.Second    // suspicious 状态阈值（1个周期）
	OfflineThreshold    = 60 * time.Second    // offline 状态阈值（2个周期）

	// Default values
	DefaultHeartbeatInterval = 30 * time.Second // 心跳间隔
	DefaultNodeTTL           = 60 * time.Second // 节点TTL
	DefaultReplicas          = 150              // 一致性哈希虚拟节点数
)

// NodeState 节点状态类型
type NodeState string

const (
	NodeStateUnknown         NodeState = "unknown"         // 未知状态（节点首次加入时）
	NodeStateOnline          NodeState = "online"          // 正常运行
	NodeStateSuspicious      NodeState = "suspicious"      // 心跳超时1个周期
	NodeStateOffline         NodeState = "offline"         // 心跳超时2个周期
	NodeStateDecommissioning NodeState = "decommissioning" // 手动下线中
	NodeStateTombstone       NodeState = "tombstone"       // 已确认移除
)

// NodeMetrics 节点运行时指标
type NodeMetrics struct {
	CPUPercent     float64 `json:"cpu_percent"`
	MemoryUsedMB   float64 `json:"memory_used_mb"`
	Goroutines     int     `json:"goroutines"`
	ActiveQueries  int64   `json:"active_queries"`
	QPS            float64 `json:"qps"`
	AvgQueryTimeMs float64 `json:"avg_query_time_ms"`
	PendingWrites  int64   `json:"pending_writes"`
	MinIOPoolUsage float64 `json:"minio_pool_usage"`
	UpdatedAt      int64   `json:"updated_at"`
}

// NodeEvent 节点事件
type NodeEvent struct {
	Timestamp time.Time `json:"timestamp"`
	NodeID    string    `json:"node_id"`
	Event     string    `json:"event"` // joined/suspicious/offline/recovered/decommissioned/recommissioned/removed
	Message   string    `json:"message"`
	PrevState NodeState `json:"prev_state"`
	NewState  NodeState `json:"new_state"`
}

// pendingEvent 待记录的事件（用于锁外批量处理）
type pendingEvent struct {
	nodeID    string
	event     string
	message   string
	prevState NodeState
	newState  NodeState
}

// NodeInfo 节点信息
type NodeInfo struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	Port     string    `json:"port"`
	State    NodeState `json:"state"`
	LastSeen time.Time `json:"last_seen"`
}

// ServiceInfo 服务信息
type ServiceInfo struct {
	NodeID    string            `json:"node_id"`
	Address   string            `json:"address"`
	Port      string            `json:"port"`
	State     NodeState         `json:"state"`
	Status    string            `json:"status"` // 向后兼容，等同于 State 的字符串形式
	StartTime time.Time         `json:"start_time"`
	LastSeen  time.Time         `json:"last_seen"`
	Labels    map[string]string `json:"labels,omitempty"`
	Metadata  map[string]string `json:"metadata"`
	Metrics   *NodeMetrics      `json:"metrics,omitempty"`
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
	defer hc.registry.wg.Done()
	ticker := time.NewTicker(DefaultHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if hc.registry.redisEnabled {
				if err := hc.registry.sendHeartbeat(); err != nil {
					hc.registry.logger.Info("Health check failed", zap.Error(err))
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
	wg        sync.WaitGroup // 新增：用于等待 goroutines 优雅退出

	// Redis状态
	redisEnabled bool

	// 一致性哈希环
	hashRing *consistenthash.ConsistentHash

	// 节点信息
	nodeInfo *NodeInfo

	// 日志器
	logger *zap.Logger
}

// NewServiceRegistry 创建服务注册器
func NewServiceRegistry(cfg config.Config, nodeID, grpcPort string, logger *zap.Logger) (*ServiceRegistry, error) {
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
		redisPool, err = pool.NewRedisPool(poolConfig, logger)
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
			State:    NodeStateOnline,
			LastSeen: time.Now(),
		},
		logger: logger,
	}

	// 创建健康检查器
	registry.healthChecker = NewHealthChecker(registry)

	if redisEnabled {
		registry.logger.Info("Service registry initialized with Redis enabled", zap.String("mode", string(redisConfig.Mode)))
	} else {
		registry.logger.Info("Service registry initialized with Redis disabled (single-node mode)")
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
		sr.logger.Info("Redis disabled, skipping service registration - running in single-node mode")
		sr.isRunning = true
		return nil
	}

	// 只有在Redis启用时才进行服务注册
	if err := sr.registerService(); err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}

	// 启动健康检查
	sr.wg.Add(1)
	go sr.healthChecker.Start()

	// 启动服务发现
	sr.wg.Add(1)
	go sr.startServiceDiscovery()

	sr.isRunning = true
	sr.logger.Info("Service registry started for node", zap.String("node_id", sr.nodeID))
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

	// 等待所有 goroutines 退出（带超时）
	done := make(chan struct{})
	go func() {
		sr.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		sr.logger.Warn("Timeout waiting for goroutines to stop")
	}

	// 重置 stopChan 以便重启
	sr.stopChan = make(chan struct{})

	if sr.redisEnabled && sr.redisPool != nil {
		// 注销服务
		if err := sr.unregisterService(); err != nil {
			sr.logger.Info("Failed to unregister service", zap.Error(err))
		}

		// 关闭Redis连接池
		if err := sr.redisPool.Close(); err != nil {
			sr.logger.Info("Failed to close Redis pool", zap.Error(err))
		}
	}

	sr.logger.Info("Service registry stopped for node", zap.String("node_id", sr.nodeID))
	return nil
}

// DiscoverNodes 发现节点
func (sr *ServiceRegistry) DiscoverNodes() ([]*ServiceInfo, error) {
	if !sr.redisEnabled {
		// Redis禁用时，返回当前节点信息（单节点模式）
		currentNode := &ServiceInfo{
			NodeID:    sr.nodeID,
			Address:   "localhost:" + sr.grpcPort,
			Port:      sr.grpcPort,
			State:     NodeStateOnline,
			Status:    string(NodeStateOnline),
			StartTime: time.Now(),
			LastSeen:  time.Now(),
			Labels:    map[string]string{"mode": "single-node"},
			Metadata:  map[string]string{"mode": "single-node"},
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
	client := sr.redisPool.GetClient()
	if client == nil {
		return fmt.Errorf("Redis client not available")
	}

	// 获取本机IP地址
	address, err := getLocalIP()
	if err != nil {
		return fmt.Errorf("failed to get local IP: %w", err)
	}

	serviceInfo := &ServiceInfo{
		NodeID:    sr.nodeID,
		Address:   address,
		Port:      sr.grpcPort,
		State:     NodeStateOnline,
		Status:    string(NodeStateOnline),
		StartTime: time.Now(),
		LastSeen:  time.Now(),
		Labels:    map[string]string{},
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

	sr.logger.Info("Service registered", zap.String("node_id", sr.nodeID), zap.String("address", address), zap.String("port", sr.grpcPort))
	return nil
}

// unregisterService 从Redis注销服务
func (sr *ServiceRegistry) unregisterService() error {
	if !sr.redisEnabled || sr.redisPool == nil {
		return nil
	}

	ctx := context.Background()
	client := sr.redisPool.GetClient()
	if client == nil {
		return nil
	}

	// 从服务列表移除
	if err := client.HDel(ctx, ServiceRegistryKey, sr.nodeID).Err(); err != nil {
		sr.logger.Warn("Failed to remove service from registry", zap.Error(err))
	}

	// 删除健康状态
	healthKey := fmt.Sprintf(NodeHealthKey, sr.nodeID)
	if err := client.Del(ctx, healthKey).Err(); err != nil {
		sr.logger.Warn("Failed to remove service health", zap.Error(err))
	}

	sr.logger.Info("Service unregistered", zap.String("node_id", sr.nodeID))
	return nil
}

// discoverFromRedis 从Redis发现服务
func (sr *ServiceRegistry) discoverFromRedis() ([]*ServiceInfo, error) {
	if !sr.redisEnabled || sr.redisPool == nil {
		return nil, fmt.Errorf("Redis not enabled or pool not available")
	}

	ctx := context.Background()
	client := sr.redisPool.GetClient()
	if client == nil {
		return nil, fmt.Errorf("Redis client not available")
	}

	// 获取所有注册的服务
	serviceMap, err := client.HGetAll(ctx, ServiceRegistryKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get registered services: %w", err)
	}

	var healthyServices []*ServiceInfo

	for serviceID, serviceData := range serviceMap {
		var service ServiceInfo
		if err := json.Unmarshal([]byte(serviceData), &service); err != nil {
			sr.logger.Warn("Failed to unmarshal service data", zap.String("service_id", serviceID), zap.Error(err))
			continue
		}

		// 检查服务健康状态
		healthKey := fmt.Sprintf(NodeHealthKey, serviceID)
		_, err := client.Get(ctx, healthKey).Result()
		if err == redis.Nil {
			// 服务不健康，从注册表移除
			client.HDel(ctx, ServiceRegistryKey, serviceID)
			sr.logger.Info("Removed unhealthy service from registry", zap.String("service_id", serviceID))
			continue
		} else if err != nil {
			sr.logger.Warn("Failed to check health for service", zap.String("service_id", serviceID), zap.Error(err))
			continue
		}

		healthyServices = append(healthyServices, &service)
	}

	return healthyServices, nil
}

// startServiceDiscovery 启动服务发现
func (sr *ServiceRegistry) startServiceDiscovery() {
	defer sr.wg.Done()
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
				sr.logger.Error("Heartbeat failed", zap.Error(err))
			}

			// 更新服务列表
			if err := sr.updateServiceList(); err != nil {
				sr.logger.Error("Failed to update service list", zap.Error(err))
			}
		case <-sr.stopChan:
			return
		}
	}
}

// sendHeartbeat 发送心跳并上报指标
func (sr *ServiceRegistry) sendHeartbeat() error {
	if !sr.redisEnabled || sr.redisPool == nil {
		return nil
	}

	ctx := context.Background()
	client := sr.redisPool.GetClient()
	if client == nil {
		return nil
	}

	// 刷新健康状态TTL
	healthKey := fmt.Sprintf(NodeHealthKey, sr.nodeID)
	if err := client.Expire(ctx, healthKey, DefaultNodeTTL).Err(); err != nil {
		return fmt.Errorf("failed to refresh service health TTL: %w", err)
	}

	// 获取运行时指标并上报到 Redis
	snapshot := metrics.GetRuntimeSnapshot()
	if snapshot != nil && !snapshot.Stale {
		nodeMetrics := &NodeMetrics{
			CPUPercent:     snapshot.CPUPercent,
			MemoryUsedMB:   snapshot.HeapAllocMB,
			Goroutines:     snapshot.Goroutines,
			PendingWrites:  snapshot.PendingWrites,
			MinIOPoolUsage: snapshot.MinIOPoolUsage,
			UpdatedAt:      time.Now().Unix(),
		}

		metricsData, err := json.Marshal(nodeMetrics)
		if err != nil {
			sr.logger.Warn("Failed to marshal node metrics", zap.Error(err))
		} else {
			metricsKey := fmt.Sprintf(NodeMetricsKey, sr.nodeID)
			if err := client.Set(ctx, metricsKey, metricsData, NodeMetricsTTL).Err(); err != nil {
				sr.logger.Warn("Failed to write node metrics to Redis", zap.Error(err))
			}
		}
	}

	return nil
}

// updateServiceList 更新服务列表并实施自动状态转换
func (sr *ServiceRegistry) updateServiceList() error {
	services, err := sr.discoverFromRedis()
	if err != nil {
		return err
	}

	var events []pendingEvent

	sr.mutex.Lock()

	prevServices := make(map[string]*ServiceInfo)
	for k, v := range sr.services {
		prevServices[k] = v
	}

	sr.services = make(map[string]*ServiceInfo)

	now := time.Now()

	for _, service := range services {
		nodeID := service.NodeID

		if service.StartTime.IsZero() {
			service.StartTime = now
		}
		if service.Labels == nil {
			service.Labels = make(map[string]string)
		}

		prevService, existed := prevServices[nodeID]
		if !existed {
			service.State = NodeStateOnline
			events = append(events, pendingEvent{
				nodeID: nodeID, event: "joined", message: "Node joined the cluster",
				prevState: NodeStateUnknown, newState: NodeStateOnline,
			})
		} else {
			prevState := prevService.State
			lastSeen := service.LastSeen
			timeSinceLastSeen := now.Sub(lastSeen)

			if prevState == NodeStateDecommissioning || prevState == NodeStateTombstone {
				service.State = prevState
			} else if timeSinceLastSeen >= OfflineThreshold {
				if prevState != NodeStateOffline {
					service.State = NodeStateOffline
					events = append(events, pendingEvent{
						nodeID: nodeID, event: "offline", message: "Node offline due to heartbeat timeout",
						prevState: prevState, newState: NodeStateOffline,
					})
				} else {
					service.State = NodeStateOffline
				}
			} else if timeSinceLastSeen >= SuspiciousThreshold {
				if prevState != NodeStateSuspicious {
					service.State = NodeStateSuspicious
					events = append(events, pendingEvent{
						nodeID: nodeID, event: "suspicious", message: "Node suspicious due to heartbeat delay",
						prevState: prevState, newState: NodeStateSuspicious,
					})
				} else {
					service.State = NodeStateSuspicious
				}
			} else {
				if prevState == NodeStateSuspicious || prevState == NodeStateOffline {
					service.State = NodeStateOnline
					events = append(events, pendingEvent{
						nodeID: nodeID, event: "recovered", message: "Node recovered",
						prevState: prevState, newState: NodeStateOnline,
					})
				} else {
					service.State = NodeStateOnline
				}
			}
		}

		sr.services[nodeID] = service
	}

	for nodeID, prevService := range prevServices {
		if _, stillExists := sr.services[nodeID]; !stillExists {
			if prevService.State != NodeStateTombstone {
				events = append(events, pendingEvent{
					nodeID: nodeID, event: "offline", message: "Node removed from registry",
					prevState: prevService.State, newState: NodeStateOffline,
				})
			}
		}
	}

	sr.mutex.Unlock()

	for _, e := range events {
		sr.recordEvent(e.nodeID, e.event, e.message, e.prevState, e.newState)
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

	// 使用排序的节点列表确保确定性
	nodeIDs := make([]string, 0, len(sr.services))
	for nodeID := range sr.services {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)

	hash := hashString(key)
	nodeIndex := hash % len(nodeIDs)
	return nodeIDs[nodeIndex]
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

	return service.State == NodeStateOnline || service.State == NodeStateSuspicious
}

// GetHealthyNodes 获取健康的节点列表（只返回 online 和 suspicious 状态的节点）
func (sr *ServiceRegistry) GetHealthyNodes() ([]*NodeInfo, error) {
	services, err := sr.DiscoverNodes()
	if err != nil {
		return nil, err
	}

	var nodes []*NodeInfo
	for _, service := range services {
		if service.State != NodeStateOnline && service.State != NodeStateSuspicious {
			continue
		}
		node := &NodeInfo{
			ID:       service.NodeID,
			Address:  service.Address,
			Port:     service.Port,
			State:    service.State,
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

// recordEvent 记录节点事件到 Redis List（nodes:events:{nodeID}）
// 如果 Redis 不可用，则仅在内存中记录
func (sr *ServiceRegistry) recordEvent(nodeID, eventType, message string, prevState, newState NodeState) {
	event := &NodeEvent{
		Timestamp: time.Now(),
		NodeID:    nodeID,
		Event:     eventType,
		Message:   message,
		PrevState: prevState,
		NewState:  newState,
	}

	sr.logger.Info("Node event recorded",
		zap.String("node_id", nodeID),
		zap.String("event", eventType),
		zap.String("prev_state", string(prevState)),
		zap.String("new_state", string(newState)),
		zap.String("message", message),
	)

	// 如果 Redis 不可用，仅在内存中记录
	if !sr.redisEnabled || sr.redisPool == nil {
		return
	}

	client := sr.redisPool.GetClient()
	if client == nil {
		return
	}

	ctx := context.Background()
	eventsKey := fmt.Sprintf(NodeEventsKey, nodeID)

	eventData, err := json.Marshal(event)
	if err != nil {
		sr.logger.Warn("Failed to marshal node event", zap.Error(err))
		return
	}

	// LPUSH 添加事件到列表头部
	if err := client.LPush(ctx, eventsKey, eventData).Err(); err != nil {
		sr.logger.Warn("Failed to write node event to Redis", zap.Error(err))
		return
	}

	// LTRIM 保留最近 100 条事件
	if err := client.LTrim(ctx, eventsKey, 0, int64(NodeEventsMaxCount-1)).Err(); err != nil {
		sr.logger.Warn("Failed to trim node events list", zap.Error(err))
	}
}

// DecommissionNode 将节点标记为 decommissioning
func (sr *ServiceRegistry) DecommissionNode(ctx context.Context, nodeID string) error {
	var prevState NodeState
	var updatedService *ServiceInfo

	sr.mutex.Lock()
	service, exists := sr.services[nodeID]
	if !exists {
		sr.mutex.Unlock()
		return fmt.Errorf("node %s not found", nodeID)
	}

	if service.State != NodeStateOnline && service.State != NodeStateSuspicious {
		sr.mutex.Unlock()
		return fmt.Errorf("node %s is in state %s, cannot decommission (must be online or suspicious)", nodeID, service.State)
	}

	prevState = service.State
	service.State = NodeStateDecommissioning
	service.Status = string(NodeStateDecommissioning)
	updatedService = service
	sr.mutex.Unlock()

	if sr.redisEnabled && sr.redisPool != nil {
		client := sr.redisPool.GetClient()
		if client != nil {
			serviceData, err := json.Marshal(updatedService)
			if err != nil {
				sr.logger.Warn("Failed to marshal service info for decommission", zap.Error(err))
			} else {
				if err := client.HSet(ctx, ServiceRegistryKey, nodeID, serviceData).Err(); err != nil {
					sr.logger.Warn("Failed to update node state in Redis", zap.Error(err))
				}
			}
		}
	}

	sr.recordEvent(nodeID, "decommissioned", "Node decommissioned by operator", prevState, NodeStateDecommissioning)
	sr.logger.Info("Node decommissioned", zap.String("node_id", nodeID))

	return nil
}

// RecommissionNode 将节点重新上线
func (sr *ServiceRegistry) RecommissionNode(ctx context.Context, nodeID string) error {
	var prevState NodeState
	var updatedService *ServiceInfo

	sr.mutex.Lock()
	service, exists := sr.services[nodeID]
	if !exists {
		sr.mutex.Unlock()
		return fmt.Errorf("node %s not found", nodeID)
	}

	if service.State != NodeStateDecommissioning && service.State != NodeStateOffline {
		sr.mutex.Unlock()
		return fmt.Errorf("node %s is in state %s, cannot recommission (must be decommissioning or offline)", nodeID, service.State)
	}

	prevState = service.State
	service.State = NodeStateOnline
	service.Status = string(NodeStateOnline)
	service.LastSeen = time.Now()
	updatedService = service
	sr.mutex.Unlock()

	if sr.redisEnabled && sr.redisPool != nil {
		client := sr.redisPool.GetClient()
		if client != nil {
			serviceData, err := json.Marshal(updatedService)
			if err != nil {
				sr.logger.Warn("Failed to marshal service info for recommission", zap.Error(err))
			} else {
				if err := client.HSet(ctx, ServiceRegistryKey, nodeID, serviceData).Err(); err != nil {
					sr.logger.Warn("Failed to update node state in Redis", zap.Error(err))
				}
			}
		}
	}

	sr.recordEvent(nodeID, "recommissioned", "Node recommissioned by operator", prevState, NodeStateOnline)
	sr.logger.Info("Node recommissioned", zap.String("node_id", nodeID))

	return nil
}

// RemoveNode 移除节点（标记 tombstone 并从注册表清理）
func (sr *ServiceRegistry) RemoveNode(ctx context.Context, nodeID string) error {
	var prevState NodeState

	sr.mutex.Lock()
	service, exists := sr.services[nodeID]
	if !exists {
		sr.mutex.Unlock()
		return fmt.Errorf("node %s not found", nodeID)
	}

	if service.State != NodeStateOffline && service.State != NodeStateDecommissioning {
		sr.mutex.Unlock()
		return fmt.Errorf("node %s is in state %s, cannot remove (must be offline or decommissioning)", nodeID, service.State)
	}

	prevState = service.State
	delete(sr.services, nodeID)
	sr.mutex.Unlock()

	sr.recordEvent(nodeID, "removed", "Node removed from cluster", prevState, NodeStateTombstone)

	if sr.redisEnabled && sr.redisPool != nil {
		client := sr.redisPool.GetClient()
		if client != nil {
			if err := client.HDel(ctx, ServiceRegistryKey, nodeID).Err(); err != nil {
				sr.logger.Warn("Failed to remove node from Redis registry", zap.Error(err))
			}

			healthKey := fmt.Sprintf(NodeHealthKey, nodeID)
			if err := client.Del(ctx, healthKey).Err(); err != nil {
				sr.logger.Warn("Failed to remove node health key", zap.Error(err))
			}

			metricsKey := fmt.Sprintf(NodeMetricsKey, nodeID)
			if err := client.Del(ctx, metricsKey).Err(); err != nil {
				sr.logger.Warn("Failed to remove node metrics key", zap.Error(err))
			}
		}
	}

	sr.logger.Info("Node removed", zap.String("node_id", nodeID))

	return nil
}

// GetNodeMetrics 获取节点的运行时指标
func (sr *ServiceRegistry) GetNodeMetrics(ctx context.Context, nodeID string) (*NodeMetrics, error) {
	if !sr.redisEnabled || sr.redisPool == nil {
		return nil, nil
	}

	client := sr.redisPool.GetClient()
	if client == nil {
		return nil, nil
	}

	metricsKey := fmt.Sprintf(NodeMetricsKey, nodeID)

	data, err := client.Get(ctx, metricsKey).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		sr.logger.Warn("Failed to get node metrics from Redis", zap.Error(err))
		return nil, nil
	}

	var metrics NodeMetrics
	if err := json.Unmarshal([]byte(data), &metrics); err != nil {
		sr.logger.Warn("Failed to unmarshal node metrics", zap.Error(err))
		return nil, nil
	}

	return &metrics, nil
}

// GetNodeEvents 获取节点状态变更历史
func (sr *ServiceRegistry) GetNodeEvents(ctx context.Context, nodeID string, limit int) ([]NodeEvent, error) {
	if !sr.redisEnabled || sr.redisPool == nil {
		return nil, nil
	}

	client := sr.redisPool.GetClient()
	if client == nil {
		return nil, nil
	}

	eventsKey := fmt.Sprintf(NodeEventsKey, nodeID)

	data, err := client.LRange(ctx, eventsKey, 0, int64(limit-1)).Result()
	if err != nil {
		sr.logger.Warn("Failed to get node events from Redis", zap.Error(err))
		return nil, nil
	}

	var events []NodeEvent
	for _, item := range data {
		var event NodeEvent
		if err := json.Unmarshal([]byte(item), &event); err != nil {
			sr.logger.Warn("Failed to unmarshal node event", zap.Error(err))
			continue
		}
		events = append(events, event)
	}

	return events, nil
}

// SwitchToDistributed 热切换到分布式模式（从单节点模式切换到多节点模式）
// newPool 是已验证通过的新 Redis 连接池
func (sr *ServiceRegistry) SwitchToDistributed(newPool *pool.RedisPool) error {
	sr.mutex.Lock()

	// 如果正在运行，需要先停止现有 goroutines
	if sr.isRunning {
		sr.mutex.Unlock()
		close(sr.stopChan)
		sr.wg.Wait() // 等待所有 goroutines 退出
		sr.mutex.Lock()
	}

	// 更新 Redis 连接池和状态
	sr.redisPool = newPool
	sr.redisEnabled = true
	sr.isRunning = false
	sr.stopChan = make(chan struct{})
	sr.mutex.Unlock()

	sr.logger.Info("Switching to distributed mode, starting service registry...")

	// 重新启动服务注册
	return sr.Start()
}
