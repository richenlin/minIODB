package idgen

import (
	"fmt"
	"sync"
	"time"
)

const (
	// Snowflake算法参数
	epoch           = int64(1640995200000) // 自定义纪元 (2022-01-01 00:00:00 UTC)
	timestampBits   = uint(41)             // 时间戳位数
	nodeIDBits      = uint(10)             // 节点ID位数
	sequenceBits    = uint(12)             // 序列号位数
	maxNodeID       = int64(-1) ^ (int64(-1) << nodeIDBits)
	maxSequence     = int64(-1) ^ (int64(-1) << sequenceBits)
	nodeIDShift     = sequenceBits
	timestampShift  = sequenceBits + nodeIDBits
)

// SnowflakeGenerator Snowflake ID生成器
// 64位结构:
// - 1位符号位（未使用，始终为0）
// - 41位时间戳（毫秒级，可用69年）
// - 10位节点ID（支持1024个节点）
// - 12位序列号（每毫秒可生成4096个ID）
type SnowflakeGenerator struct {
	mu           sync.Mutex
	nodeID       int64
	sequence     int64
	lastTimestamp int64
}

// NewSnowflakeGenerator 创建Snowflake生成器
func NewSnowflakeGenerator(nodeID int64) (*SnowflakeGenerator, error) {
	if nodeID < 0 || nodeID > maxNodeID {
		return nil, fmt.Errorf("node ID must be between 0 and %d", maxNodeID)
	}

	return &SnowflakeGenerator{
		nodeID:   nodeID,
		sequence: 0,
	}, nil
}

// Generate 生成Snowflake ID
func (g *SnowflakeGenerator) Generate() (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now().UnixNano() / 1e6 // 转换为毫秒

	if now < g.lastTimestamp {
		return 0, fmt.Errorf("clock moved backwards, refusing to generate ID")
	}

	if now == g.lastTimestamp {
		// 同一毫秒内，序列号递增
		g.sequence = (g.sequence + 1) & maxSequence
		if g.sequence == 0 {
			// 序列号溢出，等待下一毫秒
			now = g.waitNextMillis(now)
		}
	} else {
		// 新的毫秒，序列号重置
		g.sequence = 0
	}

	g.lastTimestamp = now

	// 组装64位ID
	id := ((now - epoch) << timestampShift) |
		(g.nodeID << nodeIDShift) |
		g.sequence

	return id, nil
}

// waitNextMillis 等待直到下一毫秒
func (g *SnowflakeGenerator) waitNextMillis(currentMillis int64) int64 {
	for currentMillis <= g.lastTimestamp {
		currentMillis = time.Now().UnixNano() / 1e6
	}
	return currentMillis
}

// ParseSnowflakeID 解析Snowflake ID（用于调试和监控）
func ParseSnowflakeID(id int64) (timestamp int64, nodeID int64, sequence int64) {
	timestamp = (id >> timestampShift) + epoch
	nodeID = (id >> nodeIDShift) & maxNodeID
	sequence = id & maxSequence
	return
}
