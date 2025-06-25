package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	"minIODB/internal/config"
	"minIODB/pkg/consistenthash"

	"github.com/go-redis/redis/v8"
)

const (
	// Redis keys
	ServiceRegistryKey = "nodes:services"        // 服务注册表
	HashRingKey        = "cluster:hash_ring"     // 一致性哈希环
	NodeHealthKey      = "nodes:health:%s"       // 节点健康状态
	
	// Default values
	DefaultHeartbeatInterval = 30 * time.Second  // 心跳间隔
	DefaultNodeTTL          = 60 * time.Second   // 节点TTL
	DefaultReplicas         = 150                // 一致性哈希虚拟节点数
)

// NodeInfo 节点信息
type NodeInfo struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	Port     string    `json:"port"`
	Status   string    `json:"status"`
	LastSeen time.Time `json:"last_seen"`
}

// ServiceRegistry 服务注册与发现
type ServiceRegistry struct {
	redisClient       *redis.Client
	nodeInfo          *NodeInfo
	heartbeatInterval time.Duration
	nodeTTL           time.Duration
	stopChan          chan struct{}
	hashRing          *consistenthash.ConsistentHash
}

// NewServiceRegistry 创建服务注册实例
func NewServiceRegistry(cfg config.Config, nodeID string, grpcPort string) (*ServiceRegistry, error) {
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	
	// 获取本机IP地址
	address, err := getLocalIP()
	if err != nil {
		return nil, fmt.Errorf("failed to get local IP: %w", err)
	}
	
	nodeInfo := &NodeInfo{
		ID:      nodeID,
		Address: address,
		Port:    grpcPort,
		Status:  "healthy",
	}
	
	hashRing := consistenthash.New(DefaultReplicas)
	
	return &ServiceRegistry{
		redisClient:       redisClient,
		nodeInfo:          nodeInfo,
		heartbeatInterval: DefaultHeartbeatInterval,
		nodeTTL:           DefaultNodeTTL,
		stopChan:          make(chan struct{}),
		hashRing:          hashRing,
	}, nil
}

// Start 启动服务注册
func (sr *ServiceRegistry) Start() error {
	ctx := context.Background()
	
	// 注册节点
	if err := sr.register(ctx); err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}
	
	// 更新哈希环
	if err := sr.updateHashRing(ctx); err != nil {
		log.Printf("WARN: failed to update hash ring: %v", err)
	}
	
	// 启动心跳
	go sr.startHeartbeat()
	
	log.Printf("Service registry started for node %s at %s:%s", 
		sr.nodeInfo.ID, sr.nodeInfo.Address, sr.nodeInfo.Port)
	
	return nil
}

// Stop 停止服务注册
func (sr *ServiceRegistry) Stop() {
	close(sr.stopChan)
	
	ctx := context.Background()
	// 注销节点
	if err := sr.unregister(ctx); err != nil {
		log.Printf("ERROR: failed to unregister node: %v", err)
	}
	
	log.Printf("Service registry stopped for node %s", sr.nodeInfo.ID)
}

// register 注册节点到Redis
func (sr *ServiceRegistry) register(ctx context.Context) error {
	sr.nodeInfo.LastSeen = time.Now()
	
	nodeData, err := json.Marshal(sr.nodeInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal node info: %w", err)
	}
	
	// 注册到服务列表
	if err := sr.redisClient.HSet(ctx, ServiceRegistryKey, sr.nodeInfo.ID, nodeData).Err(); err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}
	
	// 设置节点健康状态
	healthKey := fmt.Sprintf(NodeHealthKey, sr.nodeInfo.ID)
	if err := sr.redisClient.Set(ctx, healthKey, "healthy", sr.nodeTTL).Err(); err != nil {
		return fmt.Errorf("failed to set node health: %w", err)
	}
	
	return nil
}

// unregister 从Redis注销节点
func (sr *ServiceRegistry) unregister(ctx context.Context) error {
	// 从服务列表移除
	if err := sr.redisClient.HDel(ctx, ServiceRegistryKey, sr.nodeInfo.ID).Err(); err != nil {
		log.Printf("WARN: failed to remove node from registry: %v", err)
	}
	
	// 删除健康状态
	healthKey := fmt.Sprintf(NodeHealthKey, sr.nodeInfo.ID)
	if err := sr.redisClient.Del(ctx, healthKey).Err(); err != nil {
		log.Printf("WARN: failed to remove node health: %v", err)
	}
	
	// 更新哈希环
	if err := sr.updateHashRing(ctx); err != nil {
		log.Printf("WARN: failed to update hash ring after unregister: %v", err)
	}
	
	return nil
}

// startHeartbeat 启动心跳机制
func (sr *ServiceRegistry) startHeartbeat() {
	ticker := time.NewTicker(sr.heartbeatInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			ctx := context.Background()
			if err := sr.sendHeartbeat(ctx); err != nil {
				log.Printf("ERROR: heartbeat failed: %v", err)
			}
		case <-sr.stopChan:
			return
		}
	}
}

// sendHeartbeat 发送心跳
func (sr *ServiceRegistry) sendHeartbeat(ctx context.Context) error {
	sr.nodeInfo.LastSeen = time.Now()
	
	// 更新节点信息
	nodeData, err := json.Marshal(sr.nodeInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal node info: %w", err)
	}
	
	if err := sr.redisClient.HSet(ctx, ServiceRegistryKey, sr.nodeInfo.ID, nodeData).Err(); err != nil {
		return fmt.Errorf("failed to update node info: %w", err)
	}
	
	// 刷新健康状态TTL
	healthKey := fmt.Sprintf(NodeHealthKey, sr.nodeInfo.ID)
	if err := sr.redisClient.Expire(ctx, healthKey, sr.nodeTTL).Err(); err != nil {
		return fmt.Errorf("failed to refresh node health TTL: %w", err)
	}
	
	return nil
}

// DiscoverNodes 发现所有健康的节点
func (sr *ServiceRegistry) DiscoverNodes() ([]*NodeInfo, error) {
	ctx := context.Background()
	
	// 获取所有注册的节点
	nodeMap, err := sr.redisClient.HGetAll(ctx, ServiceRegistryKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get registered nodes: %w", err)
	}
	
	var healthyNodes []*NodeInfo
	
	for nodeID, nodeData := range nodeMap {
		var node NodeInfo
		if err := json.Unmarshal([]byte(nodeData), &node); err != nil {
			log.Printf("WARN: failed to unmarshal node data for %s: %v", nodeID, err)
			continue
		}
		
		// 检查节点健康状态
		healthKey := fmt.Sprintf(NodeHealthKey, nodeID)
		
		_, err := sr.redisClient.Get(ctx, healthKey).Result()
		if err == redis.Nil {
			// 节点不健康，从注册表移除
			sr.redisClient.HDel(ctx, ServiceRegistryKey, nodeID)
			log.Printf("Removed unhealthy node %s from registry", nodeID)
			continue
		} else if err != nil {
			log.Printf("WARN: failed to check health for node %s: %v", nodeID, err)
			continue
		}
		
		healthyNodes = append(healthyNodes, &node)
	}
	
	return healthyNodes, nil
}

// getMapKeys 获取map的所有键（辅助调试函数）
func getMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// updateHashRing 更新一致性哈希环
func (sr *ServiceRegistry) updateHashRing(ctx context.Context) error {
	nodes, err := sr.DiscoverNodes()
	if err != nil {
		return fmt.Errorf("failed to discover nodes: %w", err)
	}
	
	// 重建哈希环
	newHashRing := consistenthash.New(DefaultReplicas)
	nodeAddresses := make([]string, 0, len(nodes))
	
	for _, node := range nodes {
		nodeAddr := fmt.Sprintf("%s:%s", node.Address, node.Port)
		nodeAddresses = append(nodeAddresses, nodeAddr)
	}
	
	if len(nodeAddresses) > 0 {
		newHashRing.Add(nodeAddresses...)
	}
	
	// 更新本地哈希环
	sr.hashRing = newHashRing
	
	// 将哈希环状态保存到Redis
	hashRingData, err := json.Marshal(map[string]interface{}{
		"nodes":      nodeAddresses,
		"replicas":   DefaultReplicas,
		"updated_at": time.Now(),
	})
	if err != nil {
		return fmt.Errorf("failed to marshal hash ring data: %w", err)
	}
	
	if err := sr.redisClient.Set(ctx, HashRingKey, hashRingData, 0).Err(); err != nil {
		return fmt.Errorf("failed to save hash ring to redis: %w", err)
	}
	
	log.Printf("Hash ring updated with %d nodes", len(nodeAddresses))
	return nil
}

// GetNodeForKey 根据key获取对应的节点
func (sr *ServiceRegistry) GetNodeForKey(key string) string {
	return sr.hashRing.Get(key)
}

// GetHashRing 获取当前哈希环
func (sr *ServiceRegistry) GetHashRing() *consistenthash.ConsistentHash {
	return sr.hashRing
}

// GetNodeInfo 获取当前节点信息
func (sr *ServiceRegistry) GetNodeInfo() *NodeInfo {
	return sr.nodeInfo
}

// IsNodeHealthy 检查指定节点是否健康
func (sr *ServiceRegistry) IsNodeHealthy(nodeID string) bool {
	ctx := context.Background()
	healthKey := fmt.Sprintf(NodeHealthKey, nodeID)
	_, err := sr.redisClient.Get(ctx, healthKey).Result()
	return err != redis.Nil
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