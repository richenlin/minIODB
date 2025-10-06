package utils

import (
	"crypto/sha256"
	"sort"
	"strconv"
)

// ConsistentHash 一致性哈希环
type ConsistentHash struct {
	replicas int            // 虚拟节点数量
	keys     []int          // 已排序的哈希环
	hashMap  map[int]string // 哈希值到节点的映射
}

// New 创建一致性哈希实例
func NewConsistentHash(replicas int) *ConsistentHash {
	return &ConsistentHash{
		replicas: replicas,
		hashMap:  make(map[int]string),
	}
}

// hash 计算字符串的哈希值
func (c *ConsistentHash) hash(key string) int {
	h := sha256.Sum256([]byte(key))
	// 取前4个字节转换为int
	return int(h[0])<<24 + int(h[1])<<16 + int(h[2])<<8 + int(h[3])
}

// Add 添加节点到哈希环
func (c *ConsistentHash) Add(nodes ...string) {
	for _, node := range nodes {
		for i := 0; i < c.replicas; i++ {
			key := c.hash(strconv.Itoa(i) + node)
			c.keys = append(c.keys, key)
			c.hashMap[key] = node
		}
	}
	sort.Ints(c.keys)
}

// Remove 从哈希环中移除节点
func (c *ConsistentHash) Remove(node string) {
	for i := 0; i < c.replicas; i++ {
		key := c.hash(strconv.Itoa(i) + node)
		delete(c.hashMap, key)
		// 从keys中移除
		for j, k := range c.keys {
			if k == key {
				c.keys = append(c.keys[:j], c.keys[j+1:]...)
				break
			}
		}
	}
}

// Get 根据数据key获取对应的节点
func (c *ConsistentHash) Get(key string) string {
	if len(c.keys) == 0 {
		return ""
	}

	hash := c.hash(key)

	// 在哈希环上顺时针查找第一个节点
	idx := sort.Search(len(c.keys), func(i int) bool {
		return c.keys[i] >= hash
	})

	// 如果没找到，说明应该分配给第一个节点（环形）
	if idx == len(c.keys) {
		idx = 0
	}

	return c.hashMap[c.keys[idx]]
}

// GetNodes 获取所有节点列表
func (c *ConsistentHash) GetNodes() []string {
	nodeSet := make(map[string]bool)
	for _, node := range c.hashMap {
		nodeSet[node] = true
	}

	nodes := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		nodes = append(nodes, node)
	}
	return nodes
}

// IsEmpty 检查哈希环是否为空
func (c *ConsistentHash) IsEmpty() bool {
	return len(c.keys) == 0
}

// Stats 获取哈希环统计信息
func (c *ConsistentHash) Stats() map[string]interface{} {
	nodeCount := make(map[string]int)
	for _, node := range c.hashMap {
		nodeCount[node]++
	}

	return map[string]interface{}{
		"total_keys":        len(c.keys),
		"total_nodes":       len(nodeCount),
		"replicas":          c.replicas,
		"node_distribution": nodeCount,
	}
}
