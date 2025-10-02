package consistenthash

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	ch := New(3)
	assert.Equal(t, 3, ch.replicas)
	assert.Equal(t, 0, len(ch.keys))
	assert.Equal(t, 0, len(ch.hashMap))
	assert.True(t, ch.IsEmpty())
}

func TestAdd(t *testing.T) {
	ch := New(3)
	ch.Add("node1", "node2", "node3")
	
	assert.Equal(t, 9, len(ch.keys)) // 3 nodes * 3 replicas
	assert.Equal(t, 9, len(ch.hashMap))
	assert.False(t, ch.IsEmpty())
	
	nodes := ch.GetNodes()
	assert.Equal(t, 3, len(nodes))
	assert.Contains(t, nodes, "node1")
	assert.Contains(t, nodes, "node2")
	assert.Contains(t, nodes, "node3")
}

func TestGet(t *testing.T) {
	ch := New(3)
	
	// 空哈希环应该返回空字符串
	assert.Equal(t, "", ch.Get("key1"))
	
	// 添加节点后测试分片
	ch.Add("node1", "node2", "node3")
	
	// 测试数据分片的一致性
	key1Node := ch.Get("key1")
	assert.NotEmpty(t, key1Node)
	assert.Contains(t, []string{"node1", "node2", "node3"}, key1Node)
	
	// 同一个key应该总是分配到同一个节点
	for i := 0; i < 10; i++ {
		assert.Equal(t, key1Node, ch.Get("key1"))
	}
}

func TestRemove(t *testing.T) {
	ch := New(3)
	ch.Add("node1", "node2", "node3")
	
	// 移除一个节点
	ch.Remove("node2")
	
	nodes := ch.GetNodes()
	assert.Equal(t, 2, len(nodes))
	assert.Contains(t, nodes, "node1")
	assert.Contains(t, nodes, "node3")
	assert.NotContains(t, nodes, "node2")
	
	// 确保数据不会分配到已移除的节点
	for i := 0; i < 100; i++ {
		node := ch.Get("key" + strconv.Itoa(i))
		assert.NotEqual(t, "node2", node)
	}
}

func TestConsistency(t *testing.T) {
	ch1 := New(3)
	ch2 := New(3)
	
	nodes := []string{"node1", "node2", "node3"}
	ch1.Add(nodes...)
	ch2.Add(nodes...)
	
	// 两个相同的哈希环应该对相同的key返回相同的节点
	for i := 0; i < 100; i++ {
		key := "key" + strconv.Itoa(i)
		assert.Equal(t, ch1.Get(key), ch2.Get(key))
	}
}

func TestDistribution(t *testing.T) {
	ch := New(150) // 使用更多虚拟节点来获得更好的分布
	ch.Add("node1", "node2", "node3")
	
	distribution := make(map[string]int)
	
	// 测试1000个key的分布
	for i := 0; i < 1000; i++ {
		key := "key" + strconv.Itoa(i)
		node := ch.Get(key)
		distribution[node]++
	}
	
	// 每个节点应该分配到一定数量的key（允许一定的偏差）
	for node, count := range distribution {
		t.Logf("Node %s: %d keys", node, count)
		assert.Greater(t, count, 200) // 至少20%
		assert.Less(t, count, 500)    // 最多50%
	}
}

func TestStats(t *testing.T) {
	ch := New(3)
	ch.Add("node1", "node2")
	
	stats := ch.Stats()
	assert.Equal(t, 6, stats["total_keys"])
	assert.Equal(t, 2, stats["total_nodes"])
	assert.Equal(t, 3, stats["replicas"])
	
	distribution := stats["node_distribution"].(map[string]int)
	assert.Equal(t, 3, distribution["node1"])
	assert.Equal(t, 3, distribution["node2"])
}

func TestScalability(t *testing.T) {
	ch := New(100)
	ch.Add("node1", "node2", "node3")
	
	// 记录添加新节点前的分布
	beforeDistribution := make(map[string]string)
	for i := 0; i < 1000; i++ {
		key := "key" + strconv.Itoa(i)
		beforeDistribution[key] = ch.Get(key)
	}
	
	// 添加新节点
	ch.Add("node4")
	
	// 记录添加新节点后的分布
	changedCount := 0
	for i := 0; i < 1000; i++ {
		key := "key" + strconv.Itoa(i)
		afterNode := ch.Get(key)
		if beforeDistribution[key] != afterNode {
			changedCount++
		}
	}
	
	// 添加节点后，应该只有一部分key的分配发生变化
	// 理想情况下应该是约25%（1/4）的key会重新分配
	t.Logf("Changed keys: %d out of 1000 (%.1f%%)", changedCount, float64(changedCount)/10)
	assert.Greater(t, changedCount, 100)  // 至少10%
	assert.Less(t, changedCount, 400)     // 最多40%
} 