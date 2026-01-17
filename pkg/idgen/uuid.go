package idgen

import (
	"github.com/google/uuid"
)

// UUIDGenerator UUID生成器
type UUIDGenerator struct{}

// NewUUIDGenerator 创建UUID生成器
func NewUUIDGenerator() *UUIDGenerator {
	return &UUIDGenerator{}
}

// Generate 生成UUID v4
func (g *UUIDGenerator) Generate() (string, error) {
	id := uuid.New()
	return id.String(), nil
}
