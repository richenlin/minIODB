package idgen

import (
	"fmt"
	"regexp"
)

// IDGeneratorStrategy 定义ID生成策略类型
type IDGeneratorStrategy string

const (
	StrategyUUID         IDGeneratorStrategy = "uuid"
	StrategySnowflake    IDGeneratorStrategy = "snowflake"
	StrategyCustom       IDGeneratorStrategy = "custom"
	StrategyUserProvided IDGeneratorStrategy = "user_provided"
)

// IDGenerator ID生成器接口
type IDGenerator interface {
	Generate(table string, strategy IDGeneratorStrategy, prefix string) (string, error)
	Validate(id string, maxLength int, pattern string) error
}

// GeneratorConfig ID生成器配置
type GeneratorConfig struct {
	NodeID          int64  // 节点ID (用于Snowflake)
	DefaultStrategy string // 默认策略
}

// DefaultGenerator 默认ID生成器实现
type DefaultGenerator struct {
	config          *GeneratorConfig
	uuidGenerator   *UUIDGenerator
	snowflake       *SnowflakeGenerator
	customGenerator *CustomGenerator
	validator       *IDValidator
}

// NewGenerator 创建新的ID生成器
func NewGenerator(config *GeneratorConfig) (IDGenerator, error) {
	if config == nil {
		config = &GeneratorConfig{
			NodeID:          1,
			DefaultStrategy: string(StrategyUUID),
		}
	}

	// 初始化各个生成器
	uuidGen := NewUUIDGenerator()
	
	snowflake, err := NewSnowflakeGenerator(config.NodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to create snowflake generator: %w", err)
	}

	customGen := NewCustomGenerator()
	validator := NewIDValidator()

	return &DefaultGenerator{
		config:          config,
		uuidGenerator:   uuidGen,
		snowflake:       snowflake,
		customGenerator: customGen,
		validator:       validator,
	}, nil
}

// Generate 生成ID
func (g *DefaultGenerator) Generate(table string, strategy IDGeneratorStrategy, prefix string) (string, error) {
	// 如果未指定策略，使用默认策略
	if strategy == "" {
		strategy = IDGeneratorStrategy(g.config.DefaultStrategy)
	}

	switch strategy {
	case StrategyUUID:
		return g.uuidGenerator.Generate()
	case StrategySnowflake:
		id, err := g.snowflake.Generate()
		if err != nil {
			return "", err
		}
		// 如果有前缀，添加前缀
		if prefix != "" {
			return fmt.Sprintf("%s-%d", prefix, id), nil
		}
		return fmt.Sprintf("%d", id), nil
	case StrategyCustom:
		return g.customGenerator.Generate(prefix)
	case StrategyUserProvided:
		return "", fmt.Errorf("user_provided strategy requires ID to be provided by user")
	default:
		return "", fmt.Errorf("unknown ID generation strategy: %s", strategy)
	}
}

// Validate 验证ID
func (g *DefaultGenerator) Validate(id string, maxLength int, pattern string) error {
	return g.validator.Validate(id, maxLength, pattern)
}

// IDValidator ID验证器
type IDValidator struct {
	compiledPatterns map[string]*regexp.Regexp
}

// NewIDValidator 创建新的ID验证器
func NewIDValidator() *IDValidator {
	return &IDValidator{
		compiledPatterns: make(map[string]*regexp.Regexp),
	}
}

// Validate 验证ID格式
func (v *IDValidator) Validate(id string, maxLength int, pattern string) error {
	if id == "" {
		return fmt.Errorf("ID cannot be empty")
	}

	// 验证长度
	if maxLength > 0 && len(id) > maxLength {
		return fmt.Errorf("ID length %d exceeds maximum %d", len(id), maxLength)
	}

	// 如果指定了正则模式，进行验证
	if pattern != "" {
		re, ok := v.compiledPatterns[pattern]
		if !ok {
			var err error
			re, err = regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("invalid pattern: %w", err)
			}
			v.compiledPatterns[pattern] = re
		}

		if !re.MatchString(id) {
			return fmt.Errorf("ID does not match pattern: %s", pattern)
		}
	}

	return nil
}
